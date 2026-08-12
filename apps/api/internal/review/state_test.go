package review

import "testing"

func TestTransitions(t *testing.T) {
	tests := []struct {
		from   Status
		action Action
		want   Status
	}{
		{Draft, Submit, PendingReview},
		{PendingReview, Approve, Published},
		{PendingReview, Reject, Draft},
		{Published, Archive, Archived},
		{Archived, Unarchive, Draft},
	}
	for _, tt := range tests {
		got, err := Next(tt.from, tt.action)
		if err != nil || got != tt.want {
			t.Fatalf("Next(%s,%s) = %s,%v; want %s", tt.from, tt.action, got, err, tt.want)
		}
	}
}

func TestInvalidTransitions(t *testing.T) {
	for _, status := range []Status{Draft, PendingReview, Published, Archived} {
		for _, action := range []Action{Submit, Approve, Reject, Archive, Unarchive} {
			if _, valid := transitions[status][action]; valid {
				continue
			}
			if _, err := Next(status, action); err == nil {
				t.Fatalf("Next(%s,%s) should fail", status, action)
			}
		}
	}
}
