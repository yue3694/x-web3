// Package testenv 提供端到端集成测试的共享测试环境。
//
// 目标：
//  1. 默认走 DATABASE_URL_TEST（运维侧 CI 启动的 PG）+ miniredis；
//  2. 当 INTEGRATION_USE_TC=1 且未提供 URL 时，自动用 testcontainers-go 拉起
//     临时 PG（postgres:16-alpine），一次性套用 migrations + seed，避免每个
//     开发者手动起服务；
//  3. 单个 go test 进程共享一个容器（sync.Once），每个用例 TRUNCATE wallets
//     即可隔离。
//
// 选型动机：testcontainers-go 可以在 CI 零配置起服务；同时保留 DATABASE_URL_TEST
// 兼容现有脚本与团队本地复用（指向 Postgres.app / brew / docker）。
package testenv

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// PGFixture 描述一组 integration 测试用的 PG。
type PGFixture struct {
	URL       string
	Container *postgres.PostgresContainer // nil = 环境变量驱动
	applyMu   sync.Mutex
	applied   bool
}

var (
	pgOnce sync.Once
	pgFix  *PGFixture
	pgErr  error
)

// BootPG 启动一次 PG 并应用 migrations + seed。
//
// 优先用 DATABASE_URL_TEST；缺省且 INTEGRATION_USE_TC=1 时拉 testcontainer。
// 都没有时调用 t.Skip 跳过整个测试。
func BootPG(t *testing.T) *PGFixture {
	t.Helper()
	pgOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		if url := strings.TrimSpace(os.Getenv("DATABASE_URL_TEST")); url != "" {
			pgFix = &PGFixture{URL: url}
		} else if os.Getenv("INTEGRATION_USE_TC") == "1" {
			c, err := postgres.Run(ctx,
				"postgres:16-alpine",
				postgres.WithDatabase("xweb3_test"),
				postgres.WithUsername("postgres"),
				postgres.WithPassword("postgres"),
				testcontainers.WithWaitStrategy(
					wait.ForLog("database system is ready to accept connections").
						WithOccurrence(2).
						WithStartupTimeout(90*time.Second),
				),
			)
			if err != nil {
				pgErr = fmt.Errorf("testcontainers postgres: %w", err)
				return
			}
			cs, err := c.ConnectionString(ctx, "sslmode=disable")
			if err != nil {
				_ = testcontainers.TerminateContainer(c)
				pgErr = fmt.Errorf("postgres connection string: %w", err)
				return
			}
			pgFix = &PGFixture{URL: cs, Container: c}
		} else {
			pgErr = fmt.Errorf("integration: set DATABASE_URL_TEST or INTEGRATION_USE_TC=1 to enable")
		}
	})
	if pgErr != nil {
		t.Skip(pgErr.Error())
	}
	if _, err := pgFix.applyOnce(); err != nil {
		t.Skipf("integration: migrations apply failed: %v", err)
	}
	return pgFix
}

// applyOnce 保证 migrations + seed 只跑一次。
//
// 幂等性：若 DB 中已存在 `users` 表（说明已被外部手动 migrate / 上次 session 残留），
// 直接跳过 migration；避免重复 CREATE TABLE 报错。
func (f *PGFixture) applyOnce() (bool, error) {
	if f == nil {
		return false, fmt.Errorf("nil fixture")
	}
	f.applyMu.Lock()
	defer f.applyMu.Unlock()
	if f.applied {
		return false, nil
	}
	alreadyMigrated, err := schemaExists(f.URL)
	if err != nil {
		return false, fmt.Errorf("probe schema: %w", err)
	}
	if !alreadyMigrated {
		if err := applySQLViaPSQL(f.URL, MigrationFiles()); err != nil {
			return false, fmt.Errorf("apply migrations: %w", err)
		}
	}
	if err := applySQLViaPSQL(f.URL, SeedFiles()); err != nil {
		// seed 用 ON CONFLICT DO NOTHING，重复执行是幂等的；
		// 但若 seed 表还没建好（migration 没跑过），返回 500。
		// 已经走到这步通常意味着 migration 已建，只是 seed 没被执行，补一次。
		if !alreadyMigrated {
			return false, fmt.Errorf("apply seed: %w", err)
		}
	}
	f.applied = true
	return true, nil
}

// schemaExists 检查 `users` 表是否已存在；用 psql -tA 直接询问。
func schemaExists(dsn string) (bool, error) {
	cmd := exec.Command("psql", dsn, "-tA", "-c",
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='users')`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("psql probe: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)) == "t", nil
}

// MigrationFiles 列出要按顺序应用的 migration；按字典序追加。
func MigrationFiles() []string {
	return repoSQLFiles([]string{
		"0001_identity.up.sql",
		"0002_course.up.sql",
		"0003_enrollments.up.sql",
		"0004_order.up.sql",
		"0007_reorg_reconcile.up.sql",
		"0009_cert_jobs.up.sql",
	})
}

// SeedFiles 列出要按顺序应用的 seed。
func SeedFiles() []string {
	return repoSQLFiles([]string{"0001_roles.sql"})
}

// repoSQLFiles 从仓库根目录 database/{dir} 取 SQL 文件绝对路径列表。
func repoSQLFiles(names []string) []string {
	root := repoRoot()
	out := make([]string, 0, len(names))
	for _, n := range names {
		// migration by default; fall back to seed if missing
		p := filepath.Join(root, "database", "migrations", n)
		if !fileExists(p) {
			p = filepath.Join(root, "database", "seed", n)
		}
		out = append(out, p)
	}
	return out
}

// TerminateContainer 在测试结束或 SIGINT 时关掉 TC 起的容器；
// 当 INTEGRATION_KEEP_TC=1 时保留方便排查。
func TerminateContainer() {
	if pgFix != nil && pgFix.Container != nil && os.Getenv("INTEGRATION_KEEP_TC") != "1" {
		terminateCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		_ = testcontainers.TerminateContainer(pgFix.Container)
		cancel()
		_ = terminateCtx // 预留：未来需要等待容器确认 SIGTERM 退出
	}
}

func repoRoot() string {
	// runtime.Caller(0) 在 test 包里指向 internal/integration/testenv；往上 4 层即 monorepo root。
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	dir := filepath.Dir(thisFile)
	for i := 0; i < 8; i++ {
		if fileExists(filepath.Join(dir, "go.work")) || fileExists(filepath.Join(dir, "database", "migrations")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return dir
}

func fileExists(p string) bool {
	if _, err := os.Stat(p); err == nil {
		return true
	}
	return false
}

// applySQLViaPSQL 通过系统 psql 客户端应用 SQL。
// 优点：能处理多 MB SQL 文件，且不依赖 driver。
func applySQLViaPSQL(dsn string, paths []string) error {
	for _, p := range paths {
		cmd := exec.Command("psql", dsn, "-v", "ON_ERROR_STOP=1", "-q", "-f", p)
		cmd.Env = append(os.Environ(), "PGCONNECT_TIMEOUT=10")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("psql %s: %w: %s", filepath.Base(p), err, string(out))
		}
	}
	return nil
}

// Pool 启动 PG 并返回连接池。
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	f := BootPG(t)
	pool, err := pgxpool.New(context.Background(), f.URL)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("database ping: %v", err)
	}
	return pool
}
