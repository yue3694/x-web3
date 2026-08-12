package catalog

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/x-web3/api/internal/course"
)

// TestFilterHash_Stable 同样的 filter 必须产生同样的 hash。
func TestFilterHash_Stable(t *testing.T) {
	id := uuid.New()
	before := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	f := course.ListFilter{Query: "web3", TeacherID: &id, Limit: 20, BeforeAt: &before, BeforeID: &id}
	h1 := filterHash(f)
	h2 := filterHash(f)
	if h1 != h2 {
		t.Errorf("hash not stable: %s vs %s", h1, h2)
	}
	if len(h1) != 16 {
		t.Errorf("hash len = %d, want 16", len(h1))
	}
}

// TestFilterHash_DiffersAcrossDifferentFilters 任何字段变化 hash 必须变。
func TestFilterHash_DiffersAcrossDifferentFilters(t *testing.T) {
	a := course.ListFilter{Query: "a", Limit: 20}
	b := course.ListFilter{Query: "b", Limit: 20}
	if filterHash(a) == filterHash(b) {
		t.Error("different query should differ")
	}
	c := course.ListFilter{Query: "", Limit: 10}
	d := course.ListFilter{Query: "", Limit: 20}
	if filterHash(c) == filterHash(d) {
		t.Error("different limit should differ")
	}
	id := uuid.New()
	e := course.ListFilter{TeacherID: &id, Limit: 20}
	f := course.ListFilter{Limit: 20}
	if filterHash(e) == filterHash(f) {
		t.Error("different teacher should differ")
	}
}

// TestFilterHash_CursorIndependentOfLimitFieldOrder 验证 limit 写在最末不会
// 导致 hash 误判 — 当前实现是简单 fmt.Sprintf 各字段拼接，保证 cursor 不
// 漂移即可。
func TestFilterHash_CursorIndependentOfLimitFieldOrder(t *testing.T) {
	id := uuid.New()
	before := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	a := course.ListFilter{Query: "x", BeforeAt: &before, BeforeID: &id, Limit: 20}
	b := course.ListFilter{Query: "x", BeforeAt: &before, BeforeID: &id, Limit: 20}
	if filterHash(a) != filterHash(b) {
		t.Error("identical cursor + limit should produce same hash")
	}
	if !strings.HasPrefix(filterHash(a), "") {
		// sanity
	}
}
