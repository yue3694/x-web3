// Package wallet 处理钱包绑定：nonce 防重放、eip-191 签名校验、唯一性冲突。
package wallet

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// NonceStore 在 Redis 维护一次性 nonce。key 形式：wallet:nonce:{value} = user_id。
type NonceStore struct {
	rdb *redis.Client
	ttl time.Duration
}

func NewNonceStore(rdb *redis.Client, ttl time.Duration) *NonceStore {
	return &NonceStore{rdb: rdb, ttl: ttl}
}

// Issue 生成 16-byte nonce，并立即 reserve（SETNX）。
// 成功 reserve 后 nonce 才能被 binding 流程使用。
func (n *NonceStore) Issue(ctx context.Context, userID string) (string, time.Time, error) {
	if userID == "" {
		return "", time.Time{}, errors.New("wallet: user id required")
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", time.Time{}, err
	}
	nonce := hex.EncodeToString(b)
	ok, err := n.rdb.SetNX(ctx, n.key(nonce), userID, n.ttl).Result()
	if err != nil {
		return "", time.Time{}, err
	}
	if !ok {
		return "", time.Time{}, errors.New("wallet: nonce collision (rare)")
	}
	return nonce, time.Now().UTC().Add(n.ttl), nil
}

// Consume 标记 nonce 已用。重复 Consume 返回 error。
func (n *NonceStore) Consume(ctx context.Context, nonce, userID string) error {
	if nonce == "" || userID == "" {
		return errors.New("wallet: empty nonce")
	}
	const consumeScript = `
local value = redis.call("GET", KEYS[1])
if not value then return 0 end
if value ~= ARGV[1] then return -1 end
redis.call("DEL", KEYS[1])
return 1`
	result, err := n.rdb.Eval(ctx, consumeScript, []string{n.key(nonce)}, userID).Int()
	if err != nil {
		return fmt.Errorf("wallet: consume nonce: %w", err)
	}
	switch result {
	case 1:
		return nil
	case -1:
		return errors.New("wallet: nonce belongs to another user")
	default:
		return errors.New("wallet: nonce unknown, expired, or reused")
	}
}

// VerifyDomain 仅作 helper：domain 不允许为空、必须包含在请求里。
func VerifyDomain(stated, expected string) error {
	stated = strings.TrimSpace(strings.ToLower(stated))
	expected = strings.TrimSpace(strings.ToLower(expected))
	if stated == "" {
		return errors.New("wallet: missing domain")
	}
	if stated != expected {
		return fmt.Errorf("wallet: domain mismatch (%s)", stated)
	}
	return nil
}

func (n *NonceStore) key(nonce string) string { return "wallet:nonce:" + nonce }
