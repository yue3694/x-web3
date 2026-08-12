// Package admin — chain_status.go 提供 GET /admin/chain/sync。
//
// 行为：
//   - 必须 query 带 chainId；
//   - 从 chain_checkpoints 取本地推进位（next_block / last_block_hash / updated_at）；
//   - 若配置了对应 chain 的 RPC URL，则调 eth_blockNumber + eth_getBlockByNumber
//     把本地推进位与链头对齐，计算 lagBlocks / lagSeconds；
//   - 若 RPC 未配置或调用失败：仅返回 checkpoint 数据，head 字段为 null，
//     并在响应中携带 rpcAvailable=false，调用方据此判定是否告警；
//   - 不写 audit（read-only）。
package admin

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/x-web3/api/internal/config"
	"github.com/x-web3/api/internal/errcode"
	"github.com/x-web3/api/internal/httpkit"
	"github.com/x-web3/api/internal/rbac"
	"github.com/x-web3/api/internal/user"
)

// ChainStatusHandler 暴露 GET /admin/chain/sync。
type ChainStatusHandler struct {
	pool   *pgxpool.Pool
	cfg    *config.Config
	rbac   *rbac.Engine
	logger *zap.Logger
	http   *http.Client
}

// NewChainStatusHandler 构造 handler。http client 可为 nil：nil 时退化为不查 RPC。
func NewChainStatusHandler(pool *pgxpool.Pool, cfg *config.Config, rbac *rbac.Engine, logger *zap.Logger) *ChainStatusHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ChainStatusHandler{
		pool:   pool,
		cfg:    cfg,
		rbac:   rbac,
		logger: logger,
		http:   &http.Client{Timeout: 5 * time.Second},
	}
}

// syncResponse 是 GET /admin/chain/sync 的响应形状。
type syncResponse struct {
	ChainID         int64      `json:"chainId"`
	Consumer        string     `json:"consumer"`
	NextBlock       int64      `json:"nextBlock"`
	LastBlockHash   *string    `json:"lastBlockHash"`
	LagSeconds      *float64   `json:"lagSeconds"`
	LagBlocks       *int64     `json:"lagBlocks"`
	HeadBlockNumber *int64     `json:"headBlockNumber"`
	HeadBlockHash   *string    `json:"headBlockHash"`
	HeadTimestamp   *time.Time `json:"headTimestamp"`
	LastUpdatedAt   *time.Time `json:"lastUpdatedAt"`
	RpcAvailable    bool       `json:"rpcAvailable"`
	RpcError        *string    `json:"rpcError,omitempty"`
	CheckedAt       time.Time  `json:"checkedAt"`
}

// syncConsumer 与 chain_rewind.go 保持一致（"indexer"）。
const syncConsumer = "indexer"

// Status GET /admin/chain/sync?chainId=N。
func (h *ChainStatusHandler) Status(c *httpkit.Context) {
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	if err := h.rbac.RequireRole(user.RoleSuperAdmin)(c.Request.Context(), uid); err != nil {
		httpkit.Error(c, http.StatusForbidden, errcode.Forbidden, "permission denied", nil)
		return
	}
	chainIDStr := strings.TrimSpace(c.Query("chainId"))
	if chainIDStr == "" {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "chainId is required", nil)
		return
	}
	chainID, err := strconv.ParseInt(chainIDStr, 10, 64)
	if err != nil || chainID <= 0 {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "chainId must be a positive integer", nil)
		return
	}

	// 1) checkpoint 数据（DB 权威）。
	var (
		nextBlock     int64
		lastHashBytes []byte
		updatedAt     *time.Time
	)
	row := h.pool.QueryRow(c.Request.Context(), `
SELECT next_block, last_block_hash, updated_at
FROM chain_checkpoints
WHERE chain_id = $1 AND consumer = $2`, chainID, syncConsumer)
	if err := row.Scan(&nextBlock, &lastHashBytes, &updatedAt); err != nil {
		httpkit.Error(c, http.StatusNotFound, errcode.NotFound,
			"checkpoint not found for this chain / consumer", nil)
		return
	}

	resp := syncResponse{
		ChainID:       chainID,
		Consumer:      syncConsumer,
		NextBlock:     nextBlock,
		LastBlockHash: hashToHex(lastHashBytes),
		LastUpdatedAt: updatedAt,
		CheckedAt:     time.Now().UTC(),
		RpcAvailable:  false,
	}
	if updatedAt != nil {
		lag := resp.CheckedAt.Sub(*updatedAt).Seconds()
		resp.LagSeconds = &lag
	}

	// 2) RPC 调用（可选）：仅在配置里找到 chain 的 RPC URL 时尝试。
	rpcURL := h.rpcURLForChain(chainID)
	if rpcURL == "" {
		errStr := "no rpc url configured for this chain"
		resp.RpcError = &errStr
	} else if head, herr := h.fetchHead(c.Request.Context(), rpcURL); herr != nil {
		errStr := herr.Error()
		resp.RpcError = &errStr
		h.logger.Warn("chain_sync_rpc_failed",
			zap.Int64("chainId", chainID), zap.String("rpc", maskRPC(rpcURL)),
			zap.Error(herr))
	} else {
		resp.RpcAvailable = true
		resp.HeadBlockNumber = &head.Number
		resp.HeadBlockHash = &head.Hash
		resp.HeadTimestamp = &head.Timestamp
		lag := head.Number - resp.NextBlock
		if lag < 0 {
			lag = 0
		}
		resp.LagBlocks = &lag
	}

	c.JSON(http.StatusOK, resp)
}

// rpcURLForChain 查 cfg 里是否有针对给定 chainId 的 URL；当前实现把
// `cfg.SepoliaRPCURL` 视为 chainId=11155111 的入口；其它 chain 走空字符串 →
// 返回 checkpoint-only 视图。如未来多链，可在 cfg 加 ChainRPCURLs map[int64]string。
func (h *ChainStatusHandler) rpcURLForChain(chainID int64) string {
	if h.cfg == nil {
		return ""
	}
	// Sepolia 约定。
	if chainID == 11155111 && h.cfg.SepoliaRPCURL != "" {
		return h.cfg.SepoliaRPCURL
	}
	return ""
}

// chainHead 是 eth_getBlockByNumber("latest") 解析出的最小字段。
type chainHead struct {
	Number    int64
	Hash      string
	Timestamp time.Time
}

// fetchHead 通过 JSON-RPC 取链头（block number + hash + timestamp）。
func (h *ChainStatusHandler) fetchHead(ctx context.Context, rpcURL string) (chainHead, error) {
	if h.http == nil {
		return chainHead{}, errors.New("http client not initialized")
	}
	// 1) eth_blockNumber
	num, err := h.rpcCallInt(ctx, rpcURL, "eth_blockNumber", nil)
	if err != nil {
		return chainHead{}, fmt.Errorf("eth_blockNumber: %w", err)
	}
	// 2) eth_getBlockByNumber("latest", false)
	blockHex := "0x" + strconv.FormatInt(num, 16)
	raw2, err := h.rpcCallRaw(ctx, rpcURL, "eth_getBlockByNumber", []any{blockHex, false})
	if err != nil {
		return chainHead{}, fmt.Errorf("eth_getBlockByNumber: %w", err)
	}
	var blk struct {
		Number    string `json:"number"`
		Hash      string `json:"hash"`
		Timestamp string `json:"timestamp"`
	}
	if err := json.Unmarshal(raw2, &blk); err != nil {
		return chainHead{}, fmt.Errorf("decode block: %w", err)
	}
	bn, err := hexToInt64(blk.Number)
	if err != nil {
		return chainHead{}, fmt.Errorf("parse number: %w", err)
	}
	ts, err := hexToInt64(blk.Timestamp)
	if err != nil {
		return chainHead{}, fmt.Errorf("parse timestamp: %w", err)
	}
	return chainHead{
		Number:    bn,
		Hash:      blk.Hash,
		Timestamp: time.Unix(ts, 0).UTC(),
	}, nil
}

// rpcCallInt 取 hex int 字段（如 "0x10" → 16）。
func (h *ChainStatusHandler) rpcCallInt(ctx context.Context, url, method string, params []any) (int64, error) {
	raw, err := h.rpcCallRaw(ctx, url, method, params)
	if err != nil {
		return 0, err
	}
	return hexToInt64(string(raw))
}

// rpcCallRaw 调 JSON-RPC；返回 result 字段（原始 JSON）。
func (h *ChainStatusHandler) rpcCallRaw(ctx context.Context, url, method string, params []any) (json.RawMessage, error) {
	if params == nil {
		params = []any{}
	}
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      time.Now().UnixNano(),
		"method":  method,
		"params":  params,
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(buf)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("rpc http %d", resp.StatusCode)
	}
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return nil, fmt.Errorf("decode envelope: %w", err)
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("rpc %s: %s", method, envelope.Error.Message)
	}
	return envelope.Result, nil
}

// hexToInt64 解析 "0x..." hex 字符串为 int64。
func hexToInt64(s string) (int64, error) {
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	if s == "" {
		return 0, errors.New("empty hex")
	}
	v, err := strconv.ParseInt(s, 16, 64)
	if err != nil {
		return 0, err
	}
	return v, nil
}

// hashToHex 把 bytea 转 0x 前缀 hex；nil/空 → nil。
func hashToHex(b []byte) *string {
	if len(b) == 0 {
		return nil
	}
	out := "0x" + hex.EncodeToString(b)
	return &out
}

// maskRPC 把 URL 里的 api key 之类敏感串替换成 ***。
// 与 main.go 的 maskURL 风格保持一致。
func maskRPC(u string) string {
	const at = "@"
	if i := strings.Index(u, at); i >= 0 {
		// 截断 userinfo：保留 scheme + host。
		if j := strings.Index(u, "://"); j >= 0 && j < i {
			return u[:j+3] + "***" + u[i:]
		}
	}
	return u
}