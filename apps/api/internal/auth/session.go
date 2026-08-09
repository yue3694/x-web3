package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// SessionData 是 sid 对应的平台 session。
// 存 Redis：JSON + HMAC 签名防篡改。
type SessionData struct {
	UserID      string    `json:"user_id"`
	Subject     string    `json:"subject"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	Fingerprint string    `json:"fp,omitempty"` // UA hash；可选用作防劫持
}

// SessionStore 负责 session 的签发、读取、销毁。
type SessionStore struct {
	rdb    *redis.Client
	secret []byte
	ttl    time.Duration
}

func NewSessionStore(rdb *redis.Client, secret []byte, ttl time.Duration) *SessionStore {
	return &SessionStore{rdb: rdb, secret: secret, ttl: ttl}
}

const sessionPrefix = "session:"

func (s *SessionStore) key(sid string) string { return sessionPrefix + sid }

// Issue 生成 sid 并写入 Redis；返回 sid 供 cookie 设置。
func (s *SessionStore) Issue(ctx context.Context, subject string, fp string) (string, *SessionData, error) {
	sid, err := NewSessionID()
	if err != nil {
		return "", nil, err
	}
	now := time.Now().UTC()
	d := &SessionData{
		Subject:     subject,
		CreatedAt:   now,
		ExpiresAt:   now.Add(s.ttl),
		Fingerprint: fp,
	}
	if err := s.write(ctx, sid, d); err != nil {
		return "", nil, err
	}
	return sid, d, nil
}

// Read 校验签名后返回 session 数据。
func (s *SessionStore) Read(ctx context.Context, sid string) (*SessionData, error) {
	raw, err := s.rdb.Get(ctx, s.key(sid)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.verifyAndDecode(raw)
}

// Refresh atomically rotates an existing session id and extends its TTL.
// The old id is revoked before the new id is returned to prevent replay.
func (s *SessionStore) Refresh(ctx context.Context, sid string, fp string) (string, *SessionData, error) {
	if sid == "" {
		return "", nil, errors.New("session: id required")
	}
	d, err := s.Read(ctx, sid)
	if err != nil || d == nil {
		if err == nil {
			err = errors.New("session: not found")
		}
		return "", nil, err
	}
	if fp != "" {
		d.Fingerprint = fp
	}
	newSID, err := NewSessionID()
	if err != nil {
		return "", nil, err
	}
	now := time.Now().UTC()
	d.CreatedAt = now
	d.ExpiresAt = now.Add(s.ttl)
	if err := s.write(ctx, newSID, d); err != nil {
		return "", nil, err
	}
	if err := s.Destroy(ctx, sid); err != nil {
		_ = s.Destroy(ctx, newSID)
		return "", nil, err
	}
	return newSID, d, nil
}

// Destroy revokes a session id. It is idempotent for logout retries.
func (s *SessionStore) Destroy(ctx context.Context, sid string) error {
	return s.rdb.Del(ctx, s.key(sid)).Err()
}

// write 用 HMAC 签名后 JSON 存 Redis。
func (s *SessionStore) write(ctx context.Context, sid string, d *SessionData) error {
	body, err := json.Marshal(d)
	if err != nil {
		return err
	}
	mac := s.sign(body)
	v := base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(mac)
	return s.rdb.Set(ctx, s.key(sid), v, s.ttl).Err()
}

func (s *SessionStore) verifyAndDecode(raw string) (*SessionData, error) {
	parts := splitOnce(raw, '.')
	if parts == nil {
		return nil, errors.New("session: malformed")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("session: body b64: %w", err)
	}
	got, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("session: mac b64: %w", err)
	}
	want := s.sign(body)
	if !hmac.Equal(got, want) {
		return nil, errors.New("session: signature mismatch")
	}
	var d SessionData
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, err
	}
	if time.Now().After(d.ExpiresAt) {
		return nil, errors.New("session: expired")
	}
	return &d, nil
}

func (s *SessionStore) sign(body []byte) []byte {
	m := hmac.New(sha256.New, s.secret)
	m.Write(body)
	return m.Sum(nil)
}

func splitOnce(s string, sep byte) []string {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return []string{s[:i], s[i+1:]}
		}
	}
	return nil
}
