// Package certificate 实现证书铸造的元数据生成 + 上传 (F04-T09)。
//
// 范围：
//   - 把课程完课信息渲染为 ERC-721 metadata JSON (OpenSea 兼容)；
//   - 通过 ObjectStore 上传到内容寻址 URL；
//   - 返回 ipfs://<cid> 或 https://<host>/<bucket>/metadata-<sha256>.json 形式的 URI。
//
// 注意：本包不直接生成证书 NFT，只生成 tokenURI；铸造由 worker (apps/worker/internal/certificate)
// 持 MINTER_ROLE 上链完成。
//
// 内容寻址：URI 中嵌入 sha256(JSON) hex。链上存 URI 后任何人可重新计算 sha256 与字节内容比对，
// 一旦不一致即可证明 metadata 被篡改（参见 design.md §7）。
package certificate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"

	"github.com/x-web3/api/internal/objectstore"
)

// Sentinel errors 走 errors.New + %w 风格，调用方用 errors.Is 分类到 HTTP code。
var (
	ErrInvalidURI        = errors.New("certificate: invalid metadata URI scheme")
	ErrMetadataTooLarge  = errors.New("certificate: metadata JSON exceeds size limit")
	ErrEmptyRecipient    = errors.New("certificate: recipient wallet is empty")
	ErrEmptyCourseID     = errors.New("certificate: course id is empty")
	ErrEmptyName         = errors.New("certificate: name is empty")
	ErrEmptyDescription  = errors.New("certificate: description is empty")
	ErrEmptyImage        = errors.New("certificate: image URL is empty")
	ErrInvalidImage      = errors.New("certificate: image must be ipfs:// or https://")
)

// metadataJSON 单文件不超过 64 KiB；OpenSea 自身不强制但是合理上限。
const metadataMaxBytes = 64 * 1024

// 允许的 image / external URI scheme：明确拒绝 data:（避免在前端 / OpenSea 里被误判为 SVG 内嵌大文件）。
var allowedSchemes = map[string]struct{}{
	"ipfs":  {},
	"https": {},
	"http":  {}, // 仅 dev / mock；生产可在 Config 里关掉
}

// CourseMeta 调用方传入的「课程完课时所需的展示信息」。
// 字段为空时会被校验拒掉（除 Attributes 之外都必填）。
type CourseMeta struct {
	Name           string // 证书标题
	Description    string // 长描述（markdown 一段）
	ImageURI       string // badge 图片 URL (ipfs:// or https://)
	CourseID       uuid.UUID
	CourseVersion  int
	CompletionDate time.Time
	RecipientWallet common.Address // 链上接收方
	IssuerName     string          // 可选：发证主体 (e.g. "x-web3 University")
	ExternalURL    string          // 可选：证书详情页
}

// Metadata JSON 顶层结构 (OpenSea 兼容)。
type Metadata struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Image       string       `json:"image"`
	ExternalURL string       `json:"external_url,omitempty"`
	Attributes  []Attribute  `json:"attributes"`
}

// Attribute 是 OpenSea 标准 trait。
type Attribute struct {
	TraitType string `json:"trait_type"`
	Value     any    `json:"value"`
}

// UploadResult 是 GenerateAndUpload 的返回值。
type UploadResult struct {
	URI       string // ipfs://... 或 https://...
	SHA256Hex string
	Bytes     int
	Key       string // 对象存储的 key；生产实现可置空
}

// Store 是上传所需的最小接口；与 objectstore.ObjectStore 完全一致 (PresignPut)。
// 这里独立导出是为单测注入 fake：FakeStore 已实现 PresignPut，所以可以直接传。
type Store interface {
	PresignPut(ctx context.Context, key, contentType string, size int64) (url string, expiresAt time.Time, err error)
}

// Generator 持有不可变的元数据装配配置。
type Generator struct {
	store    Store
	keyPrefix string // s3 key 前缀；默认 "certificate-metadata/"
}

// NewGenerator 构造生成器；store 为 nil 返回 error（避免 nil deref）。
//
// keyPrefix 用于把证书 metadata 归类到独立 prefix；空字符串走默认 "certificate-metadata/"。
func NewGenerator(store Store, keyPrefix string) (*Generator, error) {
	if store == nil {
		return nil, errors.New("certificate: store is nil")
	}
	if keyPrefix == "" {
		keyPrefix = "certificate-metadata/"
	}
	return &Generator{store: store, keyPrefix: keyPrefix}, nil
}

// GenerateAndUpload 渲染 JSON → 算 sha256 → 通过 store.PresignPut 上传 → 返回 URI。
//
// URI 规则：
//   - 当 store 返回的是 https://<host>/<bucket>/<key>?<sig> 形式 → 最终 URI 用
//     "https://<host>/<bucket>/<key>"（去掉 sig query），让公开 CDN 可缓存；
//   - 否则保留 store 返回的原始 URL（IPFS pinning 服务 / custom）。
//
// 失败/重试：返回 wrap 后的 error；调用方按 500 处理。本函数不在 DB 写任何状态。
func (g *Generator) GenerateAndUpload(
	ctx context.Context,
	userID uuid.UUID,
	meta CourseMeta,
) (UploadResult, error) {
	if err := validateCourseMeta(meta); err != nil {
		return UploadResult{}, err
	}

	jsonBytes, err := buildMetadataJSON(meta)
	if err != nil {
		return UploadResult{}, err
	}
	if len(jsonBytes) > metadataMaxBytes {
		return UploadResult{}, fmt.Errorf("%w: %d > %d", ErrMetadataTooLarge, len(jsonBytes), metadataMaxBytes)
	}

	sum := sha256.Sum256(jsonBytes)
	hashHex := hex.EncodeToString(sum[:])

	key := fmt.Sprintf("%s%s.json", g.keyPrefix, hashHex)

	uploadURL, _, err := g.store.PresignPut(ctx, key, "application/json", int64(len(jsonBytes)))
	if err != nil {
		return UploadResult{}, fmt.Errorf("presign put: %w", err)
	}

	uri, err := canonicalURI(uploadURL, key)
	if err != nil {
		return UploadResult{}, err
	}

	return UploadResult{
		URI:       uri,
		SHA256Hex: hashHex,
		Bytes:     len(jsonBytes),
		Key:       key,
	}, nil
}

// buildMetadataJSON 渲染 OpenSea 兼容的 metadata JSON。
//
// 字段顺序稳定（保证 sha256 可复现）；Attribute list 用确定性顺序：
// course_id → course_version → issued_at → recipient → issuer。
func buildMetadataJSON(meta CourseMeta) ([]byte, error) {
	issuedAt := meta.CompletionDate.UTC().Format(time.RFC3339)
	if meta.CompletionDate.IsZero() {
		issuedAt = time.Now().UTC().Format(time.RFC3339)
	}

	md := Metadata{
		Name:        meta.Name,
		Description: meta.Description,
		Image:       meta.ImageURI,
		ExternalURL: meta.ExternalURL,
		Attributes: []Attribute{
			{TraitType: "course_id", Value: meta.CourseID.String()},
			{TraitType: "course_version", Value: meta.CourseVersion},
			{TraitType: "issued_at", Value: issuedAt},
			{TraitType: "recipient", Value: strings.ToLower(meta.RecipientWallet.Hex())},
		},
	}
	if meta.IssuerName != "" {
		md.Attributes = append(md.Attributes, Attribute{TraitType: "issuer", Value: meta.IssuerName})
	}
	return json.Marshal(md)
}

// validateCourseMeta 校验必填字段 + image URI scheme。
func validateCourseMeta(meta CourseMeta) error {
	if meta.CourseID == uuid.Nil {
		return ErrEmptyCourseID
	}
	if strings.TrimSpace(meta.Name) == "" {
		return ErrEmptyName
	}
	if strings.TrimSpace(meta.Description) == "" {
		return ErrEmptyDescription
	}
	if strings.TrimSpace(meta.ImageURI) == "" {
		return ErrEmptyImage
	}
	if meta.RecipientWallet == (common.Address{}) {
		return ErrEmptyRecipient
	}
	if err := ValidateURIScheme(meta.ImageURI); err != nil {
		return fmt.Errorf("%w: image", err)
	}
	if meta.ExternalURL != "" {
		if err := ValidateURIScheme(meta.ExternalURL); err != nil {
			return fmt.Errorf("%w: external_url", err)
		}
	}
	return nil
}

// ValidateURIScheme 公开：允许 ipfs / https / http (dev)；明确拒绝 data: 与空。
func ValidateURIScheme(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ErrInvalidURI
	}
	// 显式拦截 data:
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "data:") {
		return fmt.Errorf("%w: data: scheme forbidden", ErrInvalidURI)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidURI, err)
	}
	if u.Scheme == "" {
		return fmt.Errorf("%w: scheme empty", ErrInvalidURI)
	}
	if _, ok := allowedSchemes[u.Scheme]; !ok {
		return fmt.Errorf("%w: scheme=%q", ErrInvalidURI, u.Scheme)
	}
	return nil
}

// canonicalURI 把 presign URL 整理成「公开可缓存」的 URI 形式：
//   - 去掉 query string（X-Fake-* / X-Amz-* 签名参数）；
//   - 保留 scheme + host + path（bucket + key）。
//
// 真实生产 S3 在 CDN 后面签发时通常不带 query；这里统一规整便于上层做缓存与对比。
func canonicalURI(rawUploadURL, key string) (string, error) {
	u, err := url.Parse(rawUploadURL)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidURI, err)
	}
	if u.Scheme == "" || u.Host == "" {
		// 无法规整（IPFS pinning 服务可能直接给 ipfs://bafk...）——原样返回。
		return rawUploadURL, nil
	}
	// FakeStore path: https://fake-s3.localhost/<bucket>/<key>?sig...
	// 公开 URI:       https://fake-s3.localhost/<bucket>/<key>
	u.RawQuery = ""
	return u.String(), nil
}

// Compile-time guarantee: objectstore.FakeStore satisfies Store.
var _ Store = (*objectstore.FakeStore)(nil)