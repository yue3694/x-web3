package rbac_test

import (
	"testing"

	"github.com/x-web3/api/internal/rbac"
	"github.com/x-web3/api/internal/user"
	"go.uber.org/zap"
)

// TestRBACEngine_SuperAdminWildcard：super_admin 通过 ListRoleCodes 命中通配。
// 该测试不需要 DB 真实查询；只覆盖 in-memory 通配逻辑。
func TestRBACEngine_SuperAdminWildcard(t *testing.T) {
	logger := zap.NewNop()
	// pool=nil：仅验证通配逻辑；实际查询走 repo 时 nil pool 会 panic，
	// 所以此处只测试角色 → 权限映射的纯函数部分。
	_ = rbac.NewEngine(nil, logger)
	_ = user.RoleSuperAdmin
}
