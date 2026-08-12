package indexer

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 这些 helper 只在 *_test.go 编译；保持 production binary 干净。

var (
	integrationOnce  sync.Once
	integrationDBURL string
	integrationDBOK  bool
)

func integrationDBEnabled() bool {
	integrationOnce.Do(func() {
		if url := os.Getenv("DATABASE_URL_TEST"); url != "" {
			integrationDBURL = url
			integrationDBOK = true
		} else if os.Getenv("INTEGRATION_USE_TC") == "1" {
			// testcontainers 自动启动；具体实现在 integration_test.go
			// 失败时由 t.Skip 兜底。
			integrationDBOK = true
		}
	})
	return integrationDBOK
}

// integrationPool 返回单测池；调用方负责 Close。
//
// 当前实现：只有 DATABASE_URL_TEST 直接返回；testcontainer 留给后续接入。
func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if integrationDBURL == "" {
		t.Skip("DATABASE_URL_TEST not set")
	}
	pool, err := pgxpool.New(context.Background(), integrationDBURL)
	if err != nil {
		t.Fatalf("pgxpool: %v", err)
	}
	return pool
}
