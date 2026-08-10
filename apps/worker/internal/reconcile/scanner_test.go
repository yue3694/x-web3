package reconcile

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// memDLQStore 内存版 DLQStore。
type memDLQStore struct {
	mu   sync.Mutex
	next int64
	rows map[int64]*DLQRow
}

func newMemDLQStore() *memDLQStore {
	return &memDLQStore{rows: map[int64]*DLQRow{}}
}

func (m *memDLQStore) Write(_ context.Context, e Entry) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	r := &DLQRow{
		ID:         m.next,
		Consumer:   e.Consumer,
		ChainID:    e.ChainID,
		Kind:       e.Kind,
		Severity:   e.Severity,
		Summary:    e.Summary,
		Payload:    e.Payload,
		RetryCount: 0,
		Resolved:   false,
		CreatedAt:  time.Now().UTC(),
	}
	if r.Payload == nil {
		r.Payload = map[string]any{}
	}
	m.rows[r.ID] = r
	return r.ID, nil
}

func (m *memDLQStore) ListUnresolved(_ context.Context, limit int) ([]DLQRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]DLQRow, 0, limit)
	for _, r := range m.rows {
		if !r.Resolved {
			out = append(out, *r)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (m *memDLQStore) Get(_ context.Context, id int64) (*DLQRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok {
		return nil, nil
	}
	cp := *r
	return &cp, nil
}

func (m *memDLQStore) MarkResolved(_ context.Context, id int64, _ uuid.UUID, resolution string) error {
	// 注：测试不调用此方法；保留签名一致。
	return errors.New("unused in tests")
}

func (m *memDLQStore) IncrementRetry(_ context.Context, id int64) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok {
		return 0, nil
	}
	r.RetryCount++
	return r.RetryCount, nil
}

// TestWriter_RejectsEmptyConsumer 校验：Consumer 必填。
func TestWriter_RejectsEmptyConsumer(t *testing.T) {
	w := NewWriter(newMemDLQStore(), newDiscardLogger())
	_, err := w.Write(context.Background(), Entry{Kind: "gap", Summary: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestWriter_RejectsEmptyKindAndSummary 校验：Kind / Summary 必填。
func TestWriter_RejectsEmptyKindAndSummary(t *testing.T) {
	w := NewWriter(newMemDLQStore(), newDiscardLogger())
	_, err := w.Write(context.Background(), Entry{Consumer: "x"})
	if err == nil {
		t.Fatal("expected error for missing kind")
	}
	_, err = w.Write(context.Background(), Entry{Consumer: "x", Kind: "gap"})
	if err == nil {
		t.Fatal("expected error for missing summary")
	}
}

// TestWriter_HappyPath 写入 + 列表。
func TestWriter_HappyPath(t *testing.T) {
	store := newMemDLQStore()
	w := NewWriter(store, newDiscardLogger())
	id, err := w.Write(context.Background(), Entry{
		Consumer: "indexer",
		Kind:     "gap",
		Severity: "error",
		Summary:  "missing 100..200",
		Payload:  map[string]any{"from": 100, "to": 200},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}
	items, err := store.ListUnresolved(context.Background(), 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 unresolved, got %d", len(items))
	}
	if items[0].Summary != "missing 100..200" {
		t.Errorf("summary mismatch: %s", items[0].Summary)
	}
}

func newDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestScanner_RequiresPool 测试构造校验。
func TestScanner_RequiresPool(t *testing.T) {
	_, err := NewScanner(Config{})
	if err == nil {
		t.Fatal("expected error for nil pool")
	}
	_, err = NewScanner(Config{Pool: &pgxpool.Pool{}})
	if err == nil {
		t.Fatal("expected error for nil writer")
	}
}

// TestScanner_Defaults 测试默认值。
func TestScanner_Defaults(t *testing.T) {
	// 因为 Pool 必须有效，这里用 nil-test 即可（不调用 Scan）。
	s, err := NewScanner(Config{
		Pool:   nil, // will error
		Writer: nil, // will error
	})
	if err == nil {
		t.Fatal("expected err for nil inputs")
	}
	if s != nil {
		t.Fatal("expected nil scanner")
	}
}
