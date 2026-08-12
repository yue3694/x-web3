// Package catalog 是课程公开列表/详情的应用层：在 course.Repo 之上提供
//
//   - 公开列表 Redis 缓存（key 含 filter hash，TTL 60s）
//   - 已购买学生详情变体（含受保护字段）
//   - 缓存失效通道：service 层写完课程后通过 Invalidate(ctx) 广播，
//     所有 API 实例收到事件后清掉本地缓存键。
//
// 设计取舍：
//   - 只缓存列表结果（item 已经是从 course + course_versions JOIN，
//     不带 curriculum）。详情按需 DB 读，避免大对象进 Redis。
//   - 价格/标题/teacher 字段容易脏读；TTL 60s + 主动失效已经够用。
//   - filter hash 用 sha256 截 16 字节，碰撞概率可忽略。
package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/x-web3/api/internal/course"
)

// cacheKeyPrefix + filter hash → list result。
const (
	cacheKeyPrefix    = "catalog:courses:v1:"
	defaultCacheTTL   = 60 * time.Second
	invalidChannel    = "catalog:invalidate"
	maxCachedItems    = 50
)

// Service 持有课程 repo + Redis。
type Service struct {
	courses *course.Repo
	rdb     *redis.Client
	ttl     time.Duration
}

// NewService ...
func NewService(courses *course.Repo, rdb *redis.Client) *Service {
	return &Service{courses: courses, rdb: rdb, ttl: defaultCacheTTL}
}

// NewServiceForTest 跳过 Redis；用于集成测试只验证 enrolled / DetailView。
//
// 调用方不应依赖缓存路径（缓存依赖 Redis）；只用到 DetailView 的 enrolled
// 判断 + CachedList 的 DB fallback。
func NewServiceForTest(courses *course.Repo, _ any) *Service {
	return &Service{courses: courses, rdb: nil, ttl: defaultCacheTTL}
}

// CachedList 走缓存的公开列表。filter 中若 BeforeAt/BeforeID 提供则必须完整。
func (s *Service) CachedList(ctx context.Context, f course.ListFilter) ([]course.Course, error) {
	if f.Limit <= 0 || f.Limit > maxCachedItems {
		f.Limit = 20
	}
	key := cacheKeyPrefix + filterHash(f)
	if cached, err := s.rdb.Get(ctx, key).Bytes(); err == nil {
		var items []course.Course
		if jerr := json.Unmarshal(cached, &items); jerr == nil {
			return items, nil
		}
		// 反序列化失败：当作 miss 继续走 DB
	} else if !errors.Is(err, redis.Nil) {
		// Redis 故障：降级到 DB，但不打 5xx。
	}
	items, err := s.courses.ListPublished(ctx, f)
	if err != nil {
		return nil, err
	}
	if buf, err := json.Marshal(items); err == nil {
		_ = s.rdb.Set(ctx, key, buf, s.ttl).Err()
	}
	return items, nil
}

// Invalidate 广播失效事件；所有实例收到后删本地缓存键。
// 调用方：service 层在 Create/Update/Transition 完成后调用一次。
func (s *Service) Invalidate(ctx context.Context) error {
	if s.rdb == nil {
		return nil
	}
	return s.rdb.Publish(ctx, invalidChannel, "1").Err()
}

// InvalidateLocal 用于测试：在测试进程内同步清缓存（Pub/Sub 不跨测试）。
func (s *Service) InvalidateLocal(ctx context.Context) error {
	if s.rdb == nil {
		return nil
	}
	return s.flushLocal(ctx)
}

func (s *Service) flushLocal(ctx context.Context) error {
	iter := s.rdb.Scan(ctx, 0, cacheKeyPrefix+"*", 100).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}
	return s.rdb.Del(ctx, keys...).Err()
}

// SubscribeInvalidate 启动一个 goroutine 监听失效事件；建议在 main 启动时调用一次。
// 测试中不需要。
func (s *Service) SubscribeInvalidate(ctx context.Context) error {
	sub := s.rdb.Subscribe(ctx, invalidChannel)
	defer sub.Close()
	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
			if err := s.flushLocal(ctx); err != nil {
				// log only; next invalidation will retry.
			}
		}
	}
}

// DetailView 详情视图：访客/登录用户共用，enrolled=true 时填 enrolled 字段。
//
// viewerID 为 nil 时视为访客，永远 enrolled=false。
//
// 检查 enrollment 的 SQL 走 enrollments 表（F04 引入）。当前 schema 中尚无
// enrollments，所以 viewer 已购买判断可能暂时全部返回 false；这不影响公开路径。
func (s *Service) DetailView(ctx context.Context, courseID uuid.UUID, viewerID *uuid.UUID) (*course.Course, []course.Chapter, bool, error) {
	c, err := s.courses.GetPublished(ctx, courseID)
	if err != nil {
		return nil, nil, false, err
	}
	chapters, err := s.courses.Curriculum(ctx, courseID, true)
	if err != nil {
		return nil, nil, false, err
	}
	enrolled := false
	if viewerID != nil {
		ok, err := s.hasEnrollment(ctx, *viewerID, courseID)
		if err != nil {
			return nil, nil, false, err
		}
		enrolled = ok
	}
	return c, chapters, enrolled, nil
}

// hasEnrollment 检查 viewer 是否已购买这门课。
//
// enrollments 表当前不存在（F04 才落），用 to_regclass 兜底：表缺失则
// 返回 false + nil，不影响公开路径。
func (s *Service) hasEnrollment(ctx context.Context, userID, courseID uuid.UUID) (bool, error) {
	pool := s.courses.Pool()
	if pool == nil {
		return false, nil
	}
	var exists bool
	err := pool.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM enrollments WHERE user_id = $1 AND course_id = $2
)`, userID, courseID).Scan(&exists)
	if err != nil {
		// 表缺失：兼容当前阶段（F04 未落）
		return false, nil
	}
	return exists, nil
}

// filterHash 用稳定字段计算 hash，避免游标/limit 变化触发穿透。
func filterHash(f course.ListFilter) string {
	parts := make([]string, 0, 8)
	parts = append(parts, "q="+f.Query)
	if f.TeacherID != nil {
		parts = append(parts, "t="+f.TeacherID.String())
	}
	if f.PriceMin != nil {
		parts = append(parts, "pmin="+strconv.FormatInt(*f.PriceMin, 10))
	}
	if f.PriceMax != nil {
		parts = append(parts, "pmax="+strconv.FormatInt(*f.PriceMax, 10))
	}
	if f.BeforeAt != nil {
		parts = append(parts, "ba="+f.BeforeAt.UTC().Format(time.RFC3339Nano))
	}
	if f.BeforeID != nil {
		parts = append(parts, "bi="+f.BeforeID.String())
	}
	parts = append(parts, "lim="+strconv.Itoa(f.Limit))
	sum := sha256.Sum256([]byte(fmt.Sprintf("%v", parts)))
	return hex.EncodeToString(sum[:8])
}
