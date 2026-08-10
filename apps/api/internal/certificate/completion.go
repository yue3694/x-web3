// Package certificate — 完课判定 + mint job 创建（F04-T07）。
//
// 设计要点：
//   - POST /courses/{id}/complete 是「重算式」接口：
//     每次调用都重新评估 lesson_progress 与课程 required lessons，
//     满足 100% 才创建 course_completions + certificates + certificate_jobs；
//   - 三表写入在同一个 pgx.Tx：
//     1) course_completions(enrollment_id, rule_version) — UNIQUE 兜底并发；
//     2) certificates(completion_id, user_id, course_id, certificate_id, ...) —
//        UNIQUE(user_id, course_id, cert_version)；
//     3) certificate_jobs(certificate_id, status='pending', attempt=0) — 1:1 关联；
//   - 幂等：第二次调用走 ON CONFLICT DO NOTHING + SELECT 回查，返回相同 completion；
//   - 鉴权：必须已 enrollment，否则 ErrNotEnrolled；
//   - 422 区分：未达 100%（partial）→ 422 UNPROCESSABLE_ENTITY；
//     已存在完成记录 → 200 + 已存在 record（idempotent success）。
//   - certificateId（uint256）按 sha256(user_id||course_id||rule_version) 派生，
//     在 (user_id, course_id, cert_version) 上 UNIQUE 兜底并发；worker 上链时即使用
//     同样的证书 ID。
package certificate

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// 错误哨兵：handler 用 errors.Is 分类到 errcode。
var (
	// ErrNotEnrolled 用户未 enrollment，无法触发完课判定。
	ErrNotEnrolled = errors.New("certificate: not enrolled")
	// ErrCourseNotFound 课程不存在。
	ErrCourseNotFound = errors.New("certificate: course not found")
	// ErrNotCompleted 尚未达成 100% 必修课时进度。
	ErrNotCompleted = errors.New("certificate: not all required lessons completed")
	// ErrNoRecipientWallet 用户没有绑定任何钱包（is_primary 也没有），无法签发证书。
	ErrNoRecipientWallet = errors.New("certificate: no recipient wallet bound")
	// ErrMetadataUpload 元数据上传失败（store unreachable）。
	ErrMetadataUpload = errors.New("certificate: metadata upload failed")
)

// ServiceConfig 装配 Service 所需依赖。
type ServiceConfig struct {
	Pool     *pgxpool.Pool
	Metadata *Generator // 完成时生成 + 上传元数据；nil 时跳过上传写占位符（dev 用）
	ChainID  int64      // 写入 certificates.chain_id；零值时取 ETH chain_id default 11155111
	Logger   *zap.Logger
}

// Service 是完课判定子系统的入口。
type Service struct {
	pool     *pgxpool.Pool
	metadata *Generator
	chainID  int64
	logger   *zap.Logger
}

// NewService 构造完课判定服务。
//
// metadata 可为 nil（仅在 dev / 单测需要元数据 stub 时使用）；chainID 必须 > 0。
func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.Pool == nil {
		return nil, errors.New("certificate: pool is nil")
	}
	if cfg.ChainID <= 0 {
		cfg.ChainID = 11155111 // Sepolia 默认
	}
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}
	return &Service{
		pool:     cfg.Pool,
		metadata: cfg.Metadata,
		chainID:  cfg.ChainID,
		logger:   cfg.Logger,
	}, nil
}

// CompletionRecord 是 API 返回的完课记录（包含 certificate 摘要）。
type CompletionRecord struct {
	ID                    uuid.UUID  `json:"id"`
	EnrollmentID          uuid.UUID  `json:"enrollmentId"`
	UserID                uuid.UUID  `json:"userId"`
	CourseID              uuid.UUID  `json:"courseId"`
	RuleVersion           int        `json:"ruleVersion"`
	CompletedAt           time.Time  `json:"completedAt"`
	CompletedLessonsCount int        `json:"completedLessonsCount"`
	TotalLessonsCount     int        `json:"totalLessonsCount"`
	CertificateID         *uuid.UUID `json:"certificateId,omitempty"` // 关联的 certificates.id（若有）
	OnchainCertID         string     `json:"onchainCertId"`           // numeric(78,0) 字符串形式
	Status                string     `json:"status"`                  // pending / minting / confirmed / failed / dead
	RecipientWallet       string     `json:"recipientWallet"`
	MetadataURI           string     `json:"metadataUri"`
	MetadataSHA256        string     `json:"metadataSha256"`
}

// CompleteCourse 评估完课并原子地写 completion + certificate + mint job。
//
//   - user 必须已 enrollment（ErrNotEnrolled）；
//   - required lessons pct=100 计数必须 == required 计数（ErrNotCompleted）；
//   - 用户必须至少有 1 个已绑定的钱包（recipient），否则 ErrNoRecipientWallet；
//   - 已存在 completion → 返回原记录，且不会创建新 job（幂等）；
//   - 满足时：插入 course_completions + certificates + certificate_jobs in single tx。
func (s *Service) CompleteCourse(ctx context.Context, userID, courseID uuid.UUID) (*CompletionRecord, error) {
	// 1. course 存在性
	var courseExists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM courses WHERE id = $1 AND deleted_at IS NULL)`,
		courseID,
	).Scan(&courseExists); err != nil {
		return nil, err
	}
	if !courseExists {
		return nil, ErrCourseNotFound
	}

	// 2. enrollment
	enrollmentID, err := s.userEnrollmentID(ctx, userID, courseID)
	if err != nil {
		return nil, err
	}
	if enrollmentID == uuid.Nil {
		return nil, ErrNotEnrolled
	}

	// 3. 已完成：直接返回（幂等）
	existing, err := s.fetchCompletion(ctx, enrollmentID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	// 4. 计算必修课时数 vs 完成数
	required, completed, err := s.countProgress(ctx, userID, courseID)
	if err != nil {
		return nil, err
	}
	if required == 0 || completed < required {
		return nil, fmt.Errorf("%w: required=%d completed=%d", ErrNotCompleted, required, completed)
	}

	// 5. 解析 recipient wallet（primary 优先；否则首个）
	recipient, err := s.pickRecipientWallet(ctx, userID)
	if err != nil {
		return nil, err
	}
	if recipient == (common.Address{}) {
		return nil, ErrNoRecipientWallet
	}

	// 6. 解析 rule_version（取 courses.current_version）
	ruleVersion, err := s.currentRuleVersion(ctx, courseID)
	if err != nil {
		return nil, err
	}

	// 7. 生成 metadata + 上传（失败时直接拒绝完成 — 不允许把 ipfs://pending 占位符
	// 写到链上旁路表，否则 worker 会用 stale URI 签名 → 链上 metadata 与实际不符）。
	metadataURI, metadataSHA, err := s.uploadMetadata(ctx, userID, courseID, recipient)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMetadataUpload, err)
	}

	// 8. 派生确定性 certificateId（与链上 bytes32 / uint256 对齐）
	certNumeric := deriveCertificateID(userID, courseID, ruleVersion)

	// 9. 原子写：completion + certificate + job in single tx
	rec, err := s.atomicUpsert(ctx, CompletionInput{
		EnrollmentID:  enrollmentID,
		UserID:        userID,
		CourseID:      courseID,
		RuleVersion:   ruleVersion,
		Completed:     completed,
		Required:      required,
		CertificateID: certNumeric,
		Recipient:     recipient,
		MetadataURI:   metadataURI,
		MetadataSHA:   metadataSHA,
	})
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// CompletionInput 内部装配参数，避免 atomicUpsert 形参列表过长。
type CompletionInput struct {
	EnrollmentID  uuid.UUID
	UserID        uuid.UUID
	CourseID      uuid.UUID
	RuleVersion   int
	Completed     int
	Required      int
	CertificateID *big.Int
	Recipient     common.Address
	MetadataURI   string
	MetadataSHA   string
}

// atomicUpsert 在单个事务里写 course_completions + certificates + certificate_jobs。
//
// 关键：所有 INSERT 都用 ON CONFLICT DO NOTHING + RETURNING；
// 如果并发请求同时插入同 (user_id, course_id, rule_version) 重复，第二个会从
// RETURNING 拿到空（pgx.ErrNoRows），此时回查 course_completions 返回原记录。
func (s *Service) atomicUpsert(ctx context.Context, in CompletionInput) (*CompletionRecord, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck

	// 5a. 写 course_completions（UNIQUE on enrollment_id + rule_version）
	var (
		completionID uuid.UUID
		completedAt  time.Time
	)
	err = tx.QueryRow(ctx, `
INSERT INTO course_completions (enrollment_id, rule_version)
VALUES ($1, $2)
ON CONFLICT (enrollment_id, rule_version) DO NOTHING
RETURNING id, completed_at`,
		in.EnrollmentID, in.RuleVersion).Scan(&completionID, &completedAt)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("insert course_completions: %w", err)
		}
		// 并发场景：ON CONFLICT DO NOTHING 命中 → 回查
		err = tx.QueryRow(ctx, `
SELECT id, completed_at
  FROM course_completions
 WHERE enrollment_id = $1 AND rule_version = $2`,
			in.EnrollmentID, in.RuleVersion).Scan(&completionID, &completedAt)
		if err != nil {
			return nil, fmt.Errorf("refetch course_completions: %w", err)
		}
	}

	// 5b. 写 certificates（UNIQUE on user_id, course_id, cert_version）
	var (
		certID uuid.UUID
		status string
		txHash []byte
	)
	err = tx.QueryRow(ctx, `
INSERT INTO certificates (
    completion_id, user_id, course_id, cert_version,
    certificate_id, recipient_wallet, metadata_uri, metadata_sha256,
    chain_id, status
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'pending')
ON CONFLICT (user_id, course_id, cert_version) DO NOTHING
RETURNING id, status, tx_hash`,
		completionID, in.UserID, in.CourseID, in.RuleVersion,
		in.CertificateID.String(), in.Recipient.Hex(), in.MetadataURI, in.MetadataSHA,
		s.chainID).Scan(&certID, &status, &txHash)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("insert certificates: %w", err)
		}
		// 并发场景：回查
		err = tx.QueryRow(ctx, `
SELECT id, status, tx_hash
  FROM certificates
 WHERE user_id = $1 AND course_id = $2 AND cert_version = $3`,
			in.UserID, in.CourseID, in.RuleVersion).Scan(&certID, &status, &txHash)
		if err != nil {
			return nil, fmt.Errorf("refetch certificates: %w", err)
		}
	}

	// 5c. 写 certificate_jobs（FK 到 cert.id，UNIQUE on certificate_id）。
	// 第二次调用走 ON CONFLICT 跳过；worker claimBatch WHERE status='pending'
	// 自然过滤已 confirmed 的 job。
	if _, err := tx.Exec(ctx, `
INSERT INTO certificate_jobs (certificate_id, status, attempt, next_retry_at)
VALUES ($1, 'pending', 0, now())
ON CONFLICT (certificate_id) DO NOTHING`,
		certID); err != nil {
		return nil, fmt.Errorf("insert certificate_jobs: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return &CompletionRecord{
		ID:                    completionID,
		EnrollmentID:          in.EnrollmentID,
		UserID:                in.UserID,
		CourseID:              in.CourseID,
		RuleVersion:           in.RuleVersion,
		CompletedAt:           completedAt,
		CompletedLessonsCount: in.Completed,
		TotalLessonsCount:     in.Required,
		CertificateID:         &certID,
		OnchainCertID:         in.CertificateID.String(),
		Status:                status,
		RecipientWallet:       in.Recipient.Hex(),
		MetadataURI:           in.MetadataURI,
		MetadataSHA256:        in.MetadataSHA,
	}, nil
}

// fetchCompletion 取已有完课记录（含 certificate 视图）；不存在返回 nil, nil。
//
// 视图选择：若同一 enrollment + rule_version 对应多条 certificate（理论不会，
// 因 UNIQUE on user_id+course_id+cert_version 兜底），取最近写入的。
func (s *Service) fetchCompletion(ctx context.Context, enrollmentID uuid.UUID) (*CompletionRecord, error) {
	var (
		rec           CompletionRecord
		certID        *uuid.UUID
		certNumeric   *string
		status        *string
		recipient     *string
		metadataURI   *string
		metadataSHA   *string
		userID        uuid.UUID
		courseID      uuid.UUID
	)
	err := s.pool.QueryRow(ctx, `
SELECT
  cc.id, cc.enrollment_id, cc.rule_version, cc.completed_at,
  c.id, c.certificate_id::text, c.status, c.recipient_wallet,
  c.metadata_uri, c.metadata_sha256,
  e.user_id, e.course_id
FROM course_completions cc
JOIN enrollments e ON e.id = cc.enrollment_id
LEFT JOIN certificates c
  ON c.completion_id = cc.id
WHERE cc.enrollment_id = $1
ORDER BY cc.completed_at DESC
LIMIT 1`,
		enrollmentID,
	).Scan(
		&rec.ID, &rec.EnrollmentID, &rec.RuleVersion, &rec.CompletedAt,
		&certID, &certNumeric, &status, &recipient, &metadataURI, &metadataSHA,
		&userID, &courseID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rec.UserID = userID
	rec.CourseID = courseID
	rec.CertificateID = certID
	if certNumeric != nil {
		rec.OnchainCertID = *certNumeric
	}
	if status != nil {
		rec.Status = *status
	}
	if recipient != nil {
		rec.RecipientWallet = *recipient
	}
	if metadataURI != nil {
		rec.MetadataURI = *metadataURI
	}
	if metadataSHA != nil {
		rec.MetadataSHA256 = *metadataSHA
	}
	return &rec, nil
}

// userEnrollmentID 取 (user_id, course_id) 对应的 enrollment.id；不存在返回 uuid.Nil, nil。
func (s *Service) userEnrollmentID(ctx context.Context, userID, courseID uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM enrollments WHERE user_id = $1 AND course_id = $2`,
		userID, courseID,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	if err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// currentRuleVersion 取课程当前生效的 rule_version（来自 courses.current_version）。
func (s *Service) currentRuleVersion(ctx context.Context, courseID uuid.UUID) (int, error) {
	var v int
	err := s.pool.QueryRow(ctx,
		`SELECT current_version FROM courses WHERE id = $1`, courseID,
	).Scan(&v)
	if err != nil {
		return 0, err
	}
	return v, nil
}

// pickRecipientWallet 优先 primary；若没有取首个；都没有返回零地址。
func (s *Service) pickRecipientWallet(ctx context.Context, userID uuid.UUID) (common.Address, error) {
	var addr string
	err := s.pool.QueryRow(ctx, `
SELECT address
  FROM wallets
 WHERE user_id = $1 AND chain_namespace = 'eip155'
 ORDER BY is_primary DESC, created_at ASC
 LIMIT 1`, userID).Scan(&addr)
	if errors.Is(err, pgx.ErrNoRows) {
		return common.Address{}, nil
	}
	if err != nil {
		return common.Address{}, err
	}
	return common.HexToAddress(addr), nil
}

// countProgress 计算 (required lessons 数, pct=100 数)。
//
// 只看 lessons.required = true 的课时；其它视为选修，不计入完课判定。
// 通过 courses.current_version + course_versions.version 锁定**当前生效**版本，
// 老版本的 lessons 不参与完课判定（避免 teacher draft 误判）。
func (s *Service) countProgress(ctx context.Context, userID, courseID uuid.UUID) (required, completed int, err error) {
	row := s.pool.QueryRow(ctx, `
SELECT
  COUNT(*) FILTER (WHERE l.required),
  COUNT(*) FILTER (WHERE l.required AND COALESCE(lp.pct, 0) = 100)
FROM lessons l
JOIN chapters ch ON ch.id = l.chapter_id
JOIN course_versions v ON v.id = ch.course_version_id
JOIN courses c ON c.id = v.course_id AND c.current_version = v.version
LEFT JOIN lesson_progress lp ON lp.lesson_id = l.id AND lp.user_id = $1
WHERE v.course_id = $2
`, userID, courseID)
	if err := row.Scan(&required, &completed); err != nil {
		return 0, 0, err
	}
	return required, completed, nil
}

// uploadMetadata 走 Generator 上传元数据；store 不可用时返回占位符。
//
// metadata 字段需要 course name + description + image URI，这里先用课程 slug/title
// 兜底；真实课程 metadata 上传接口（F04-T09 由独立 admin endpoint 提供）会替换此占位。
func (s *Service) uploadMetadata(
	ctx context.Context,
	userID, courseID uuid.UUID,
	recipient common.Address,
) (uri, sha256Hex string, err error) {
	if s.metadata == nil {
		return fmt.Sprintf("ipfs://pending-%s-%s", userID.String(), courseID.String()),
			"pending", nil
	}
	// 取课程基础信息（title / description）
	var title, description string
	if err := s.pool.QueryRow(ctx,
		`SELECT title, slug FROM courses WHERE id = $1`, courseID,
	).Scan(&title, &description); err != nil {
		return "", "", fmt.Errorf("load course: %w", err)
	}
	res, err := s.metadata.GenerateAndUpload(ctx, userID, CourseMeta{
		Name:            title + " — Completion",
		Description:     description,
		ImageURI:        "https://cdn.example.com/badges/default.png",
		CourseID:        courseID,
		CourseVersion:   1,
		CompletionDate:  time.Now().UTC(),
		RecipientWallet: recipient,
		IssuerName:      "x-web3 University",
	})
	if err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrMetadataUpload, err)
	}
	return res.URI, res.SHA256Hex, nil
}

// deriveCertificateID 计算 uint256 形式的证书 ID：sha256(user_id||course_id||rule_version)
// 取 32 字节，big-endian 解析成 big.Int。保证 (user_id, course_id, cert_version)
// 三元组对应一个确定性证书 ID，重复插入会被 (user_id, course_id, cert_version) UNIQUE 兜底。
func deriveCertificateID(userID, courseID uuid.UUID, ruleVersion int) *big.Int {
	h := sha256.New()
	h.Write(userID[:])
	h.Write(courseID[:])
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(ruleVersion))
	h.Write(buf[:])
	sum := h.Sum(nil)
	n := new(big.Int).SetBytes(sum)
	// 强制为 uint256：取 mod 2^256
	mod := new(big.Int).Lsh(big.NewInt(1), 256)
	n.Mod(n, mod)
	return n
}