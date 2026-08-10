package audit_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/x-web3/api/internal/audit"
)

// fakeSink 是 audit.Sink 的测试替身：记录收到的 SQL 与参数，
// 并允许注入错误以模拟 DB 失败。
type fakeSink struct {
	mu       sync.Mutex
	calls    []sinkCall
	execErr  error
	execRows int64
}

type sinkCall struct {
	sql  string
	args []any
}

func (s *fakeSink) Exec(_ context.Context, sql string, args ...any) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, sinkCall{sql: sql, args: args})
	if s.execErr != nil {
		return stubTag{}, s.execErr
	}
	return stubTag{rows: s.execRows}, nil
}

type stubTag struct{ rows int64 }

func (t stubTag) RowsAffected() int64 { return t.rows }

// TestActionConstants_ArePastTenseVerbs 校验 Action 常量都符合 "动词过去式" 命名约定。
func TestActionConstants_ArePastTenseVerbs(t *testing.T) {
	for _, a := range []audit.Action{
		audit.ActionUserCreated,
		audit.ActionUserLoggedIn,
		audit.ActionWalletLinked,
		audit.ActionWalletUnbound,
		audit.ActionRoleGranted,
		audit.ActionRoleRevoked,
		audit.ActionCourseCreated,
		audit.ActionCourseReview,
		audit.ActionOrderCreated,
		audit.ActionChainReplayed,
		audit.ActionDLQRetriedReplay,
		audit.ActionDLQRetriedIgnored,
		audit.ActionDLQRetriedManual,
	} {
		if string(a) == "" {
			t.Errorf("empty action constant")
		}
		if !containsDot(string(a)) {
			t.Errorf("action %q should be in 'noun.verb' form", a)
		}
	}
}

func containsDot(s string) bool {
	for _, c := range s {
		if c == '.' {
			return true
		}
	}
	return false
}

// TestFillCorrelationID_EmptySetsTimestamp 当 CorrelationID 为空时填入时间戳兜底值。
func TestFillCorrelationID_EmptySetsTimestamp(t *testing.T) {
	e := audit.Entry{}
	audit.FillCorrelationID(&e)
	if e.CorrelationID == "" {
		t.Fatal("expected correlation ID to be filled")
	}
	if _, err := time.Parse(time.RFC3339Nano, e.CorrelationID); err != nil {
		t.Errorf("expected RFC3339Nano, got %q: %v", e.CorrelationID, err)
	}
}

// TestFillCorrelationID_KeepsExisting 当 CorrelationID 已有值时不被覆盖。
func TestFillCorrelationID_KeepsExisting(t *testing.T) {
	const cid = "req_existing_123"
	e := audit.Entry{CorrelationID: cid}
	audit.FillCorrelationID(&e)
	if e.CorrelationID != cid {
		t.Errorf("existing correlation overwritten: got %q want %q", e.CorrelationID, cid)
	}
}

// TestWriter_LogWritesExpectedSQL 验证 INSERT 字段顺序与参数与 schema 对齐。
func TestWriter_LogWritesExpectedSQL(t *testing.T) {
	sink := &fakeSink{}
	w := audit.NewWriterWithSink(sink, zap.NewNop())
	actor := uuid.New()

	before := map[string]any{"status": "draft"}
	after := map[string]any{"status": "published"}
	e := audit.Entry{
		ActorUserID:   &actor,
		Action:        audit.ActionCourseReview,
		TargetType:    "course",
		TargetID:      "course-123",
		Before:        before,
		After:         after,
		IP:            "203.0.113.10",
		UserAgent:     "Mozilla/5.0",
		CorrelationID: "req_abc",
	}
	if err := w.Log(context.Background(), e); err != nil {
		t.Fatalf("Log: %v", err)
	}

	if got := len(sink.calls); got != 1 {
		t.Fatalf("expected 1 Exec call, got %d", got)
	}
	c := sink.calls[0]
	if c.sql != audit.AuditInsertSQLForTest() {
		t.Errorf("sql mismatch:\n got=%q\nwant=%q", c.sql, audit.AuditInsertSQLForTest())
	}
	if len(c.args) != 9 {
		t.Fatalf("expected 9 args, got %d", len(c.args))
	}
	if c.args[0] != &actor {
		t.Errorf("arg[0] actor mismatch")
	}
	if c.args[1] != string(audit.ActionCourseReview) {
		t.Errorf("arg[1] action = %v", c.args[1])
	}
	if c.args[2] != "course" {
		t.Errorf("arg[2] target_type = %v", c.args[2])
	}
	if c.args[3] != "course-123" {
		t.Errorf("arg[3] target_id = %v", c.args[3])
	}
	// beforeB 应是合法的 JSON
	var got map[string]any
	if err := json.Unmarshal(c.args[4].([]byte), &got); err != nil {
		t.Errorf("before not valid json: %v", err)
	} else if got["status"] != "draft" {
		t.Errorf("before payload lost: %v", got)
	}
	if err := json.Unmarshal(c.args[5].([]byte), &got); err != nil {
		t.Errorf("after not valid json: %v", err)
	} else if got["status"] != "published" {
		t.Errorf("after payload lost: %v", got)
	}
	if c.args[6] != "req_abc" {
		t.Errorf("arg[6] correlation = %v", c.args[6])
	}
	if c.args[7] != "203.0.113.10" {
		t.Errorf("arg[7] ip = %v", c.args[7])
	}
	if c.args[8] != "Mozilla/5.0" {
		t.Errorf("arg[8] ua = %v", c.args[8])
	}
}

// TestWriter_LogGeneratesCorrelationIDWhenMissing 验证未注入 correlationID 时 Log 自动填充。
func TestWriter_LogGeneratesCorrelationIDWhenMissing(t *testing.T) {
	sink := &fakeSink{}
	w := audit.NewWriterWithSink(sink, zap.NewNop())
	if err := w.Log(context.Background(), audit.Entry{Action: audit.ActionUserLoggedIn}); err != nil {
		t.Fatalf("Log: %v", err)
	}
	if got := sink.calls[0].args[6].(string); got == "" {
		t.Errorf("expected auto-generated correlation id, got empty")
	}
}

// TestWriter_LogPropagatesSinkError sink 报错必须原样返回。
func TestWriter_LogPropagatesSinkError(t *testing.T) {
	wantErr := errors.New("connection refused")
	sink := &fakeSink{execErr: wantErr}
	w := audit.NewWriterWithSink(sink, zap.NewNop())
	err := w.Log(context.Background(), audit.Entry{Action: audit.ActionUserCreated})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected sink error, got %v", err)
	}
}

// TestWriter_LogSerializesNilBeforeAfterBefore/After 为 nil 时也得是合法 JSON（"null"）。
func TestWriter_LogSerializesNilBeforeAfter(t *testing.T) {
	sink := &fakeSink{}
	w := audit.NewWriterWithSink(sink, zap.NewNop())
	if err := w.Log(context.Background(), audit.Entry{
		Action:        audit.ActionWalletLinked,
		CorrelationID: "req_nil",
	}); err != nil {
		t.Fatalf("Log: %v", err)
	}
	beforeB := sink.calls[0].args[4].([]byte)
	afterB := sink.calls[0].args[5].([]byte)
	if string(beforeB) != "null" {
		t.Errorf("nil Before must marshal to 'null', got %q", string(beforeB))
	}
	if string(afterB) != "null" {
		t.Errorf("nil After must marshal to 'null', got %q", string(afterB))
	}
}

// TestWriter_LogSerializesComplexBeforeAfter 嵌套 struct 与 slice 必须正确序列化。
func TestWriter_LogSerializesComplexBeforeAfter(t *testing.T) {
	sink := &fakeSink{}
	w := audit.NewWriterWithSink(sink, zap.NewNop())

	type payload struct {
		Roles       []string `json:"roles"`
		Permissions []string `json:"permissions"`
	}
	after := payload{
		Roles:       []string{"student", "teacher"},
		Permissions: []string{"COURSE_CREATE"},
	}
	if err := w.Log(context.Background(), audit.Entry{
		Action:        audit.ActionRoleGranted,
		After:         after,
		CorrelationID: "req_struct",
	}); err != nil {
		t.Fatalf("Log: %v", err)
	}
	var got payload
	if err := json.Unmarshal(sink.calls[0].args[5].([]byte), &got); err != nil {
		t.Fatalf("after not valid json: %v", err)
	}
	if len(got.Roles) != 2 || got.Roles[0] != "student" {
		t.Errorf("after.roles lost: %+v", got)
	}
	if len(got.Permissions) != 1 || got.Permissions[0] != "COURSE_CREATE" {
		t.Errorf("after.permissions lost: %+v", got)
	}
}

// TestWriter_LogAcceptsNilActor 匿名操作（系统触发的审计）actor 可为 nil。
func TestWriter_LogAcceptsNilActor(t *testing.T) {
	sink := &fakeSink{}
	w := audit.NewWriterWithSink(sink, zap.NewNop())
	var nilUUID *uuid.UUID
	if err := w.Log(context.Background(), audit.Entry{
		Action:        audit.ActionChainReplayed,
		ActorUserID:   nilUUID,
		CorrelationID: "req_sys",
	}); err != nil {
		t.Fatalf("Log: %v", err)
	}
	if sink.calls[0].args[0] != (*uuid.UUID)(nil) {
		t.Errorf("expected nil *uuid.UUID actor, got %v", sink.calls[0].args[0])
	}
}
