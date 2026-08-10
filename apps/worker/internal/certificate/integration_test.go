//go:build integration

// F04-T15：worker 端端到端集成测试。
//
// 流程：插入 cert_jobs + certificates → consumer.runOnce() →
//   生成 metadata（worker 不真生成，复用 apps/api 元数据直接预填 URI） →
//   签名（fake signer / anvilDriver 真实签名）→ 广播（fake ethclient） →
//   receipt 确认 → status=confirmed。
//
// 失败场景：minedErr 持续 → retry → 达 maxAttempts → dead + DLQ。
//
// 幂等：pre-confirmed → consumer 跳过（SKIP LOCKED + status!='pending'）。
//
// 入口：DATABASE_URL_TEST 必须指向带 0009_cert_jobs migration 的 PG。
package certificate_test

import (
	"context"
	"errors"
	"log/slog"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	certpkg "github.com/x-web3/worker/internal/certificate"
	"github.com/x-web3/worker/internal/reconcile"
)

const (
	itChainID    = 11155111 // Sepolia（同 worker 配置默认）
	itConfirm    = 12
	itBatch      = 10
	itPollFast   = 50 * time.Millisecond
)

// anvilPK 公开测试私钥；与 signer_test 共享。
const itAnvilPK = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

// anvilAddr 是 anvilPK 对应的已知地址。
var itAnvilAddr = common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")

// itContract 任意非零地址；consumer 不真广播，仅 ABI 编码校验。
var itContract = common.HexToAddress("0x5FbDB2315678afecb367f032d93F642f64180aa3")

// itRecipient 接收方。
var itRecipient = common.HexToAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79C8")

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func itPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL_TEST")
	if dsn == "" {
		t.Skip("DATABASE_URL_TEST not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		t.Skipf("pg ping failed: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func itLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

// itNewSigner 用 anvilDriver 构造真签名器（但不连 RPC；用 StaticTxParams 兜底）。
func itNewSigner(t *testing.T) *certpkg.AnvilDriver {
	t.Helper()
	d, err := certpkg.NewAnvilDriver(certpkg.SignerConfig{
		Driver:   certpkg.DriverAnvil,
		ChainID:  big.NewInt(itChainID),
		Contract: itContract,
		Params:   certpkg.StaticTxParams{Params: certpkg.TxParams{Nonce: 7, GasLimit: 100_000}},
		AnvilPrivateKey: "0x" + itAnvilPK,
	})
	if err != nil {
		t.Fatalf("NewAnvilDriver: %v", err)
	}
	return d
}

// seedJobRow 落一组（certificates + certificate_jobs）用于单测。
func seedJobRow(t *testing.T, pool *pgxpool.Pool, recipient common.Address, certID *big.Int, prefilledURI string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	userID := uuid.New()
	courseID := uuid.New()
	completionID := uuid.New()
	enrollmentID := uuid.New()

	if _, err := pool.Exec(ctx,
		`INSERT INTO users(id, privy_user_id) VALUES($1, $2) ON CONFLICT DO NOTHING`,
		userID, "u-"+userID.String()); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO courses(id, teacher_id, title, slug, status) VALUES($1, $2, 't', $3, 'published')`,
		courseID, userID, "c-"+courseID.String()); err != nil {
		t.Fatalf("insert course: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO enrollments(id, user_id, course_id) VALUES($1, $2, $3)`,
		enrollmentID, userID, courseID); err != nil {
		t.Fatalf("insert enrollment: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO course_completions(id, enrollment_id, rule_version) VALUES($1, $2, 1)`,
		completionID, enrollmentID); err != nil {
		t.Fatalf("insert completion: %v", err)
	}

	certRowID := uuid.New()
	if _, err := pool.Exec(ctx, `
INSERT INTO certificates(id, completion_id, user_id, course_id, cert_version,
                         certificate_id, recipient_wallet, metadata_uri, metadata_sha256,
                         chain_id, status)
VALUES($1,$2,$3,$4,1,$5,$6,$7,'deadbeef',$8,'pending')`,
		certRowID, completionID, userID, courseID,
		certID.String(), recipient.Hex(), prefilledURI, itChainID); err != nil {
		t.Fatalf("insert certificate: %v", err)
	}

	jobID := uuid.New()
	if _, err := pool.Exec(ctx, `
INSERT INTO certificate_jobs(id, certificate_id, status, attempt, next_retry_at)
VALUES($1, $2, 'pending', 0, now() - interval '1 second')`,
		jobID, certRowID); err != nil {
		t.Fatalf("insert certificate_jobs: %v", err)
	}
	return jobID, certRowID
}

func itJobStatus(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID) string {
	t.Helper()
	var s string
	if err := pool.QueryRow(context.Background(),
		`SELECT status FROM certificate_jobs WHERE id=$1`, jobID).Scan(&s); err != nil {
		t.Fatalf("read job status: %v", err)
	}
	return s
}

func itCertStatus(t *testing.T, pool *pgxpool.Pool, certID uuid.UUID) string {
	t.Helper()
	var s string
	if err := pool.QueryRow(context.Background(),
		`SELECT status FROM certificates WHERE id=$1`, certID).Scan(&s); err != nil {
		t.Fatalf("read cert status: %v", err)
	}
	return s
}

// ---------------------------------------------------------------------------
// fakeEthClient — 注入 fake RPC。完整实现 certpkg.EthClient 接口。
// ---------------------------------------------------------------------------

type fakeEthClient struct {
	broadcastErrs  []error
	minedErr       error
	minedStatus    uint64
	broadcastCalls int
	minedCalls     int
}

func (f *fakeEthClient) PendingNonceAt(_ context.Context, _ common.Address) (uint64, error) {
	return 1, nil
}

func (f *fakeEthClient) SendTransaction(_ context.Context, _ *types.Transaction) error {
	f.broadcastCalls++
	if len(f.broadcastErrs) == 0 {
		return nil
	}
	err := f.broadcastErrs[0]
	f.broadcastErrs = f.broadcastErrs[1:]
	return err
}

func (f *fakeEthClient) WaitMined(_ context.Context, hash common.Hash) (*certpkg.Receipt, error) {
	f.minedCalls++
	if f.minedErr != nil {
		return nil, f.minedErr
	}
	return &certpkg.Receipt{
		TxHash:      hash,
		BlockNumber: 42,
		BlockHash:   common.HexToHash("0xab"),
		Status:      f.minedStatus,
	}, nil
}

var _ certpkg.EthClient = (*fakeEthClient)(nil)

// ---------------------------------------------------------------------------
// Test：端到端 success path
// ---------------------------------------------------------------------------

func TestIntegration_Consumer_SuccessMarksConfirmed(t *testing.T) {
	pool := itPool(t)

	jobID, certUUID := seedJobRow(t, pool, itRecipient, big.NewInt(12345),
		"ipfs://bafk/integration-test/1.json")

	// 用 cert 包的 EthClient（fakeEthClient 来自 consumer_test.go）。
	// 这里用本地简化 fake，只暴露 SendTransaction/WaitMined 必需。
	client := &fakeEthClient{minedStatus: 1}
	signer := itNewSigner(t)
	dlq := &dlqFake{}

	c, err := certpkg.NewConsumer(certpkg.ConsumerConfig{
		Pool:         pool,
		Signer:       signer,
		Client:       client,
		DLQ:          dlq,
		ChainID:      itChainID,
		BatchSize:    itBatch,
		PollInterval: itPollFast,
		MaxAttempts:  3,
		ConfirmDepth: itConfirm,
		Logger:       itLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	c.HandleJobForTest(context.Background(), jobID, certUUID, 0)

	if got := itJobStatus(t, pool, jobID); got != "confirmed" {
		t.Fatalf("job status = %q, want confirmed", got)
	}
	if got := itCertStatus(t, pool, certUUID); got != "confirmed" {
		t.Fatalf("cert status = %q, want confirmed", got)
	}
}

// ---------------------------------------------------------------------------
// Test：retry → eventual dead-letter
// ---------------------------------------------------------------------------

func TestIntegration_Consumer_RetryThenDeadLetter(t *testing.T) {
	pool := itPool(t)

	jobID, certUUID := seedJobRow(t, pool, itRecipient, big.NewInt(22222),
		"ipfs://bafk/integration-test/2.json")

	client := &fakeEthClient{minedErr: errors.New("rpc down")}
	signer := itNewSigner(t)
	dlq := &dlqFake{}
	maxAttempts := 3

	c, err := certpkg.NewConsumer(certpkg.ConsumerConfig{
		Pool:         pool,
		Signer:       signer,
		Client:       client,
		DLQ:          dlq,
		ChainID:      itChainID,
		PollInterval: itPollFast,
		MaxAttempts:  maxAttempts,
		ConfirmDepth: itConfirm,
		Logger:       itLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < maxAttempts; i++ {
		if _, err := pool.Exec(context.Background(),
			`UPDATE certificate_jobs SET next_retry_at = now() WHERE id=$1`, jobID); err != nil {
			t.Fatal(err)
		}
		c.HandleJobForTest(context.Background(), jobID, certUUID, i)
	}

	if got := itJobStatus(t, pool, jobID); got != "dead" {
		t.Fatalf("job status = %q, want dead", got)
	}
	if got := itCertStatus(t, pool, certUUID); got != "dead" {
		t.Fatalf("cert status = %q, want dead", got)
	}
	if len(dlq.entries) == 0 {
		t.Fatal("dlq not written")
	}
}

// ---------------------------------------------------------------------------
// Test：idempotency — pre-confirmed job 不会被 consumer 再次 claim
// ---------------------------------------------------------------------------

func TestIntegration_Consumer_Idempotency(t *testing.T) {
	pool := itPool(t)

	jobID, certUUID := seedJobRow(t, pool, itRecipient, big.NewInt(33333),
		"ipfs://bafk/integration-test/3.json")

	// 1) 第一次走通；2) 第二次 claim 应不命中（status!='pending'）。
	client := &fakeEthClient{minedStatus: 1}
	signer := itNewSigner(t)
	dlq := &dlqFake{}
	c, err := certpkg.NewConsumer(certpkg.ConsumerConfig{
		Pool:         pool,
		Signer:       signer,
		Client:       client,
		DLQ:          dlq,
		ChainID:      itChainID,
		PollInterval: itPollFast,
		MaxAttempts:  3,
		ConfirmDepth: itConfirm,
		Logger:       itLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	c.HandleJobForTest(context.Background(), jobID, certUUID, 0)
	if got := itJobStatus(t, pool, jobID); got != "confirmed" {
		t.Fatalf("1st pass status = %q", got)
	}

	// 直接调用 runOnce 看是否 claim 到任何 job（应为空）。
	preBC := client.broadcastCalls
	c.RunOnceForTest(context.Background())
	postBC := client.broadcastCalls
	if postBC != preBC {
		t.Fatalf("idempotency violated: broadcastCalls %d -> %d", preBC, postBC)
	}
}

// ---------------------------------------------------------------------------
// local fakes — dlqFake 不落库。
// ---------------------------------------------------------------------------

type dlqFake struct {
	entries []reconcile.Entry
}

func (f *dlqFake) Write(_ context.Context, e reconcile.Entry) (int64, error) {
	f.entries = append(f.entries, e)
	return int64(len(f.entries)), nil
}