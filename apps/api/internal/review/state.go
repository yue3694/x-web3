package review

import "fmt"

type Status string
type Action string

const (
	Draft         Status = "draft"
	PendingReview Status = "pending_review"
	Published     Status = "published"
	Archived      Status = "archived"

	Submit    Action = "submit"
	Approve   Action = "approve"
	Reject    Action = "reject"
	Archive   Action = "archive"
	Unarchive Action = "unarchive"
)

var transitions = map[Status]map[Action]Status{
	Draft:         {Submit: PendingReview},
	PendingReview: {Approve: Published, Reject: Draft},
	Published:     {Archive: Archived},
	Archived:      {Unarchive: Draft},
}

func Next(from Status, action Action) (Status, error) {
	to, ok := transitions[from][action]
	if !ok {
		return "", fmt.Errorf("invalid course transition: %s --%s--> ?", from, action)
	}
	return to, nil
}
