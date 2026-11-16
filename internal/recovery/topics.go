// Package recovery defines explicit operator decisions for ambiguous topic
// creation. It supplies no provider writes or automatic adoption heuristics.
package recovery

import (
	"context"
	"errors"
	"regexp"
)

var ErrConflict = errors.New("topic review no longer matches the inspected state")

// TopicSnapshot captures the inspected job, effect and destination. Its request
// digest lets reconciliation reject a decision based on changed evidence.
type TopicSnapshot struct {
	AccountID     string `json:"accountId"`
	Job           int64  `json:"job"`
	Dedup         string `json:"dedup"`
	Kind          string `json:"kind"`
	State         string `json:"state"`
	Effect        string `json:"effect"`
	EffectState   string `json:"effectState"`
	RequestDigest string `json:"requestDigest"`
	Management    int64  `json:"management"`
	Name          string `json:"name"`
	Scope         string `json:"scope"`
	SourceChat    string `json:"sourceChat,omitempty"`
	Blocked       string `json:"blocked,omitempty"`
}

// TopicDecision records explicit operator evidence for adopting a created topic
// or confirming its absence. OperationID identifies an idempotent journal entry;
// expected states and digest bind the decision to the inspected snapshot.
type TopicDecision struct {
	OperationID         string `json:"operationId"`
	AccountID           string `json:"accountId"`
	Job                 int64  `json:"job"`
	Effect              string `json:"effect"`
	ExpectedState       string `json:"expectedState"`
	ExpectedEffectState string `json:"expectedEffectState"`
	RequestDigest       string `json:"requestDigest"`
	Action              string `json:"action"`
	Management          int64  `json:"management"`
	ThreadID            int64  `json:"threadId,omitempty"`
	ConfirmCreated      bool   `json:"confirmCreated,omitempty"`
	ConfirmNoTopic      bool   `json:"confirmNoTopic,omitempty"`
}

// TopicResult is the saved outcome of one accepted reconciliation decision.
type TopicResult struct {
	Job      int64  `json:"job"`
	Effect   string `json:"effect"`
	Action   string `json:"action"`
	ThreadID int64  `json:"threadId,omitempty"`
	State    string `json:"state"`
}

var operationID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
var digest = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Valid checks decision shape and mutually exclusive confirmations. The store
// must additionally compare the decision with current locked state.
func (d TopicDecision) Valid() bool {
	if !operationID.MatchString(d.OperationID) || d.AccountID == "" || d.Job <= 0 || d.Effect == "" || !digest.MatchString(d.RequestDigest) || d.Management >= 0 {
		return false
	}
	if d.ExpectedState != "failed" && d.ExpectedState != "uncertain" && d.ExpectedState != "done" {
		return false
	}
	if d.ExpectedEffectState != "pending" && d.ExpectedEffectState != "uncertain" && d.ExpectedEffectState != "done" && d.ExpectedEffectState != "unprepared" {
		return false
	}
	switch d.Action {
	case "adopt":
		return d.ConfirmCreated && !d.ConfirmNoTopic && d.ThreadID > 1
	case "absent":
		return d.ConfirmNoTopic && !d.ConfirmCreated && d.ThreadID == 0
	default:
		return false
	}
}

// Topics separates inspection from evidence-bound reconciliation. Implementations
// must reject stale decisions and preserve original requests and receipts.
type Topics interface {
	InspectTopic(context.Context, int64) (TopicSnapshot, error)
	ReconcileTopic(context.Context, TopicDecision) (TopicResult, error)
}
