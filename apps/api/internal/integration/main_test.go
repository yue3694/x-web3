// 包级 TestMain：integration_test 的入口；测试结束或 SIGINT 时关掉
// testcontainers 启动的 PG 容器。INTEGRATION_KEEP_TC=1 时保留方便排查。
package integration_test

import (
	"os"
	"testing"

	"github.com/x-web3/api/internal/integration/testenv"
)

func TestMain(m *testing.M) {
	code := m.Run()
	testenv.TerminateContainer()
	os.Exit(code)
}
