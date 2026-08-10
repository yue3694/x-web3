package certificate

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/asn1"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// anvil account #0 —— 公开测试私钥，非真实资产。
const anvilPK = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

var (
	testChainID  = big.NewInt(31337)
	testContract = common.HexToAddress("0x5FbDB2315678afecb367f032d93F642f64180aa3")
	testTo       = common.HexToAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79C8")
	testURI      = "ipfs://bafyfixture/1.json"
	// anvilPK 对应的已知地址（anvil account #0）。
	anvilAddr = common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")
)

func baseCfg(driver string) SignerConfig {
	return SignerConfig{
		Driver:   driver,
		ChainID:  testChainID,
		Contract: testContract,
		Params:   StaticTxParams{Params: TxParams{Nonce: 7}},
	}
}

// recoverSender 从已签名交易中还原发送方地址。
func recoverSender(t *testing.T, tx *types.Transaction) common.Address {
	t.Helper()
	from, err := types.Sender(types.LatestSignerForChainID(testChainID), tx)
	if err != nil {
		t.Fatalf("recover sender: %v", err)
	}
	return from
}

func assertMintTx(t *testing.T, tx *types.Transaction) {
	t.Helper()
	if tx.To() == nil || *tx.To() != testContract {
		t.Fatalf("tx.To = %v, want %v", tx.To(), testContract)
	}
	if tx.Nonce() != 7 {
		t.Fatalf("nonce = %d, want 7", tx.Nonce())
	}
	if tx.Value().Sign() != 0 {
		t.Fatalf("value = %s, want 0", tx.Value())
	}
	method, err := certABI.MethodById(tx.Data()[:4])
	if err != nil || method.Name != "mintCertificate" {
		t.Fatalf("calldata selector mismatch: %v %v", method, err)
	}
	args, err := method.Inputs.Unpack(tx.Data()[4:])
	if err != nil {
		t.Fatalf("unpack calldata: %v", err)
	}
	if args[0].(common.Address) != testTo {
		t.Fatalf("to = %v, want %v", args[0], testTo)
	}
	if args[1].(*big.Int).Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("certID = %v, want 42", args[1])
	}
	if args[2].(string) != testURI {
		t.Fatalf("uri = %v, want %v", args[2], testURI)
	}
}

// ---------------------------------------------------------------------------
// AnvilDriver
// ---------------------------------------------------------------------------

func TestAnvilDriver_SignRecoversAddress(t *testing.T) {
	cfg := baseCfg(DriverAnvil)
	cfg.AnvilPrivateKey = "0x" + anvilPK

	d, err := NewAnvilDriver(cfg)
	if err != nil {
		t.Fatalf("NewAnvilDriver: %v", err)
	}
	// anvil account #0 的已知地址。
	want := anvilAddr
	if d.Address() != want {
		t.Fatalf("Address = %v, want %v", d.Address(), want)
	}

	tx, err := d.SignCertificateMint(context.Background(), big.NewInt(42), testTo, testURI)
	if err != nil {
		t.Fatalf("SignCertificateMint: %v", err)
	}
	assertMintTx(t, tx)
	if got := recoverSender(t, tx); got != d.Address() {
		t.Fatalf("recovered %v, want %v", got, d.Address())
	}
}

func TestAnvilDriver_AcceptsKeyWithoutPrefix(t *testing.T) {
	cfg := baseCfg(DriverAnvil)
	cfg.AnvilPrivateKey = anvilPK
	if _, err := NewAnvilDriver(cfg); err != nil {
		t.Fatalf("no-prefix key rejected: %v", err)
	}
}

func TestAnvilDriver_ErrorsOnMissingKey(t *testing.T) {
	if _, err := NewAnvilDriver(baseCfg(DriverAnvil)); !errors.Is(err, ErrSignerUnavailable) {
		t.Fatalf("err = %v, want ErrSignerUnavailable", err)
	}
}

func TestSignCertificateMint_RejectsZeroRecipient(t *testing.T) {
	cfg := baseCfg(DriverAnvil)
	cfg.AnvilPrivateKey = anvilPK
	d, err := NewAnvilDriver(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.SignCertificateMint(context.Background(), big.NewInt(1), common.Address{}, testURI)
	if !errors.Is(err, ErrInvalidAddress) {
		t.Fatalf("err = %v, want ErrInvalidAddress", err)
	}
}

func TestSignCertificateMint_RejectsNegativeCertID(t *testing.T) {
	cfg := baseCfg(DriverAnvil)
	cfg.AnvilPrivateKey = anvilPK
	d, _ := NewAnvilDriver(cfg)
	if _, err := d.SignCertificateMint(context.Background(), big.NewInt(-1), testTo, testURI); !errors.Is(err, ErrSignFailed) {
		t.Fatalf("err = %v, want ErrSignFailed", err)
	}
}

// ---------------------------------------------------------------------------
// KeystoreDriver
// ---------------------------------------------------------------------------

// newTempKeystore 在 t.TempDir()（测试结束自动删除）里生成一个 keystore 文件。
func newTempKeystore(t *testing.T, password string) (path string, addr common.Address) {
	t.Helper()
	dir := t.TempDir()
	// LightScryptN/P 让单测别跑几秒；生产用 StandardScryptN。
	ks := keystore.NewKeyStore(dir, keystore.LightScryptN, keystore.LightScryptP)
	acct, err := ks.NewAccount(password)
	if err != nil {
		t.Fatalf("create keystore account: %v", err)
	}
	return acct.URL.Path, acct.Address
}

func TestKeystoreDriver_SignRecoversAddress(t *testing.T) {
	const pw = "correct horse battery staple"
	path, addr := newTempKeystore(t, pw)

	cfg := baseCfg(DriverKeystore)
	cfg.KeystorePath = path
	cfg.KeystorePassword = pw

	d, err := NewKeystoreDriver(cfg)
	if err != nil {
		t.Fatalf("NewKeystoreDriver: %v", err)
	}
	if d.Address() != addr {
		t.Fatalf("Address = %v, want %v", d.Address(), addr)
	}

	tx, err := d.SignCertificateMint(context.Background(), big.NewInt(42), testTo, testURI)
	if err != nil {
		t.Fatalf("SignCertificateMint: %v", err)
	}
	assertMintTx(t, tx)
	if got := recoverSender(t, tx); got != addr {
		t.Fatalf("recovered %v, want %v", got, addr)
	}

	// 显式清理（t.TempDir 也会删，这里验证文件确实存在过）。
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("keystore file missing: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove temp keystore: %v", err)
	}
}

func TestKeystoreDriver_ErrorsOnWrongPassword(t *testing.T) {
	path, _ := newTempKeystore(t, "right")
	cfg := baseCfg(DriverKeystore)
	cfg.KeystorePath = path
	cfg.KeystorePassword = "wrong"
	if _, err := NewKeystoreDriver(cfg); !errors.Is(err, ErrSignerUnavailable) {
		t.Fatalf("err = %v, want ErrSignerUnavailable", err)
	}
}

func TestKeystoreDriver_ErrorsOnMissingFile(t *testing.T) {
	cfg := baseCfg(DriverKeystore)
	cfg.KeystorePath = filepath.Join(t.TempDir(), "nope.json")
	if _, err := NewKeystoreDriver(cfg); !errors.Is(err, ErrSignerUnavailable) {
		t.Fatalf("err = %v, want ErrSignerUnavailable", err)
	}
}

// ---------------------------------------------------------------------------
// KMSDriver —— 真实 AWS 调用在单测中跳过，只覆盖格式转换与未配置分支
// ---------------------------------------------------------------------------

// fakeKMS 用本地 secp256k1 私钥模拟 KMS 的 DER 输出，覆盖
// SPKI 解析 + DER→[R||S||V] + low-S 归一化路径。
type fakeKMS struct {
	key        *ecdsa.PrivateKey
	forceHighS bool
}

func (f *fakeKMS) GetPublicKey(context.Context, *kms.GetPublicKeyInput, ...func(*kms.Options)) (*kms.GetPublicKeyOutput, error) {
	raw := crypto.FromECDSAPub(&f.key.PublicKey)
	// 组装最小 SPKI：SEQUENCE { AlgorithmIdentifier, BIT STRING }。
	algo, err := asn1.Marshal(struct{ A, B asn1.ObjectIdentifier }{
		A: asn1.ObjectIdentifier{1, 2, 840, 10045, 2, 1}, // id-ecPublicKey
		B: asn1.ObjectIdentifier{1, 3, 132, 0, 10},       // secp256k1
	})
	if err != nil {
		return nil, err
	}
	der, err := asn1.Marshal(struct {
		Algorithm asn1.RawValue
		PublicKey asn1.BitString
	}{
		Algorithm: asn1.RawValue{FullBytes: algo},
		PublicKey: asn1.BitString{Bytes: raw, BitLength: len(raw) * 8},
	})
	if err != nil {
		return nil, err
	}
	return &kms.GetPublicKeyOutput{PublicKey: der}, nil
}

func (f *fakeKMS) Sign(_ context.Context, in *kms.SignInput, _ ...func(*kms.Options)) (*kms.SignOutput, error) {
	r, s, err := ecdsa.Sign(rand.Reader, f.key, in.Message)
	if err != nil {
		return nil, err
	}
	if f.forceHighS && s.Cmp(secp256k1HalfN) <= 0 {
		s = new(big.Int).Sub(secp256k1N, s) // 模拟 KMS 返回 high-S
	}
	der, err := asn1.Marshal(ecdsaDERSig{R: r, S: s})
	if err != nil {
		return nil, err
	}
	return &kms.SignOutput{Signature: der}, nil
}

func TestKMSDriver_SignRecoversAddress(t *testing.T) {
	for _, highS := range []bool{false, true} {
		key, err := crypto.GenerateKey()
		if err != nil {
			t.Fatal(err)
		}
		cfg := baseCfg(DriverKMS)
		cfg.KMSKeyID = "alias/x-web3-cert-minter"
		cfg.KMSClient = &fakeKMS{key: key, forceHighS: highS}

		d, err := NewKMSDriver(context.Background(), cfg)
		if err != nil {
			t.Fatalf("NewKMSDriver(highS=%v): %v", highS, err)
		}
		want := crypto.PubkeyToAddress(key.PublicKey)
		if d.Address() != want {
			t.Fatalf("Address = %v, want %v", d.Address(), want)
		}

		tx, err := d.SignCertificateMint(context.Background(), big.NewInt(42), testTo, testURI)
		if err != nil {
			t.Fatalf("SignCertificateMint(highS=%v): %v", highS, err)
		}
		assertMintTx(t, tx)
		if got := recoverSender(t, tx); got != want {
			t.Fatalf("recovered %v, want %v", got, want)
		}
		// EIP-2：S 必须落在低半区。
		_, _, s := tx.RawSignatureValues()
		if s.Cmp(secp256k1HalfN) > 0 {
			t.Fatalf("high-S signature leaked (highS=%v)", highS)
		}
	}
}

func TestKMSDriver_ErrorsWhenKeyIDUnset(t *testing.T) {
	t.Setenv("SIGNER_KMS_KEY_ID", "")
	if _, err := NewKMSDriver(context.Background(), baseCfg(DriverKMS)); !errors.Is(err, ErrKMSNotConfigured) {
		t.Fatalf("err = %v, want ErrKMSNotConfigured", err)
	}
}

func TestKMSDriver_SkipsRealAWS(t *testing.T) {
	if os.Getenv("KMS_INTEGRATION") == "" {
		t.Skip("skipping: real AWS KMS test requires KMS_INTEGRATION + AWS credentials")
	}
	cfg := baseCfg(DriverKMS)
	cfg.KMSKeyID = os.Getenv("SIGNER_KMS_KEY_ID")
	if _, err := NewKMSDriver(context.Background(), cfg); err != nil {
		t.Fatalf("NewKMSDriver against real AWS: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 工厂 / env dispatch
// ---------------------------------------------------------------------------

func TestNewMintSigner_Dispatch(t *testing.T) {
	ksPath, ksAddr := newTempKeystore(t, "pw")

	tests := []struct {
		name    string
		mutate  func(*SignerConfig)
		wantErr error
		wantAdr *common.Address
	}{
		{
			name:    "anvil",
			mutate:  func(c *SignerConfig) { c.Driver = DriverAnvil; c.AnvilPrivateKey = anvilPK },
			wantAdr: ptr(anvilAddr),
		},
		{
			name: "keystore",
			mutate: func(c *SignerConfig) {
				c.Driver = DriverKeystore
				c.KeystorePath, c.KeystorePassword = ksPath, "pw"
			},
			wantAdr: &ksAddr,
		},
		{
			name: "empty driver defaults to keystore",
			mutate: func(c *SignerConfig) {
				c.Driver = ""
				c.KeystorePath, c.KeystorePassword = ksPath, "pw"
			},
			wantAdr: &ksAddr,
		},
		{
			name:    "kms unconfigured",
			mutate:  func(c *SignerConfig) { c.Driver = DriverKMS },
			wantErr: ErrKMSNotConfigured,
		},
		{
			name:    "unknown driver",
			mutate:  func(c *SignerConfig) { c.Driver = "hsm" },
			wantErr: ErrUnsupportedDriver,
		},
		{
			name:    "zero contract address",
			mutate:  func(c *SignerConfig) { c.Driver = DriverAnvil; c.Contract = common.Address{} },
			wantErr: ErrInvalidAddress,
		},
		{
			name:    "missing chain id",
			mutate:  func(c *SignerConfig) { c.Driver = DriverAnvil; c.ChainID = nil },
			wantErr: ErrSignerUnavailable,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseCfg("")
			tc.mutate(&cfg)
			s, err := NewMintSigner(context.Background(), cfg)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewMintSigner: %v", err)
			}
			if s.Address() != *tc.wantAdr {
				t.Fatalf("Address = %v, want %v", s.Address(), *tc.wantAdr)
			}
		})
	}
}

func TestSignerConfigFromEnv(t *testing.T) {
	t.Setenv("SIGNER_DRIVER", "  ANVIL ")
	t.Setenv("CERT_NFT_ADDRESS", testContract.Hex())
	t.Setenv("SIGNER_ANVIL_PRIVATE_KEY", anvilPK)
	t.Setenv("SIGNER_KEYSTORE_PATH", "/tmp/ks.json")
	t.Setenv("SIGNER_KMS_KEY_ID", "alias/foo")

	cfg := SignerConfigFromEnv(testChainID)
	if cfg.Driver != DriverAnvil {
		t.Fatalf("Driver = %q, want %q", cfg.Driver, DriverAnvil)
	}
	if cfg.Contract != testContract {
		t.Fatalf("Contract = %v, want %v", cfg.Contract, testContract)
	}
	if cfg.KMSKeyID != "alias/foo" || cfg.KeystorePath != "/tmp/ks.json" || cfg.AnvilPrivateKey != anvilPK {
		t.Fatalf("env fields not wired: %+v", cfg)
	}

	s, err := NewMintSigner(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewMintSigner from env: %v", err)
	}
	if s.Address() == (common.Address{}) {
		t.Fatal("Address is zero")
	}
}

// TestSignerConfigFromEnv_ReadsExpectedAddress 锁定 CERT_MINTER_ADDRESS 的接线。
func TestSignerConfigFromEnv_ReadsExpectedAddress(t *testing.T) {
	t.Setenv("SIGNER_DRIVER", DriverAnvil)
	t.Setenv("CERT_NFT_ADDRESS", testContract.Hex())
	t.Setenv("CERT_MINTER_ADDRESS", anvilAddr.Hex())

	cfg := SignerConfigFromEnv(testChainID)
	if cfg.ExpectedAddress != anvilAddr {
		t.Fatalf("ExpectedAddress = %v, want %v", cfg.ExpectedAddress, anvilAddr)
	}
}

// TestSignerConfigFromEnv_DropsAnvilKeyForNonAnvilDriver 是纵深防御第一层：
// 非 anvil driver 下明文私钥环境变量绝不进结构体。
func TestSignerConfigFromEnv_DropsAnvilKeyForNonAnvilDriver(t *testing.T) {
	for _, driver := range []string{"", DriverKeystore, DriverKMS} {
		t.Run("driver="+driver, func(t *testing.T) {
			t.Setenv("SIGNER_DRIVER", driver)
			t.Setenv("CERT_NFT_ADDRESS", testContract.Hex())
			t.Setenv("SIGNER_ANVIL_PRIVATE_KEY", anvilPK)

			if cfg := SignerConfigFromEnv(testChainID); cfg.AnvilPrivateKey != "" {
				t.Fatalf("anvil private key leaked into config for driver %q", driver)
			}
		})
	}
}

// TestNonAnvilDrivers_RejectLeakedAnvilKey 是纵深防御第二层：即使调用方绕过
// SignerConfigFromEnv 直接构造 SignerConfig，driver 也必须 fail-fast。
func TestNonAnvilDrivers_RejectLeakedAnvilKey(t *testing.T) {
	ksPath, _ := newTempKeystore(t, "pw")

	t.Run("keystore", func(t *testing.T) {
		cfg := baseCfg(DriverKeystore)
		cfg.KeystorePath, cfg.KeystorePassword = ksPath, "pw"
		cfg.AnvilPrivateKey = anvilPK
		if _, err := NewKeystoreDriver(cfg); !errors.Is(err, ErrSignerMisconfigured) {
			t.Fatalf("err = %v, want ErrSignerMisconfigured", err)
		}
	})

	t.Run("kms", func(t *testing.T) {
		key, err := crypto.GenerateKey()
		if err != nil {
			t.Fatal(err)
		}
		cfg := baseCfg(DriverKMS)
		cfg.KMSKeyID = "alias/x-web3-cert-minter"
		cfg.KMSClient = &fakeKMS{key: key}
		cfg.AnvilPrivateKey = anvilPK
		if _, err := NewKMSDriver(context.Background(), cfg); !errors.Is(err, ErrSignerMisconfigured) {
			t.Fatalf("err = %v, want ErrSignerMisconfigured", err)
		}
	})

	// 经由工厂时同样拦截（含 driver 为空默认 keystore 的分支）。
	t.Run("via NewMintSigner with empty driver", func(t *testing.T) {
		cfg := baseCfg("")
		cfg.KeystorePath, cfg.KeystorePassword = ksPath, "pw"
		cfg.AnvilPrivateKey = anvilPK
		if _, err := NewMintSigner(context.Background(), cfg); !errors.Is(err, ErrSignerMisconfigured) {
			t.Fatalf("err = %v, want ErrSignerMisconfigured", err)
		}
	})
}

// TestExpectedAddressMismatch_FailsFast 覆盖三种 driver 的地址断言。
func TestExpectedAddressMismatch_FailsFast(t *testing.T) {
	wrong := common.HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

	t.Run("anvil", func(t *testing.T) {
		cfg := baseCfg(DriverAnvil)
		cfg.AnvilPrivateKey = anvilPK
		cfg.ExpectedAddress = wrong
		if _, err := NewAnvilDriver(cfg); !errors.Is(err, ErrSignerMisconfigured) {
			t.Fatalf("err = %v, want ErrSignerMisconfigured", err)
		}

		// 正确地址必须放行。
		cfg.ExpectedAddress = anvilAddr
		if _, err := NewAnvilDriver(cfg); err != nil {
			t.Fatalf("matching ExpectedAddress rejected: %v", err)
		}
	})

	t.Run("keystore", func(t *testing.T) {
		path, addr := newTempKeystore(t, "pw")
		cfg := baseCfg(DriverKeystore)
		cfg.KeystorePath, cfg.KeystorePassword = path, "pw"
		cfg.ExpectedAddress = wrong
		if _, err := NewKeystoreDriver(cfg); !errors.Is(err, ErrSignerMisconfigured) {
			t.Fatalf("err = %v, want ErrSignerMisconfigured", err)
		}

		cfg.ExpectedAddress = addr
		if _, err := NewKeystoreDriver(cfg); err != nil {
			t.Fatalf("matching ExpectedAddress rejected: %v", err)
		}
	})

	t.Run("kms", func(t *testing.T) {
		key, err := crypto.GenerateKey()
		if err != nil {
			t.Fatal(err)
		}
		cfg := baseCfg(DriverKMS)
		cfg.KMSKeyID = "alias/x-web3-cert-minter"
		cfg.KMSClient = &fakeKMS{key: key}
		cfg.ExpectedAddress = wrong
		if _, err := NewKMSDriver(context.Background(), cfg); !errors.Is(err, ErrSignerMisconfigured) {
			t.Fatalf("err = %v, want ErrSignerMisconfigured", err)
		}

		cfg.ExpectedAddress = crypto.PubkeyToAddress(key.PublicKey)
		if _, err := NewKMSDriver(context.Background(), cfg); err != nil {
			t.Fatalf("matching ExpectedAddress rejected: %v", err)
		}
	})
}

// TestKMSDriver_SelfTestCounter 确认每次成功签名都记一次自检。
func TestKMSDriver_SelfTestCounter(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	cfg := baseCfg(DriverKMS)
	cfg.KMSKeyID = "alias/x-web3-cert-minter"
	cfg.KMSClient = &fakeKMS{key: key}

	d, err := NewKMSDriver(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewKMSDriver: %v", err)
	}
	if got := d.SelfTestCount(); got != 0 {
		t.Fatalf("SelfTestCount before signing = %d, want 0", got)
	}
	for i := 1; i <= 3; i++ {
		if _, err := d.SignCertificateMint(context.Background(), big.NewInt(int64(i)), testTo, testURI); err != nil {
			t.Fatalf("SignCertificateMint #%d: %v", i, err)
		}
		if got := d.SelfTestCount(); got != uint64(i) {
			t.Fatalf("SelfTestCount = %d, want %d", got, i)
		}
	}
}

// TestKMSDriver_RejectsSignatureFromWrongKey 模拟 KMS 密钥被换掉（GetPublicKey
// 与 Sign 用不同私钥）：签名无法还原出缓存地址，必须报错而不是产出坏交易。
func TestKMSDriver_RejectsSignatureFromWrongKey(t *testing.T) {
	pubKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	signKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	cfg := baseCfg(DriverKMS)
	cfg.KMSKeyID = "alias/x-web3-cert-minter"
	cfg.KMSClient = &mismatchedKMS{pubKey: pubKey, signKey: signKey}

	d, err := NewKMSDriver(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewKMSDriver: %v", err)
	}
	_, err = d.SignCertificateMint(context.Background(), big.NewInt(42), testTo, testURI)
	if !errors.Is(err, ErrSignFailed) {
		t.Fatalf("err = %v, want ErrSignFailed", err)
	}
	if got := d.SelfTestCount(); got != 0 {
		t.Fatalf("SelfTestCount = %d, want 0 (failed sign must not count)", got)
	}
}

// mismatchedKMS 的 GetPublicKey 与 Sign 使用不同私钥。
type mismatchedKMS struct{ pubKey, signKey *ecdsa.PrivateKey }

func (m *mismatchedKMS) GetPublicKey(
	ctx context.Context, in *kms.GetPublicKeyInput, opts ...func(*kms.Options),
) (*kms.GetPublicKeyOutput, error) {
	return (&fakeKMS{key: m.pubKey}).GetPublicKey(ctx, in, opts...)
}

func (m *mismatchedKMS) Sign(
	ctx context.Context, in *kms.SignInput, opts ...func(*kms.Options),
) (*kms.SignOutput, error) {
	return (&fakeKMS{key: m.signKey}).Sign(ctx, in, opts...)
}

// TestStaticTxParams_Defaults 保证未注入 provider 时有可用的 gas 兜底。
func TestStaticTxParams_Defaults(t *testing.T) {
	p, err := StaticTxParams{}.TxParams(context.Background(), common.Address{})
	if err != nil {
		t.Fatal(err)
	}
	if p.GasLimit != defaultGasLimit || p.GasFeeCap == nil || p.GasTipCap == nil {
		t.Fatalf("bad defaults: %+v", p)
	}
}

func ptr[T any](v T) *T { return &v }
