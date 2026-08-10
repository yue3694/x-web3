// Package treasury 提供 worker 端的「业务关键余额告警」监控（F06-T08）。
//
// 职责：
//   - 周期性轮询 treasury / minter / hot-wallet 地址的 ETH + ERC20 (YD) 余额；
//   - 与阈值比对，命中阈值时插入 treasury_alerts 行；
//   - 通过 Metrics 接口上报 Prometheus counter（counter 实际注册由 API/worker
//     主进程负责；本包只暴露接口，单测注入 fake）；
//   - 用 slog warn 输出结构化日志（worker 现有约定）。
//
// 设计要点：
//   - 包内所有外部依赖通过接口注入：Client (RPC) / Store (DB) / Metrics。
//     真机 / 单测用同一套 Monitor，仅替换实现。
//   - ETH 走 ethclient.BalanceAt，ERC20 走 go-ethereum/rpc + bind.Call，
//     不引入额外的 ERC20 SDK 依赖。
//   - severity 规则：balance < threshold/4 → 'critical'，否则 'warn'。
//     这条策略保持简单、便于 ops 调阈值；后续如需多档（info/warn/critical）
//     再扩。
package treasury

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Asset 标识监控的资产类型。
type Asset string

const (
	AssetETH Asset = "ETH"
	AssetYD  Asset = "YD"
)

// Severity 告警严重度。
type Severity string

const (
	SeverityWarn     Severity = "warn"
	SeverityCritical Severity = "critical"
)

// balanceOfABI ERC20 的 balanceOf(address) -> uint256 最小 ABI。
// 仅 view 用途，足够覆盖监控场景，避免引入完整 ERC20 ABI。
const balanceOfABI = `[{"constant":true,"inputs":[{"name":"_owner","type":"address"}],"name":"balanceOf","outputs":[{"name":"balance","type":"uint256"}],"type":"function"}]`

// Config 构造参数。
type Config struct {
	// Pool 数据库连接；用于持久化 treasury_alerts。
	Pool *pgxpool.Pool
	// RPC 用于 ethclient + bind.Call 的底层 RPC 连接。
	RPC *rpc.Client
	// Logger；为 nil 时用 slog.Default()。
	Logger *slog.Logger

	// TreasuryAddresses 主 treasury 地址（CSV）。允许多个。
	TreasuryAddresses []common.Address
	// MinterAddress 证书 minter 单地址；可为空。
	MinterAddress common.Address
	// HotWalletAddresses 业务热钱包地址（CSV）。可为空。
	HotWalletAddresses []common.Address

	// YDToken YD ERC20 合约地址；为 common.Address{} 时跳过 ERC20 检查。
	YDToken common.Address

	// MinETHWei ETH 阈值，单位 wei；默认 0.1 ETH。
	MinETHWei *big.Int
	// MinYDBalance YD 阈值；默认 1_000_000e18。
	MinYDBalance *big.Int

	// Interval 轮询周期；默认 5 min。
	Interval time.Duration
}

// Monitor 周期性余额告警监控器。
type Monitor struct {
	cfg Config

	client *ethclient.Client // 用于 ETH balance
	rpc    *rpc.Client       // 用于 ERC20 bind.Call

	store   *pgStore
	metrics Metrics

	// criticalRatio threshold 多少以下算 critical；默认 0.25（即 < 25%）。
	criticalRatio float64

	// abiParsed cache 避免每次轮询重新解析。
	abiParsed    abi.ABI
	abiParsedErr error
	abiOnce      sync.Once

	// 内部状态（用于 metrics / 测试断言）
	scanRuns    atomic.Int64
	alertsFired atomic.Int64
	lastScanNS  atomic.Int64
}

// AddressMeta 一个被监控地址的分类标签（决定日志/指标的 severity 标签）。
type AddressMeta struct {
	Address common.Address
	Label   string // 'treasury' | 'minter' | 'hot_wallet'
}

// NewMonitor 构造监控器。
func NewMonitor(cfg Config) (*Monitor, error) {
	if cfg.Pool == nil {
		return nil, errors.New("treasury: pool required")
	}
	if cfg.RPC == nil {
		return nil, errors.New("treasury: rpc client required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Minute
	}
	if cfg.MinETHWei == nil {
		cfg.MinETHWei = new(big.Int).Mul(big.NewInt(1e17), big.NewInt(1)) // 0.1 ETH
	}
	if cfg.MinYDBalance == nil {
		// 1_000_000 * 1e18 —— 写到大整数里避免 uint64 overflow（1e24 > 2^64）。
		cfg.MinYDBalance = new(big.Int).Mul(
			new(big.Int).SetUint64(1_000_000),
			new(big.Int).SetUint64(1e18),
		)
	}
	m := &Monitor{
		cfg:           cfg,
		client:        ethclient.NewClient(cfg.RPC),
		rpc:           cfg.RPC,
		store:         &pgStore{pool: cfg.Pool},
		metrics:       noopMetrics{},
		criticalRatio: 0.25,
	}
	// 提前解析 ABI 一次，失败记录但不阻塞构造（运行时再报错）。
	m.abiOnce.Do(func() {
		m.abiParsed, m.abiParsedErr = abi.JSON(strings.NewReader(balanceOfABI))
	})
	return m, nil
}

// SetMetrics 注入指标实现；主进程应在 NewMonitor 之后调用以挂载真实 Prometheus counter。
func (m *Monitor) SetMetrics(metrics Metrics) {
	if metrics == nil {
		m.metrics = noopMetrics{}
		return
	}
	m.metrics = metrics
}

// AllAddresses 返回所有监控地址 + 标签（去重 + 跳过零地址）。
func (m *Monitor) AllAddresses() []AddressMeta {
	seen := map[common.Address]struct{}{}
	var out []AddressMeta
	add := func(addr common.Address, label string) {
		if addr == (common.Address{}) {
			return
		}
		if _, ok := seen[addr]; ok {
			return
		}
		seen[addr] = struct{}{}
		out = append(out, AddressMeta{Address: addr, Label: label})
	}
	for _, a := range m.cfg.TreasuryAddresses {
		add(a, "treasury")
	}
	add(m.cfg.MinterAddress, "minter")
	for _, a := range m.cfg.HotWalletAddresses {
		add(a, "hot_wallet")
	}
	return out
}

// Start 阻塞；ctx.Done() 退出。启动即跑一次 Scan。
func (m *Monitor) Start(ctx context.Context) {
	if _, err := m.Scan(ctx); err != nil {
		m.cfg.Logger.Warn("treasury_scan_initial_failed", "err", err.Error())
	}
	t := time.NewTicker(m.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := m.Scan(ctx); err != nil {
				m.cfg.Logger.Warn("treasury_scan_failed", "err", err.Error())
			}
		}
	}
}

// Alert 一条命中阈值的告警记录。
type Alert struct {
	ID        uuid.UUID
	Address   common.Address
	Asset     Asset
	Balance   *big.Int
	Threshold *big.Int
	Severity  Severity
	Label     string
}

// ScanResult 单轮扫描的结果（用于测试 + metrics）。
type ScanResult struct {
	Scanned int
	Alerts  []Alert
}

// Scan 跑一轮检查并写库。公开以便单测。
func (m *Monitor) Scan(ctx context.Context) (ScanResult, error) {
	m.scanRuns.Add(1)
	m.lastScanNS.Store(time.Now().UnixNano())

	res := ScanResult{}
	addrs := m.AllAddresses()
	if len(addrs) == 0 {
		return res, nil
	}

	// ETH 部分：并行 batch call 也行，但地址数不多（N=~5）；简单循环串行避免
	// 抢同一个 RPC 的连接池。
	for _, am := range addrs {
		bal, err := m.client.BalanceAt(ctx, am.Address, nil)
		if err != nil {
			m.cfg.Logger.Warn("treasury_eth_balance_failed",
				"address", am.Address.Hex(),
				"label", am.Label,
				"err", err.Error())
			continue
		}
		res.Scanned++
		if bal.Cmp(m.cfg.MinETHWei) < 0 {
			a := Alert{
				Address:   am.Address,
				Asset:     AssetETH,
				Balance:   new(big.Int).Set(bal),
				Threshold: new(big.Int).Set(m.cfg.MinETHWei),
				Severity:  m.classifySeverity(bal, m.cfg.MinETHWei),
				Label:     am.Label,
			}
			if err := m.recordAlert(ctx, a); err != nil {
				m.cfg.Logger.Error("treasury_alert_persist_failed",
					"address", a.Address.Hex(),
					"asset", string(a.Asset),
					"err", err.Error())
				continue
			}
			res.Alerts = append(res.Alerts, a)
			m.alertsFired.Add(1)
			m.metrics.IncAlert(a.Address.Hex(), string(a.Asset), string(a.Severity))
			m.cfg.Logger.Warn("treasury_alert",
				"address", a.Address.Hex(),
				"label", a.Label,
				"asset", string(a.Asset),
				"severity", string(a.Severity),
				"balance", bal.String(),
				"threshold", m.cfg.MinETHWei.String(),
			)
		}
	}

	// YD 部分：仅当 token 配置且 RPC ABI 解析成功。
	if m.cfg.YDToken != (common.Address{}) && m.abiParsedErr == nil {
		for _, am := range addrs {
			bal, err := m.callBalanceOf(ctx, m.cfg.YDToken, am.Address)
			if err != nil {
				m.cfg.Logger.Warn("treasury_yd_balance_failed",
					"address", am.Address.Hex(),
					"label", am.Label,
					"err", err.Error())
				continue
			}
			if bal.Cmp(m.cfg.MinYDBalance) < 0 {
				a := Alert{
					Address:   am.Address,
					Asset:     AssetYD,
					Balance:   new(big.Int).Set(bal),
					Threshold: new(big.Int).Set(m.cfg.MinYDBalance),
					Severity:  m.classifySeverity(bal, m.cfg.MinYDBalance),
					Label:     am.Label,
				}
				if err := m.recordAlert(ctx, a); err != nil {
					m.cfg.Logger.Error("treasury_alert_persist_failed",
						"address", a.Address.Hex(),
						"asset", string(a.Asset),
						"err", err.Error())
					continue
				}
				res.Alerts = append(res.Alerts, a)
				m.alertsFired.Add(1)
				m.metrics.IncAlert(a.Address.Hex(), string(a.Asset), string(a.Severity))
				m.cfg.Logger.Warn("treasury_alert",
					"address", a.Address.Hex(),
					"label", a.Label,
					"asset", string(a.Asset),
					"severity", string(a.Severity),
					"balance", bal.String(),
					"threshold", m.cfg.MinYDBalance.String(),
				)
			}
		}
	}
	return res, nil
}

// classifySeverity balance < threshold * criticalRatio 时升级 critical。
func (m *Monitor) classifySeverity(balance, threshold *big.Int) Severity {
	if threshold.Sign() <= 0 {
		return SeverityWarn
	}
	// criticalLine = threshold * criticalRatio
	ratioNum := big.NewInt(int64(m.criticalRatio * 1e6))
	ratioDen := big.NewInt(1_000_000)
	criticalLine := new(big.Int).Mul(threshold, ratioNum)
	criticalLine.Quo(criticalLine, ratioDen)
	if balance.Cmp(criticalLine) < 0 {
		return SeverityCritical
	}
	return SeverityWarn
}

// callBalanceOf 用 bind.Call 走 RPC 调 balanceOf；不依赖任何 SDK。
func (m *Monitor) callBalanceOf(ctx context.Context, token, holder common.Address) (*big.Int, error) {
	if m.abiParsedErr != nil {
		return nil, fmt.Errorf("treasury: abi parse: %w", m.abiParsedErr)
	}
	data, err := m.abiParsed.Pack("balanceOf", holder)
	if err != nil {
		return nil, fmt.Errorf("treasury: pack balanceOf: %w", err)
	}
	// eth_call msg。
	msg := ethereum.CallMsg{
		To:   &token,
		Data: data,
	}
	raw, err := m.client.CallContract(ctx, msg, nil)
	if err != nil {
		return nil, fmt.Errorf("treasury: eth_call balanceOf: %w", err)
	}
	out, err := m.abiParsed.Unpack("balanceOf", raw)
	if err != nil {
		return nil, fmt.Errorf("treasury: unpack balanceOf: %w", err)
	}
	if len(out) == 0 {
		return nil, errors.New("treasury: balanceOf returned empty")
	}
	bal, ok := out[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("treasury: balanceOf returned %T, want *big.Int", out[0])
	}
	return bal, nil
}

// recordAlert 落库；id 由 DB default gen_random_uuid() 生成。
func (m *Monitor) recordAlert(ctx context.Context, a Alert) error {
	var id uuid.UUID
	err := m.store.pool.QueryRow(ctx, `
INSERT INTO treasury_alerts(address, asset, balance, threshold, severity)
VALUES($1,$2,$3::numeric,$4::numeric,$5)
RETURNING id`,
		a.Address.Hex(), string(a.Asset), a.Balance.String(), a.Threshold.String(), string(a.Severity),
	).Scan(&id)
	if err != nil {
		return fmt.Errorf("treasury: insert alert: %w", err)
	}
	a.ID = id
	return nil
}

// Metrics 是 monitor 上报指标所需的接口。
//
// 设计：worker 主进程若引入 prometheus/client_golang，
// 则实现 IncAlert(...) 把 {address,asset,severity} 三个 label 打上去；
// 未引入时用 noopMetrics。
type Metrics interface {
	IncAlert(address, asset, severity string)
}

// noopMetrics 默认实现 —— 在 worker 真正接入 prometheus 之前不做事。
type noopMetrics struct{}

func (noopMetrics) IncAlert(_, _, _ string) {}

// pgStore 是 treasury_alerts 表的 DB 适配层（目前只用 INSERT，留扩展）。
type pgStore struct {
	pool *pgxpool.Pool
}

// Ensure pgx.ErrNoRows 引用保留，避免 unused import 在某些 build 模式下报错。
var _ = pgx.ErrNoRows

// LoadConfigFromEnv 从环境变量装载 Config。
//
// TREASURY_POLL_INTERVAL=5m
// TREASURY_ADDRESSES=0xabc...,0xdef...
// MINTER_ADDRESS=0x...
// HOT_WALLET_ADDRESSES=0x...,0x...
// YD_TOKEN_ADDRESS=0x...
// TREASURY_MIN_ETH_WEI=100000000000000000  (0.1 ETH)
// YD_MIN_BALANCE=1000000000000000000000000  (1M * 1e18)
//
// 任何一个解析失败就 panic —— 配置错误应当 fail-fast，避免监控静默失效。
func LoadConfigFromEnv(pool *pgxpool.Pool, rpcClient *rpc.Client) (Config, error) {
	defaultYDMin := new(big.Int).Mul(
		new(big.Int).SetUint64(1_000_000),
		new(big.Int).SetUint64(1e18),
	)
	cfg := Config{
		Pool:         pool,
		RPC:          rpcClient,
		Logger:       slog.Default(),
		MinETHWei:    new(big.Int).SetUint64(parseUint64Env("TREASURY_MIN_ETH_WEI", 1e17)),
		MinYDBalance: parseBigUintEnv("YD_MIN_BALANCE", defaultYDMin),
	}
	if v := os.Getenv("TREASURY_POLL_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("treasury: invalid TREASURY_POLL_INTERVAL %q: %w", v, err)
		}
		cfg.Interval = d
	}
	addrs, err := parseAddrCSV(os.Getenv("TREASURY_ADDRESSES"))
	if err != nil {
		return cfg, fmt.Errorf("treasury: TREASURY_ADDRESSES: %w", err)
	}
	cfg.TreasuryAddresses = addrs

	minter, err := parseAddrOpt(os.Getenv("MINTER_ADDRESS"))
	if err != nil {
		return cfg, fmt.Errorf("treasury: MINTER_ADDRESS: %w", err)
	}
	cfg.MinterAddress = minter

	hot, err := parseAddrCSV(os.Getenv("HOT_WALLET_ADDRESSES"))
	if err != nil {
		return cfg, fmt.Errorf("treasury: HOT_WALLET_ADDRESSES: %w", err)
	}
	cfg.HotWalletAddresses = hot

	yd, err := parseAddrOpt(os.Getenv("YD_TOKEN_ADDRESS"))
	if err != nil {
		return cfg, fmt.Errorf("treasury: YD_TOKEN_ADDRESS: %w", err)
	}
	cfg.YDToken = yd
	return cfg, nil
}

func parseUint64Env(key string, def uint64) uint64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := parseBigUint(v)
	if err != nil {
		return def
	}
	if !n.IsUint64() {
		return def
	}
	return n.Uint64()
}

// parseBigUintEnv 读 env 并按十进制解析成 *big.Int；解析失败 / 未设返回 def。
func parseBigUintEnv(key string, def *big.Int) *big.Int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return new(big.Int).Set(def)
	}
	n, err := parseBigUint(v)
	if err != nil {
		return new(big.Int).Set(def)
	}
	return n
}

// parseAddrCSV 解析逗号分隔地址列表，跳过空项。
func parseAddrCSV(s string) ([]common.Address, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]common.Address, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		addr, err := parseAddrOpt(p)
		if err != nil {
			return nil, err
		}
		if addr == (common.Address{}) {
			continue
		}
		out = append(out, addr)
	}
	return out, nil
}

// parseAddrOpt 解析单个地址；空字符串返回零地址 + nil。
func parseAddrOpt(s string) (common.Address, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return common.Address{}, nil
	}
	if !common.IsHexAddress(s) {
		return common.Address{}, fmt.Errorf("invalid address %q", s)
	}
	return common.HexToAddress(s), nil
}

// parseBigUint 接受十进制字符串；上限 uint256（虽校验更宽，但足够阈值场景）。
func parseBigUint(s string) (*big.Int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("empty")
	}
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil, fmt.Errorf("invalid uint %q", s)
	}
	if n.Sign() < 0 {
		return nil, fmt.Errorf("negative uint %q", s)
	}
	return n, nil
}
