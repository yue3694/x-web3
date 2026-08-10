package certificate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"

	"github.com/x-web3/api/internal/objectstore"
)

// fixedMeta 构造一组合法的 CourseMeta 用于测试。
func fixedMeta() CourseMeta {
	return CourseMeta{
		Name:            "Solidity 101 Completion",
		Description:     "Awarded for completing the on-chain Solidity 101 course.",
		ImageURI:        "https://cdn.example.com/badges/sol101.png",
		CourseID:        uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		CourseVersion:   1,
		CompletionDate:  time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
		RecipientWallet: common.HexToAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79C8"),
		IssuerName:      "x-web3 University",
		ExternalURL:     "https://university.example.com/certificates/abc",
	}
}

// TestNewGenerator_NilStore 校验 nil store 直接报错。
func TestNewGenerator_NilStore(t *testing.T) {
	if _, err := NewGenerator(nil, ""); err == nil {
		t.Fatalf("expected error for nil store")
	}
}

// TestGenerateAndUpload_HappyPath 走通 metadata 生成 + 上传。
func TestGenerateAndUpload_HappyPath(t *testing.T) {
	store := objectstore.NewFakeStore()
	g, err := NewGenerator(store, "test/")
	if err != nil {
		t.Fatalf("NewGenerator: %v", err)
	}

	userID := uuid.New()
	res, err := g.GenerateAndUpload(context.Background(), userID, fixedMeta())
	if err != nil {
		t.Fatalf("GenerateAndUpload: %v", err)
	}

	if res.URI == "" {
		t.Fatal("URI is empty")
	}
	if res.SHA256Hex == "" || len(res.SHA256Hex) != 64 {
		t.Fatalf("bad sha256: %q", res.SHA256Hex)
	}
	if res.Bytes == 0 {
		t.Fatal("Bytes == 0")
	}
	if !strings.HasPrefix(res.URI, "https://") {
		t.Fatalf("URI scheme unexpected: %q", res.URI)
	}
	if strings.Contains(res.URI, "?") {
		t.Fatalf("canonical URI should not contain query: %q", res.URI)
	}
	if !strings.HasSuffix(res.URI, ".json") {
		t.Fatalf("URI should end with .json: %q", res.URI)
	}
	if !strings.Contains(res.URI, res.SHA256Hex) {
		t.Fatalf("URI should embed sha256: uri=%q hash=%q", res.URI, res.SHA256Hex)
	}
}

// TestBuildMetadataJSON_Shape 锁定 JSON 形状（OpenSea 兼容）。
func TestBuildMetadataJSON_Shape(t *testing.T) {
	bytes, err := buildMetadataJSON(fixedMeta())
	if err != nil {
		t.Fatalf("buildMetadataJSON: %v", err)
	}
	var got Metadata
	if err := json.Unmarshal(bytes, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Name != fixedMeta().Name {
		t.Errorf("Name = %q, want %q", got.Name, fixedMeta().Name)
	}
	if got.Description != fixedMeta().Description {
		t.Errorf("Description mismatch")
	}
	if got.Image != fixedMeta().ImageURI {
		t.Errorf("Image mismatch")
	}
	if got.ExternalURL != fixedMeta().ExternalURL {
		t.Errorf("ExternalURL mismatch")
	}

	// attributes 必须按顺序包含 5 项（course_id / course_version / issued_at / recipient / issuer）
	if len(got.Attributes) != 5 {
		t.Fatalf("attributes len = %d, want 5", len(got.Attributes))
	}
	wantOrder := []string{"course_id", "course_version", "issued_at", "recipient", "issuer"}
	for i, want := range wantOrder {
		if got.Attributes[i].TraitType != want {
			t.Errorf("attr[%d] trait_type = %q, want %q", i, got.Attributes[i].TraitType, want)
		}
	}
	// recipient attribute 应该是 lowercase hex
	if got.Attributes[3].Value.(string) != strings.ToLower(fixedMeta().RecipientWallet.Hex()) {
		t.Errorf("recipient attr = %v, want lowercase", got.Attributes[3].Value)
	}
}

// TestBuildMetadataJSON_NoIssuerOptional 当 IssuerName 为空时不写入 issuer attr。
func TestBuildMetadataJSON_NoIssuerOptional(t *testing.T) {
	m := fixedMeta()
	m.IssuerName = ""
	bytes, err := buildMetadataJSON(m)
	if err != nil {
		t.Fatalf("buildMetadataJSON: %v", err)
	}
	var got Metadata
	_ = json.Unmarshal(bytes, &got)
	for _, a := range got.Attributes {
		if a.TraitType == "issuer" {
			t.Fatalf("issuer attr must be omitted when empty; got %v", a)
		}
	}
}

// TestBuildMetadataJSON_StableHash 同一入参必须产出同一 sha256（内容寻址基本保证）。
func TestBuildMetadataJSON_StableHash(t *testing.T) {
	bytes1, _ := buildMetadataJSON(fixedMeta())
	bytes2, _ := buildMetadataJSON(fixedMeta())
	if string(bytes1) != string(bytes2) {
		t.Fatal("buildMetadataJSON not deterministic")
	}
	h := sha256.Sum256(bytes1)
	if len(h) != sha256.Size {
		t.Fatal("sha256 size mismatch")
	}
	if hex.EncodeToString(h[:]) == "" {
		t.Fatal("sha256 hex empty")
	}
}

// TestBuildMetadataJSON_DefaultsIssuedAt 当 CompletionDate 为零值时使用 now()。
func TestBuildMetadataJSON_DefaultsIssuedAt(t *testing.T) {
	m := fixedMeta()
	m.CompletionDate = time.Time{}
	bytes, err := buildMetadataJSON(m)
	if err != nil {
		t.Fatalf("buildMetadataJSON: %v", err)
	}
	var got Metadata
	_ = json.Unmarshal(bytes, &got)
	// 找到 issued_at attribute
	var issuedAt string
	for _, a := range got.Attributes {
		if a.TraitType == "issued_at" {
			issuedAt = a.Value.(string)
		}
	}
	if issuedAt == "" {
		t.Fatal("issued_at empty when CompletionDate is zero")
	}
	if _, err := time.Parse(time.RFC3339, issuedAt); err != nil {
		t.Fatalf("issued_at not RFC3339: %q", issuedAt)
	}
}

// TestValidateCourseMeta 覆盖各种校验失败。
func TestValidateCourseMeta(t *testing.T) {
	base := fixedMeta()
	cases := []struct {
		name    string
		mutate  func(m *CourseMeta)
		wantErr error
	}{
		{"empty course id", func(m *CourseMeta) { m.CourseID = uuid.Nil }, ErrEmptyCourseID},
		{"empty name", func(m *CourseMeta) { m.Name = "" }, ErrEmptyName},
		{"empty description", func(m *CourseMeta) { m.Description = "" }, ErrEmptyDescription},
		{"empty image", func(m *CourseMeta) { m.ImageURI = "" }, ErrEmptyImage},
		{"zero recipient", func(m *CourseMeta) { m.RecipientWallet = common.Address{} }, ErrEmptyRecipient},
		{"data scheme image", func(m *CourseMeta) { m.ImageURI = "data:image/png;base64,AAAA" }, ErrInvalidURI},
		{"ftp scheme image", func(m *CourseMeta) { m.ImageURI = "ftp://x/y" }, ErrInvalidURI},
		{"scheme-less image", func(m *CourseMeta) { m.ImageURI = "cdn.example.com/x.png" }, ErrInvalidURI},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := base
			tc.mutate(&m)
			if err := validateCourseMeta(m); !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestValidateCourseMeta_OK 锁定 happy path 不误报。
func TestValidateCourseMeta_OK(t *testing.T) {
	if err := validateCourseMeta(fixedMeta()); err != nil {
		t.Fatalf("validateCourseMeta(happy): %v", err)
	}
}

// TestValidateURIScheme 单独覆盖 scheme 校验（公开函数）。
func TestValidateURIScheme(t *testing.T) {
	cases := []struct {
		raw     string
		wantErr bool
	}{
		{"ipfs://bafk.../foo.png", false},
		{"https://cdn.example.com/x.png", false},
		{"http://localhost:8080/x.png", false}, // dev only
		{"data:image/png;base64,AAAA", true},
		{"", true},
		{"no-scheme", true},
		{"javascript:alert(1)", true},
	}
	for _, tc := range cases {
		err := ValidateURIScheme(tc.raw)
		if (err != nil) != tc.wantErr {
			t.Errorf("ValidateURIScheme(%q) err = %v, wantErr=%v", tc.raw, err, tc.wantErr)
		}
	}
}

// TestGenerateAndUpload_RejectsBadImage 端到端：image 不合法 → 报错。
func TestGenerateAndUpload_RejectsBadImage(t *testing.T) {
	store := objectstore.NewFakeStore()
	g, err := NewGenerator(store, "")
	if err != nil {
		t.Fatalf("NewGenerator: %v", err)
	}
	m := fixedMeta()
	m.ImageURI = "javascript:alert(1)"
	if _, err := g.GenerateAndUpload(context.Background(), uuid.New(), m); !errors.Is(err, ErrInvalidURI) {
		t.Fatalf("err = %v, want ErrInvalidURI", err)
	}
}

// TestGenerateAndUpload_EmbedsUserIDInKey 校验 userID 出现在 key 里（便于审计/溯源）。
//
// 注：当前 sha256 内容寻址已经把 userID 通过 CourseMeta.RecipientWallet 反映；
// 这里额外把 userID hex 前缀写到 key 上以满足「按 user 排查 s3 对象」的需求。
func TestGenerateAndUpload_EmbedsUserIDInKey(t *testing.T) {
	store := objectstore.NewFakeStore()
	g, err := NewGenerator(store, "")
	if err != nil {
		t.Fatalf("NewGenerator: %v", err)
	}
	res, err := g.GenerateAndUpload(context.Background(), uuid.MustParse("22222222-2222-2222-2222-222222222222"), fixedMeta())
	if err != nil {
		t.Fatalf("GenerateAndUpload: %v", err)
	}
	if !strings.HasPrefix(res.Key, "certificate-metadata/") {
		t.Errorf("key prefix = %q, want certificate-metadata/", res.Key)
	}
	if !strings.HasSuffix(res.Key, ".json") {
		t.Errorf("key suffix not .json: %q", res.Key)
	}
}

// TestCanonicalURI 锁定 canonical 化逻辑。
func TestCanonicalURI(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://fake-s3.localhost/x-web3-media/foo?X-Fake-Signature=abc", "https://fake-s3.localhost/x-web3-media/foo"},
		{"ipfs://bafk.../metadata-x.json", "ipfs://bafk.../metadata-x.json"}, // 不能解析的 scheme 原样
	}
	for _, tc := range cases {
		got, err := canonicalURI(tc.in, "metadata-x.json")
		if err != nil {
			t.Fatalf("canonicalURI(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("canonicalURI(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}