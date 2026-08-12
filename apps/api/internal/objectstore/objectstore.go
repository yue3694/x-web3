// Package objectstore 抽象对象存储（S3 / LocalStack / dev-fake）。
//
// 接口只暴露最小集：PresignPut / PresignGet / Head。
//
// 当前提供 FakeStore（dev/test）与 S3Store（production）。
package objectstore

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// Store 是 API 各业务模块使用的完整对象存储能力。
type Store interface {
	PresignPut(ctx context.Context, key, contentType string, size int64) (string, time.Time, error)
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, time.Time, error)
	Head(ctx context.Context, key string) (int64, error)
	PutBytes(ctx context.Context, key, contentType string, body []byte) error
	PublicURL(key string) string
	Region() string
	Bucket() string
}

// FakeStore 内存版对象存储：模拟 PUT/HEAD；presigned URL 形式合法但指向
// 本地端点（默认 "https://fake-s3.localhost"）。生产请替换为 S3Store。
type FakeStore struct {
	mu     sync.Mutex
	clock  func() time.Time
	region string
	bucket string
	host   string
	ttl    time.Duration
	secret []byte

	objects map[string]fakeObject
}

type fakeObject struct {
	contentType string
	size        int64
}

// NewFakeStore 构造 dev/test 用的 FakeStore。
func NewFakeStore() *FakeStore {
	return &FakeStore{
		clock:   time.Now,
		region:  "us-east-1",
		bucket:  "x-web3-media",
		host:    "https://fake-s3.localhost",
		ttl:     15 * time.Minute,
		secret:  []byte("dev-only-secret-do-not-use-in-prod"),
		objects: make(map[string]fakeObject),
	}
}

// SetTTL 覆盖默认 presign TTL（仍受调用方 maxTTL 限制）。
func (s *FakeStore) SetTTL(d time.Duration) { s.ttl = d }

// Put 直接放一个对象（dev/test 用，跳过 presign）。
func (s *FakeStore) Put(key, contentType string, size int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = fakeObject{contentType: contentType, size: size}
}

func (s *FakeStore) PutBytes(_ context.Context, key, contentType string, body []byte) error {
	s.Put(key, contentType, int64(len(body)))
	return nil
}

func (s *FakeStore) PublicURL(key string) string {
	return fmt.Sprintf("%s/%s/%s", s.host, s.bucket, key)
}

// PresignPut 生成 AWS SigV4 风格的伪签名 URL；请求到 fake host 时 Head 会成功。
func (s *FakeStore) PresignPut(ctx context.Context, key, contentType string, size int64) (string, time.Time, error) {
	return s.presign(ctx, "PUT", key, map[string]string{
		"content-type":   contentType,
		"content-length": strconv.FormatInt(size, 10),
	})
}

// PresignGet 生成 GET presigned URL。
func (s *FakeStore) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, time.Time, error) {
	if ttl <= 0 || ttl > s.ttl {
		ttl = s.ttl
	}
	q := map[string]string{}
	u, exp, err := s.presignWithTTL(ctx, "GET", key, q, ttl)
	if err != nil {
		return "", time.Time{}, err
	}
	return u, exp, nil
}

// Head 取对象 size，不存在返回 ErrObjectMissing。
func (s *FakeStore) Head(ctx context.Context, key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	obj, ok := s.objects[key]
	if !ok {
		return 0, errors.New("objectstore: object missing")
	}
	return obj.size, nil
}

func (s *FakeStore) presign(ctx context.Context, method, key string, headers map[string]string) (string, time.Time, error) {
	return s.presignWithTTL(ctx, method, key, headers, s.ttl)
}

func (s *FakeStore) presignWithTTL(ctx context.Context, method, key string, headers map[string]string, ttl time.Duration) (string, time.Time, error) {
	now := s.clock().UTC()
	exp := now.Add(ttl)
	canonical := fmt.Sprintf("%s\n%s\n%d\n%s",
		method, key, exp.Unix(),
		canonicalHeaders(headers))
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(canonical))
	sig := hex.EncodeToString(mac.Sum(nil))
	q := url.Values{}
	q.Set("X-Fake-Expires", strconv.FormatInt(exp.Unix(), 10))
	q.Set("X-Fake-Signature", sig)
	q.Set("X-Fake-Algorithm", "FAKE-HMAC-SHA256")
	for k, v := range headers {
		q.Set("X-Fake-"+k, v)
	}
	return fmt.Sprintf("%s/%s/%s?%s", s.host, s.bucket, key, q.Encode()), exp, nil
}

func canonicalHeaders(h map[string]string) string {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	// 简化：不做字典序排序；只用于 dev 假签名。
	out := ""
	for _, k := range keys {
		out += k + ":" + h[k] + ";"
	}
	return out
}

// Region / Bucket 暴露给日志。
func (s *FakeStore) Region() string { return s.region }
func (s *FakeStore) Bucket() string { return s.bucket }
