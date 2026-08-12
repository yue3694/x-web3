package learning

import (
	"testing"
	"time"
)

// TestSetTTL_ClampToMax 验证 SetTTL 不会让 ttl 超过 maxTTL（5 min 硬上限）。
func TestSetTTL_ClampToMax(t *testing.T) {
	s := &Service{ttl: time.Minute}
	s.SetTTL(10 * time.Minute)
	if s.ttl != maxTTL {
		t.Errorf("ttl = %v, want %v", s.ttl, maxTTL)
	}
	s.SetTTL(0)
	if s.ttl != maxTTL {
		t.Errorf("ttl with 0 = %v, want %v", s.ttl, maxTTL)
	}
	s.SetTTL(2 * time.Minute)
	if s.ttl != 2*time.Minute {
		t.Errorf("ttl = %v, want 2m", s.ttl)
	}
}
