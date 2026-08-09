// Package media 实现媒体上传意图 / finalize 校验 / 列表查询。
//
// 流程：
//
//	teacher → POST /media/upload-intent {fileName, contentType, size}
//	         ← {mediaAssetId, uploadUrl, expiresAt}
//
//	teacher → PUT 到 uploadUrl (S3 / LocalStack)
//
//	teacher → POST /media/{id}/finalize {checksumSha256}
//	         ← {mediaAsset}  // status: draft → ready
//
// 限制：
//   - content-type 必须白名单（mp4 / webm / mov / pdf）
//   - size 上限默认 2 GiB
//   - presigned PUT 仅允许声明的 Content-Type / Content-Length
//   - finalize 时再用 HeadObject 校验对象存在 + size 一致
//   - finalize 时再次比对 checksum（optional，客户端可不上送）
//
// S3 客户端通过 ObjectStore 接口注入；生产用 AWS SDK v2，测试用 fake。
package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound      = errors.New("media: not found")
	ErrForbidden     = errors.New("media: not the owner")
	ErrAlreadyReady  = errors.New("media: already finalized")
	ErrBadMIME       = errors.New("media: content-type not allowed")
	ErrSizeTooLarge  = errors.New("media: size exceeds limit")
	ErrChecksumBad   = errors.New("media: checksum mismatch")
	ErrObjectMissing = errors.New("media: object missing in store")
	ErrBadStatus     = errors.New("media: not in draft status")
)

// Asset 是 media_assets 的镜像。
type Asset struct {
	ID            uuid.UUID
	OwnerUserID   uuid.UUID
	S3Key         string
	ContentType   string
	SizeBytes     int64
	Status        string
	ScanStatus    string
	ChecksumSHA256 string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// 默认 size 上限 2 GiB。
const DefaultMaxBytes = 2 * 1024 * 1024 * 1024

// allowList MIME 白名单。
var allowList = map[string]struct{}{
	"video/mp4":       {},
	"video/webm":      {},
	"video/quicktime": {},
	"application/pdf": {},
}

// ObjectStore 抽象 S3 客户端，便于测试用 fake。
type ObjectStore interface {
	// PresignPut 返回 PUT 用的 presigned URL 与过期时间。
	PresignPut(ctx context.Context, key, contentType string, size int64) (url string, expiresAt time.Time, err error)
	// Head 返回对象 size；不存在返回 ErrObjectMissing。
	Head(ctx context.Context, key string) (size int64, err error)
}

// Repo media_assets 数据访问。
type Repo struct{ pool *pgxpool.Pool }

func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// Pool 暴露底层 pool 给上层订阅（如 catalog 失效）。
func (r *Repo) Pool() *pgxpool.Pool { return r.pool }

// CreateIntent 在 DB 落一条 draft media_assets 并返回 presigned URL。
//
// fileName 用于派生扩展名；contentType 必须命中白名单；size > 0 且 ≤ 上限。
func (r *Repo) CreateIntent(ctx context.Context, owner uuid.UUID, fileName, contentType string, size int64, store ObjectStore, maxBytes int64) (*Asset, string, time.Time, error) {
	if size <= 0 {
		return nil, "", time.Time{}, fmt.Errorf("%w: size must be positive", ErrBadMIME)
	}
	if _, ok := allowList[contentType]; !ok {
		return nil, "", time.Time{}, fmt.Errorf("%w: %s", ErrBadMIME, contentType)
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	if size > maxBytes {
		return nil, "", time.Time{}, fmt.Errorf("%w: %d > %d", ErrSizeTooLarge, size, maxBytes)
	}
	assetID := uuid.New()
	key := buildKey(assetID, contentType, fileName)
	url, exp, err := store.PresignPut(ctx, key, contentType, size)
	if err != nil {
		return nil, "", time.Time{}, fmt.Errorf("presign: %w", err)
	}
	var a Asset
	err = r.pool.QueryRow(ctx, `INSERT INTO media_assets(id,owner_user_id,s3_key,content_type,size_bytes)
VALUES($1,$2,$3,$4,$5)
RETURNING id,owner_user_id,s3_key,content_type,size_bytes,status,scan_status,checksum_sha256,created_at,updated_at`,
		assetID, owner, key, contentType, size).Scan(
		&a.ID, &a.OwnerUserID, &a.S3Key, &a.ContentType, &a.SizeBytes, &a.Status, &a.ScanStatus, &a.ChecksumSHA256, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, "", time.Time{}, err
	}
	return &a, url, exp, nil
}

// GetByID 取一条 asset；不存在返回 ErrNotFound。
func (r *Repo) GetByID(ctx context.Context, id uuid.UUID) (*Asset, error) {
	var a Asset
	err := r.pool.QueryRow(ctx, `SELECT id,owner_user_id,s3_key,content_type,size_bytes,status,scan_status,checksum_sha256,created_at,updated_at
FROM media_assets WHERE id=$1`, id).Scan(
		&a.ID, &a.OwnerUserID, &a.S3Key, &a.ContentType, &a.SizeBytes, &a.Status, &a.ScanStatus, &a.ChecksumSHA256, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ListByOwner 列出 owner 的全部 asset（按 created_at desc）。
func (r *Repo) ListByOwner(ctx context.Context, owner uuid.UUID, limit int) ([]Asset, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `SELECT id,owner_user_id,s3_key,content_type,size_bytes,status,scan_status,checksum_sha256,created_at,updated_at
FROM media_assets WHERE owner_user_id=$1 ORDER BY created_at DESC LIMIT $2`, owner, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Asset, 0, limit)
	for rows.Next() {
		var a Asset
		if err := rows.Scan(&a.ID, &a.OwnerUserID, &a.S3Key, &a.ContentType, &a.SizeBytes, &a.Status, &a.ScanStatus, &a.ChecksumSHA256, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Finalize 校验对象存在 + size 一致；checksum 为空时跳过 sha256 校验（仍要求 Head）。
//
// owner 必须等于 asset 的 owner_user_id。
func (r *Repo) Finalize(ctx context.Context, id, owner uuid.UUID, checksumHex string, store ObjectStore) (*Asset, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var a Asset
	err = tx.QueryRow(ctx, `SELECT id,owner_user_id,s3_key,content_type,size_bytes,status,scan_status,checksum_sha256,created_at,updated_at
FROM media_assets WHERE id=$1 FOR UPDATE`, id).Scan(
		&a.ID, &a.OwnerUserID, &a.S3Key, &a.ContentType, &a.SizeBytes, &a.Status, &a.ScanStatus, &a.ChecksumSHA256, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if a.OwnerUserID != owner {
		return nil, ErrForbidden
	}
	if a.Status != "draft" {
		return nil, ErrAlreadyReady
	}
	objSize, err := store.Head(ctx, a.S3Key)
	if err != nil {
		return nil, fmt.Errorf("head: %w", err)
	}
	if objSize != a.SizeBytes {
		return nil, fmt.Errorf("%w: declared %d, head %d", ErrSizeTooLarge, a.SizeBytes, objSize)
	}
	if checksumHex != "" {
		normalized := strings.ToLower(strings.TrimPrefix(checksumHex, "0x"))
		if _, err := hex.DecodeString(normalized); err != nil {
			return nil, fmt.Errorf("%w: invalid hex", ErrChecksumBad)
		}
		if normalized != a.ChecksumSHA256 && a.ChecksumSHA256 != "" {
			return nil, fmt.Errorf("%w: server has stored %s, client claims %s", ErrChecksumBad, a.ChecksumSHA256, normalized)
		}
	}
	_, err = tx.Exec(ctx, `UPDATE media_assets SET status='ready', checksum_sha256=COALESCE(NULLIF($2,''),checksum_sha256), updated_at=now() WHERE id=$1`, id, strings.ToLower(strings.TrimPrefix(checksumHex, "0x")))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetByID(ctx, id)
}

// buildKey 生成私有 S3 key：courses/{course_id?}/lessons/{lesson_id?}/raw/{uuid}.{ext}。
//
// 当前简化版只到 owner：{owner}/{asset_id}.{ext}，F04 引入 lesson 关联后
// 再调整 key 模板。
func buildKey(assetID uuid.UUID, contentType, fileName string) string {
	ext := strings.ToLower(extension(fileName, contentType))
	now := time.Now().UTC()
	return fmt.Sprintf("media/%04d/%02d/%02d/%s%s",
		now.Year(), now.Month(), now.Day(),
		assetID.String(),
		"."+ext,
	)
}

func extension(fileName, contentType string) string {
	if idx := strings.LastIndex(fileName, "."); idx >= 0 && idx < len(fileName)-1 {
		return fileName[idx+1:]
	}
	switch contentType {
	case "video/mp4":
		return "mp4"
	case "video/webm":
		return "webm"
	case "video/quicktime":
		return "mov"
	case "application/pdf":
		return "pdf"
	}
	return "bin"
}

// hashKey 帮助单测断言 buildKey 的稳定性。
func HashKey(assetID uuid.UUID) string {
	h := sha256.Sum256([]byte(assetID.String()))
	return hex.EncodeToString(h[:4])
}
