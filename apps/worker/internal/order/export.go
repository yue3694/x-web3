package workerorder

import (
	"github.com/google/uuid"

	"github.com/x-web3/worker/internal/chain"
)

// BuildIntentFromDBForTest 暴露给外部 test 包使用；
// 真实调用方是 Confirmer.Apply，仅在数据库事务里跑。
func BuildIntentFromDBForTest(
	dbCourseKey []byte,
	dbTokenAddress, dbAmount string,
	dbPriceVersion int,
	intentID uuid.UUID,
	dbWalletAddress string,
) (chain.Intent, error) {
	return buildIntentFromDB(dbCourseKey, dbTokenAddress, dbAmount, dbPriceVersion, intentID, dbWalletAddress)
}
