// Package run executes a step graph: test, plan, apply, verify and
// evidence for each step, in dependency order, in parallel where possible,
// with gates, retries and resume (SPEC §14).
package run

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Change is one planned or applied change.
type Change struct {
	Action string `json:"action"` // create | update | delete
	Target string `json:"target"`
	Detail string `json:"detail,omitempty"`
}

// TestResult is the result of the test operation.
type TestResult struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

// PlanResult is the result of the plan and apply operations.
type PlanResult struct {
	Changes []Change `json:"changes"`
}

// VerifyResult is the result of the verify operation.
type VerifyResult struct {
	OK    bool     `json:"ok"`
	Drift []Change `json:"drift,omitempty"`
}

// EvidenceResult is the result of the evidence operation.
type EvidenceResult struct {
	Items []struct {
		Kind    string `json:"kind"`
		Ref     string `json:"ref"`
		Summary string `json:"summary,omitempty"`
	} `json:"items"`
}

// Digest identifies one planned change of a step for approval (SPEC §25.1):
// the hex sha256 of step|action|target|detail. A change whose detail
// differs has a different digest and needs its own approval.
func Digest(step string, c Change) string {
	h := sha256.Sum256([]byte(step + "|" + c.Action + "|" + c.Target + "|" + c.Detail))
	return hex.EncodeToString(h[:])
}

func hashChanges(c []Change) string {
	if len(c) == 0 {
		return ""
	}
	b, _ := json.Marshal(c)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}

func changesData(c []Change) []any {
	out := make([]any, len(c))
	for i, x := range c {
		out[i] = map[string]any{"action": x.Action, "target": x.Target, "detail": x.Detail}
	}
	return out
}
