package integration_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x-web3/api/internal/integration/testenv"
)

// testPool 兼容历史用例：转调 testenv.Pool。
//
// 历史用例原本是 `func testPool(t *testing.T) *pgxpool.Pool` 直接读
// DATABASE_URL_TEST；testenv.Pool 支持同一路径 + testcontainer 回退，
// 行为兼容。
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return testenv.Pool(t)
}
