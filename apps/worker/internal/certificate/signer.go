// Package certificate 提供证书 mint 的签名抽象（F04-T10）。
//
// 设计目标：worker 只负责「构造 + 签名」交易，**不负责广播**——广播由调用方
// 用自己的 ethclient 做，方便重试 / DLQ 逻辑（见 F04-T11 consumer）复用同一笔
// 已签名交易（同 nonce 重发是幂等的）。
//
// 三种 driver 由 SIGNER_DRIVER 选择：
//
//	anvil    — 本地开发 / 集成测试；私钥明文来自 SIGNER_ANVIL_PRIVATE_KEY
//	keystore — staging；Web3 Secret Storage 文件 + 口令（AWS Secrets Manager 注入）
//	kms      — 生产；AWS KMS 非对称密钥（ECC_SECG_P256K1 + ECDSA_SHA_256）
//
// 私钥永不落 DB、永不进日志；Address() 在构造时算一次并缓存，热路径无解密开销。
package certificate

import (
	"context"
	"crypto/ecdsa"
	"encoding/asn1"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
)

// Sentinel errors —— 与 internal/chain、internal/order 保持同一风格（errors.New +
// fmt.Errorf %w 包装），调用方用 errors.Is 分类到 jobs.failure_code。
var (
	ErrSignerUnavailable = errors.New("certificate: signer unavailable")
	ErrInvalidAddress    = errors.New("certificate: invalid address")
	ErrSignFailed        = errors.New("certificate: sign failed")
	ErrUnsupportedDriver = errors.New("certificate: unsupported signer driver")
	ErrKMSNotConfigured  = errors.New("certificate: kms not configured")

	// ErrSignerMisconfigured 表示配置本身危险或自相矛盾——不是「暂时不可用」，
	// 重试没有意义，调用方应当 fail-fast 让进程起不来。典型场景：
	//   - 非 anvil driver 却检测到 SIGNER_ANVIL_PRIVATE_KEY 残留（明文私钥泄漏面）；
	//   - 派生地址与 CERT_MINTER_ADDRESS 不符（签出来的交易必然没有 MINTER_ROLE）。
	ErrSignerMisconfigured = errors.New("certificate: signer misconfigured")
)

// Driver 名称常量。
const (
	DriverKeystore = "keystore"
	DriverKMS      = "kms"
	DriverAnvil    = "anvil"
)

// mintCertificateABI 是 CertificateNFT 中我们唯一需要编码的函数。
//
// 只内联这一个片段而不读取 packages/contracts/out/CertificateNFT.sol/CertificateNFT.json，
// 是因为 worker 的生产镜像里不含 forge 产物；签名是安全关键路径，函数签名硬编码后
// 由合约端单测锁定：packages/contracts/test/CertificateNFT.t.sol 里的
// test_MintCertificateSelectorMatchesSignerABI 会在 mintCertificate 签名变更时红，
// 提示同步更新此常量（selector 0xa70d0e45）。
const mintCertificateABI = `[{"type":"function","name":"mintCertificate","stateMutability":"nonpayable",` +
	`"inputs":[{"name":"to","type":"address"},{"name":"certificateId","type":"uint256"},` +
	`{"name":"uri","type":"string"}],"outputs":[]}]`

var certABI abi.ABI

func init() {
	parsed, err := abi.JSON(strings.NewReader(mintCertificateABI))
	if err != nil {
		panic(fmt.Sprintf("certificate: bad builtin ABI: %v", err))
	}
	certABI = parsed
}

// MintSigner 是证书铸造签名器的统一抽象。
type MintSigner interface {
	// Address 返回签名账户地址（必须持有 CertificateNFT 的 MINTER_ROLE）。
	Address() common.Address

	// SignCertificateMint 构造并签名 mintCertificate(to, certID, uri) 交易。
	// 返回已签名交易，由调用方广播。
	SignCertificateMint(ctx context.Context, certID *big.Int, to common.Address, uri string) (*types.Transaction, error)
}

// TxParams 是构造一笔 EIP-1559 交易所需的链上动态参数。
type TxParams struct {
	Nonce     uint64
	GasLimit  uint64
	GasFeeCap *big.Int // maxFeePerGas
	GasTipCap *big.Int // maxPriorityFeePerGas
}

// TxParamsProvider 由调用方注入（通常包一层 ethclient：PendingNonceAt +
// SuggestGasTipCap + HeaderByNumber）。签名器本身不持有 RPC client，
// 这样单测无需起 anvil 即可覆盖签名逻辑。
type TxParamsProvider interface {
	TxParams(ctx context.Context, from common.Address) (TxParams, error)
}

// StaticTxParams 是固定参数实现，用于本地开发与单测。
type StaticTxParams struct{ Params TxParams }

// TxParams 实现 TxParamsProvider。
func (s StaticTxParams) TxParams(context.Context, common.Address) (TxParams, error) {
	p := s.Params
	if p.GasLimit == 0 {
		p.GasLimit = defaultGasLimit
	}
	if p.GasFeeCap == nil {
		p.GasFeeCap = big.NewInt(defaultGasFeeCapWei)
	}
	if p.GasTipCap == nil {
		p.GasTipCap = big.NewInt(defaultGasTipCapWei)
	}
	return p, nil
}

const (
	// mintCertificate ≈ 150k gas（ERC721 _mint + 两个 SSTORE + string URI），留足冗余。
	defaultGasLimit     = 300_000
	defaultGasFeeCapWei = 50_000_000_000 // 50 gwei
	defaultGasTipCapWei = 2_000_000_000  // 2 gwei
)

// SignerConfig 汇总三种 driver 的配置；未使用的字段留空即可。
type SignerConfig struct {
	Driver   string
	ChainID  *big.Int
	Contract common.Address // CertificateNFT 地址（CERT_NFT_ADDRESS）

	// keystore driver
	KeystorePath     string
	KeystorePassword string

	// kms driver
	KMSKeyID  string
	KMSClient KMSAPI // 可注入 mock；nil 时按默认 AWS 配置构造

	// anvil driver
	AnvilPrivateKey string

	// ExpectedAddress 是运维侧声明的「这个签名器应该是谁」（CERT_MINTER_ADDRESS，
	// 即链上被授予 MINTER_ROLE 的地址）。非零时，所有 driver 在构造阶段校验派生地址
	// 与之一致，不一致直接 ErrSignerMisconfigured——把「换错 keystore / 指错 KMS key」
	// 这类事故从「链上 revert + job 反复重试」提前到进程启动即失败。
	// 留空则跳过校验（本地开发）。
	ExpectedAddress common.Address

	// 可选：交易参数来源；nil 时用 StaticTxParams{}（仅适合本地/单测）。
	Params TxParamsProvider
}

// checkExpectedAddress 校验派生地址与运维声明的 CERT_MINTER_ADDRESS 一致。
// ExpectedAddress 为零值时视为未声明，跳过。
func checkExpectedAddress(cfg SignerConfig, got common.Address) error {
	if cfg.ExpectedAddress == (common.Address{}) {
		return nil
	}
	if got != cfg.ExpectedAddress {
		return fmt.Errorf(
			"%w: derived signer address %s != CERT_MINTER_ADDRESS %s",
			ErrSignerMisconfigured, got.Hex(), cfg.ExpectedAddress.Hex(),
		)
	}
	return nil
}

// rejectLeakedAnvilKey 供非 anvil driver 调用：staging/prod 里出现明文私钥环境变量，
// 说明部署配置串了（例如复用了本地 .env），必须 fail-fast 而不是默默留在内存里。
func rejectLeakedAnvilKey(cfg SignerConfig, driver string) error {
	if strings.TrimSpace(cfg.AnvilPrivateKey) != "" {
		return fmt.Errorf(
			"%w: SIGNER_ANVIL_PRIVATE_KEY must not be set when SIGNER_DRIVER=%s",
			ErrSignerMisconfigured, driver,
		)
	}
	return nil
}

// SignerConfigFromEnv 从环境变量装配 SignerConfig。
//
// 防御性处理：只有 driver 明确为 anvil 时才保留 SIGNER_ANVIL_PRIVATE_KEY。
// 其余 driver（含空值默认的 keystore）下即使环境变量存在也不进结构体，
// 避免明文私钥被顺手带进进程内存 / panic dump / %+v 日志。
// 注意这只是纵深防御的第一层：driver 构造函数还会独立 fail-fast（见
// rejectLeakedAnvilKey），因为调用方可能绕过本函数直接填 SignerConfig。
func SignerConfigFromEnv(chainID *big.Int) SignerConfig {
	driver := strings.ToLower(strings.TrimSpace(os.Getenv("SIGNER_DRIVER")))

	cfg := SignerConfig{
		Driver:           driver,
		ChainID:          chainID,
		Contract:         common.HexToAddress(os.Getenv("CERT_NFT_ADDRESS")),
		ExpectedAddress:  common.HexToAddress(os.Getenv("CERT_MINTER_ADDRESS")),
		KeystorePath:     os.Getenv("SIGNER_KEYSTORE_PATH"),
		KeystorePassword: os.Getenv("SIGNER_KEYSTORE_PASSWORD"),
		KMSKeyID:         os.Getenv("SIGNER_KMS_KEY_ID"),
	}
	if driver == DriverAnvil {
		cfg.AnvilPrivateKey = os.Getenv("SIGNER_ANVIL_PRIVATE_KEY")
	}
	return cfg
}

// NewMintSigner 是 driver 工厂。Driver 为空时默认 keystore（staging 默认）。
func NewMintSigner(ctx context.Context, cfg SignerConfig) (MintSigner, error) {
	if cfg.ChainID == nil || cfg.ChainID.Sign() <= 0 {
		return nil, fmt.Errorf("%w: chain id required", ErrSignerUnavailable)
	}
	if cfg.Contract == (common.Address{}) {
		return nil, fmt.Errorf("%w: CERT_NFT_ADDRESS is zero", ErrInvalidAddress)
	}
	if cfg.Params == nil {
		cfg.Params = StaticTxParams{}
	}

	switch strings.ToLower(strings.TrimSpace(cfg.Driver)) {
	case "", DriverKeystore:
		return NewKeystoreDriver(cfg)
	case DriverAnvil:
		return NewAnvilDriver(cfg)
	case DriverKMS:
		return NewKMSDriver(ctx, cfg)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedDriver, cfg.Driver)
	}
}

// ---------------------------------------------------------------------------
// 共享构造逻辑
// ---------------------------------------------------------------------------

// buildMintTx 组装未签名的 DynamicFeeTx。三种 driver 共用，保证 calldata 一致。
func buildMintTx(
	ctx context.Context,
	cfg SignerConfig,
	from common.Address,
	certID *big.Int,
	to common.Address,
	uri string,
) (*types.Transaction, error) {
	if to == (common.Address{}) {
		return nil, fmt.Errorf("%w: recipient is zero address", ErrInvalidAddress)
	}
	if certID == nil || certID.Sign() < 0 {
		return nil, fmt.Errorf("%w: certificate id must be non-negative", ErrSignFailed)
	}

	data, err := certABI.Pack("mintCertificate", to, certID, uri)
	if err != nil {
		return nil, fmt.Errorf("%w: pack calldata: %v", ErrSignFailed, err)
	}

	p, err := cfg.Params.TxParams(ctx, from)
	if err != nil {
		return nil, fmt.Errorf("%w: tx params: %v", ErrSignerUnavailable, err)
	}
	if p.GasLimit == 0 {
		p.GasLimit = defaultGasLimit
	}
	if p.GasFeeCap == nil {
		p.GasFeeCap = big.NewInt(defaultGasFeeCapWei)
	}
	if p.GasTipCap == nil {
		p.GasTipCap = big.NewInt(defaultGasTipCapWei)
	}

	contract := cfg.Contract
	return types.NewTx(&types.DynamicFeeTx{
		ChainID:   new(big.Int).Set(cfg.ChainID),
		Nonce:     p.Nonce,
		GasTipCap: p.GasTipCap,
		GasFeeCap: p.GasFeeCap,
		Gas:       p.GasLimit,
		To:        &contract,
		Value:     big.NewInt(0),
		Data:      data,
	}), nil
}

// signWithKey 用本地 ECDSA 私钥签名（keystore / anvil 共用）。
//
// 签完立刻做一次「自检」：把签名还原成发送方地址，必须等于缓存的 expect。
// 成本 ~100µs 的 ecrecover，换掉一整类静默故障——chainID 传错、types.SignTx
// 内部签名器与后续广播用的签名器不一致时，交易会带着错误的 from 上链并被
// 节点以 nonce/权限错误拒绝，而 worker 只能看到一个没有上下文的 revert。
func signWithKey(
	tx *types.Transaction, chainID *big.Int, key *ecdsa.PrivateKey, expect common.Address,
) (*types.Transaction, error) {
	signer := types.LatestSignerForChainID(chainID)
	signed, err := types.SignTx(tx, signer, key)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSignFailed, err)
	}
	if err := assertRecoversTo(signer, signed, expect); err != nil {
		return nil, err
	}
	return signed, nil
}

// assertRecoversTo 校验已签名交易能还原出预期的发送方地址。
func assertRecoversTo(signer types.Signer, signed *types.Transaction, expect common.Address) error {
	got, err := types.Sender(signer, signed)
	if err != nil {
		return fmt.Errorf("%w: self-test: recover sender: %v", ErrSignFailed, err)
	}
	if got != expect {
		return fmt.Errorf(
			"%w: self-test: signature recovers to %s, want %s",
			ErrSignFailed, got.Hex(), expect.Hex(),
		)
	}
	return nil
}

// ---------------------------------------------------------------------------
// AnvilDriver —— 本地开发 / 集成测试
// ---------------------------------------------------------------------------

// AnvilDriver 直接持有明文私钥。**禁止用于 staging / production**。
type AnvilDriver struct {
	cfg  SignerConfig
	key  *ecdsa.PrivateKey
	addr common.Address
}

// NewAnvilDriver 从 SIGNER_ANVIL_PRIVATE_KEY（可带 0x 前缀）构造。
func NewAnvilDriver(cfg SignerConfig) (*AnvilDriver, error) {
	hexKey := strings.TrimPrefix(strings.TrimSpace(cfg.AnvilPrivateKey), "0x")
	if hexKey == "" {
		return nil, fmt.Errorf("%w: SIGNER_ANVIL_PRIVATE_KEY is empty", ErrSignerUnavailable)
	}
	key, err := crypto.HexToECDSA(hexKey)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid anvil private key", ErrSignerUnavailable)
	}
	if cfg.Params == nil {
		cfg.Params = StaticTxParams{}
	}
	addr := crypto.PubkeyToAddress(key.PublicKey)
	if err := checkExpectedAddress(cfg, addr); err != nil {
		return nil, err
	}
	return &AnvilDriver{cfg: cfg, key: key, addr: addr}, nil
}

// Address 实现 MintSigner。
func (d *AnvilDriver) Address() common.Address { return d.addr }

// SignCertificateMint 实现 MintSigner。
func (d *AnvilDriver) SignCertificateMint(
	ctx context.Context, certID *big.Int, to common.Address, uri string,
) (*types.Transaction, error) {
	tx, err := buildMintTx(ctx, d.cfg, d.addr, certID, to, uri)
	if err != nil {
		return nil, err
	}
	return signWithKey(tx, d.cfg.ChainID, d.key, d.addr)
}

// ---------------------------------------------------------------------------
// KeystoreDriver —— staging
// ---------------------------------------------------------------------------

// KeystoreDriver 在构造时一次性解密 keystore 文件，之后把私钥常驻内存。
//
// 取舍：常驻内存换掉每次签名的 scrypt KDF（~100ms+），mint 吞吐才够用；
// 进程退出即消失，不写盘、不进日志。
type KeystoreDriver struct {
	cfg  SignerConfig
	key  *ecdsa.PrivateKey
	addr common.Address
}

// NewKeystoreDriver 读取 SIGNER_KEYSTORE_PATH 并用 SIGNER_KEYSTORE_PASSWORD 解密。
func NewKeystoreDriver(cfg SignerConfig) (*KeystoreDriver, error) {
	if err := rejectLeakedAnvilKey(cfg, DriverKeystore); err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.KeystorePath) == "" {
		return nil, fmt.Errorf("%w: SIGNER_KEYSTORE_PATH is empty", ErrSignerUnavailable)
	}
	blob, err := os.ReadFile(cfg.KeystorePath)
	if err != nil {
		return nil, fmt.Errorf("%w: read keystore: %v", ErrSignerUnavailable, err)
	}
	acct, err := keystore.DecryptKey(blob, cfg.KeystorePassword)
	if err != nil {
		// 不回显 err 详情以外的内容；口令本身绝不进日志。
		return nil, fmt.Errorf("%w: decrypt keystore: %v", ErrSignerUnavailable, err)
	}
	if cfg.Params == nil {
		cfg.Params = StaticTxParams{}
	}
	addr := crypto.PubkeyToAddress(acct.PrivateKey.PublicKey)
	if err := checkExpectedAddress(cfg, addr); err != nil {
		return nil, err
	}
	return &KeystoreDriver{
		cfg:  cfg,
		key:  acct.PrivateKey,
		addr: addr,
	}, nil
}

// Address 实现 MintSigner。
func (d *KeystoreDriver) Address() common.Address { return d.addr }

// SignCertificateMint 实现 MintSigner。
func (d *KeystoreDriver) SignCertificateMint(
	ctx context.Context, certID *big.Int, to common.Address, uri string,
) (*types.Transaction, error) {
	tx, err := buildMintTx(ctx, d.cfg, d.addr, certID, to, uri)
	if err != nil {
		return nil, err
	}
	return signWithKey(tx, d.cfg.ChainID, d.key, d.addr)
}

// ---------------------------------------------------------------------------
// KMSDriver —— production
// ---------------------------------------------------------------------------

// KMSAPI 抽出我们用到的两个 KMS 调用，便于单测注入 mock。
type KMSAPI interface {
	GetPublicKey(ctx context.Context, in *kms.GetPublicKeyInput, opts ...func(*kms.Options)) (*kms.GetPublicKeyOutput, error)
	Sign(ctx context.Context, in *kms.SignInput, opts ...func(*kms.Options)) (*kms.SignOutput, error)
}

// KMSDriver 用 AWS KMS 非对称密钥签名，私钥永不出 HSM。
//
// AWS 侧前置条件（terraform 见 infra/）：
//
//  1. 创建 KMS key：KeySpec = ECC_SECG_P256K1，KeyUsage = SIGN_VERIFY。
//     （注意：只有 secp256k1 曲线可用于以太坊；P-256 无法恢复出 EOA 地址。）
//  2. worker 的 IAM role 授予 kms:Sign + kms:GetPublicKey。
//  3. 把派生出的地址（启动日志会打印 Address()）授予 CertificateNFT 的 MINTER_ROLE。
//
// 实现要点（KMS 与以太坊签名格式的三处差异）：
//
//   - KMS 返回 **DER 编码**的 (R, S)，需要拆成 32+32 字节定长；
//   - KMS 可能返回 high-S；以太坊（EIP-2）只接受 low-S，需要 S = N - S 归一化；
//   - KMS **不返回 recovery id (v)**，只能靠已缓存的公钥试 v=0/1 反推。
//
// 已知限制：
//   - MessageType 只能用 DIGEST（我们本来就签 32 字节 sighash），因此 SigningAlgorithm
//     固定 ECDSA_SHA_256——KMS 在 DIGEST 模式下不会二次哈希，与 keccak256 sighash 兼容。
//   - 每次签名一次网络往返（~20-40ms），高吞吐场景需要在 consumer 侧限流。
//   - 单测不覆盖真实 AWS：CI 无凭据，只验证「未配置时构造函数报错」。
type KMSDriver struct {
	cfg    SignerConfig
	client KMSAPI
	keyID  string
	pub    *ecdsa.PublicKey
	addr   common.Address

	// selfTests 统计通过「签名可还原为 d.addr」自检的签名次数。
	// 原子计数：SignCertificateMint 可被多个 consumer goroutine 并发调用。
	// 暴露给 metrics / 启动探针，用于确认 KMS 密钥确实是我们以为的那把。
	selfTests atomic.Uint64
}

// NewKMSDriver 构造 KMS 签名器；会立即调用 GetPublicKey 派生并缓存地址。
func NewKMSDriver(ctx context.Context, cfg SignerConfig) (*KMSDriver, error) {
	if err := rejectLeakedAnvilKey(cfg, DriverKMS); err != nil {
		return nil, err
	}
	keyID := strings.TrimSpace(cfg.KMSKeyID)
	if keyID == "" {
		return nil, fmt.Errorf("%w: SIGNER_KMS_KEY_ID is empty", ErrKMSNotConfigured)
	}
	client := cfg.KMSClient
	if client == nil {
		awsCfg, err := awsconfig.LoadDefaultConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: load aws config: %v", ErrKMSNotConfigured, err)
		}
		client = kms.NewFromConfig(awsCfg)
	}
	if cfg.Params == nil {
		cfg.Params = StaticTxParams{}
	}

	out, err := client.GetPublicKey(ctx, &kms.GetPublicKeyInput{KeyId: aws.String(keyID)})
	if err != nil {
		return nil, fmt.Errorf("%w: GetPublicKey: %v", ErrKMSNotConfigured, err)
	}
	pub, err := parseKMSPublicKey(out.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKMSNotConfigured, err)
	}

	addr := crypto.PubkeyToAddress(*pub)
	if err := checkExpectedAddress(cfg, addr); err != nil {
		return nil, err
	}

	return &KMSDriver{
		cfg:    cfg,
		client: client,
		keyID:  keyID,
		pub:    pub,
		addr:   addr,
	}, nil
}

// Address 实现 MintSigner。
func (d *KMSDriver) Address() common.Address { return d.addr }

// SelfTestCount 返回已通过签名自检的次数（见 SignCertificateMint）。
// 供 metrics / 健康检查读取：稳态下应与成功签名数相等。
func (d *KMSDriver) SelfTestCount() uint64 { return d.selfTests.Load() }

// SignCertificateMint 实现 MintSigner。
func (d *KMSDriver) SignCertificateMint(
	ctx context.Context, certID *big.Int, to common.Address, uri string,
) (*types.Transaction, error) {
	tx, err := buildMintTx(ctx, d.cfg, d.addr, certID, to, uri)
	if err != nil {
		return nil, err
	}
	signer := types.LatestSignerForChainID(d.cfg.ChainID)
	digest := signer.Hash(tx) // keccak256 sighash，32 字节

	out, err := d.client.Sign(ctx, &kms.SignInput{
		KeyId:            aws.String(d.keyID),
		Message:          digest.Bytes(),
		MessageType:      kmstypes.MessageTypeDigest,
		SigningAlgorithm: kmstypes.SigningAlgorithmSpecEcdsaSha256,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: kms sign: %v", ErrSignFailed, err)
	}

	sig, err := kmsSigToEthSig(out.Signature, digest.Bytes(), d.addr)
	if err != nil {
		return nil, err
	}
	signed, err := tx.WithSignature(signer, sig)
	if err != nil {
		return nil, fmt.Errorf("%w: attach signature: %v", ErrSignFailed, err)
	}
	// 自检：对最终交易再跑一次 ecrecover。kmsSigToEthSig 里的 v 试探只证明
	// 「(r,s,v) 能还原出 d.addr」；这里证明的是「WithSignature 之后、即将广播的
	// 那笔交易」也能——两者之间隔着 RLP 组装，值得多花一次 ecrecover 兜住。
	if err := assertRecoversTo(signer, signed, d.addr); err != nil {
		return nil, err
	}
	d.selfTests.Add(1)
	return signed, nil
}

// secp256k1N / halfN 用于 low-S 归一化（EIP-2）。
var (
	secp256k1N     = crypto.S256().Params().N
	secp256k1HalfN = new(big.Int).Rsh(secp256k1N, 1)
)

// ecdsaDERSig 对应 RFC 3279 的 ECDSA-Sig-Value SEQUENCE { r INTEGER, s INTEGER }。
type ecdsaDERSig struct {
	R *big.Int
	S *big.Int
}

// kmsSigToEthSig 把 KMS 的 DER 签名转成以太坊的 65 字节 [R||S||V]。
func kmsSigToEthSig(der, digest []byte, expect common.Address) ([]byte, error) {
	var parsed ecdsaDERSig
	if _, err := asn1.Unmarshal(der, &parsed); err != nil {
		return nil, fmt.Errorf("%w: decode DER signature: %v", ErrSignFailed, err)
	}
	r, s := parsed.R, parsed.S
	if r == nil || s == nil || r.Sign() <= 0 || s.Sign() <= 0 {
		return nil, fmt.Errorf("%w: malformed (r,s)", ErrSignFailed)
	}
	// EIP-2：high-S 会被节点拒绝，翻转到 low 半区（(r, N-s) 同样是合法签名）。
	if s.Cmp(secp256k1HalfN) > 0 {
		s = new(big.Int).Sub(secp256k1N, s)
	}

	sig := make([]byte, 65)
	r.FillBytes(sig[0:32])
	s.FillBytes(sig[32:64])

	// KMS 不返回 recovery id：试 v=0/1，取能恢复出缓存地址的那个。
	for v := byte(0); v < 2; v++ {
		sig[64] = v
		pub, err := crypto.SigToPub(digest, sig)
		if err != nil {
			continue
		}
		if crypto.PubkeyToAddress(*pub) == expect {
			return sig, nil
		}
	}
	return nil, fmt.Errorf("%w: cannot recover signer address from kms signature", ErrSignFailed)
}

// parseKMSPublicKey 从 KMS 返回的 DER SPKI 中提取 secp256k1 公钥。
//
// 不能用 x509.ParsePKIXPublicKey——Go 标准库不认 secp256k1 的 OID
// (1.3.132.0.10)，会直接报 unsupported elliptic curve。这里手工拆
// SubjectPublicKeyInfo ::= SEQUENCE { algorithm AlgorithmIdentifier, subjectPublicKey BIT STRING }。
func parseKMSPublicKey(der []byte) (*ecdsa.PublicKey, error) {
	if len(der) == 0 {
		return nil, errors.New("empty kms public key")
	}
	var spki struct {
		Algorithm asn1.RawValue
		PublicKey asn1.BitString
	}
	if _, err := asn1.Unmarshal(der, &spki); err != nil {
		return nil, fmt.Errorf("decode SPKI: %w", err)
	}
	raw := spki.PublicKey.Bytes
	if len(raw) != 65 || raw[0] != 0x04 {
		return nil, fmt.Errorf("expected 65-byte uncompressed secp256k1 point, got %d bytes", len(raw))
	}
	pub, err := crypto.UnmarshalPubkey(raw)
	if err != nil {
		return nil, fmt.Errorf("unmarshal secp256k1 pubkey: %w", err)
	}
	return pub, nil
}
