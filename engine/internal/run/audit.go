package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Smohan-vcode/O365-Box/engine/internal/events"
	"github.com/Smohan-vcode/O365-Box/engine/internal/plan"
	"github.com/Smohan-vcode/O365-Box/engine/internal/state"
)

// ModeAudit reads only: each step runs test, verify and evidence; plan and
// apply are refused (SPEC §26, SC-26).
const ModeAudit Mode = "audit"

// ReadOnlyOperations are the only operations an audit may send a provider.
var ReadOnlyOperations = map[string]bool{"test": true, "verify": true, "evidence": true}

// ErrReadOnly is returned for any operation an audit may not run.
var ErrReadOnly = errors.New("refused in audit mode: only test, verify and evidence may run")

// ReadOnlyInvoker wraps an Invoker so that plan, apply or any other
// operation outside ReadOnlyOperations never reaches the provider. Observe,
// when set, receives each operation's raw result (the audit reads verify
// results setting by setting).
type ReadOnlyInvoker struct {
	Next    Invoker
	Observe func(step, operation string, raw json.RawMessage, err error)
}

// Invoke implements Invoker.
func (r *ReadOnlyInvoker) Invoke(ctx context.Context, provider, step, operation string, input any, progress func(string, int, int)) (json.RawMessage, error) {
	if !ReadOnlyOperations[operation] {
		return nil, fmt.Errorf("%s %s: %w", step, operation, ErrReadOnly)
	}
	raw, err := r.Next.Invoke(ctx, provider, step, operation, input, progress)
	if r.Observe != nil {
		r.Observe(step, operation, raw, err)
	}
	return raw, err
}

// readOnly wraps inv unless it is already read-only.
func readOnly(inv Invoker) Invoker {
	if _, ok := inv.(*ReadOnlyInvoker); ok {
		return inv
	}
	return &ReadOnlyInvoker{Next: inv}
}

// auditStep runs one step in audit mode: test, verify, evidence. Gates are
// not waited on and nothing is planned or applied.
func (x *Executor) auditStep(ctx context.Context, n *plan.Node) outcome {
	id := n.Step.ID
	var t TestResult
	if err := x.call(ctx, n, "test", &t); err != nil {
		return x.fail(id, err)
	}
	if !t.OK {
		x.skip(id, state.Skipped, "test: "+t.Reason)
		return outcome{status: state.Skipped}
	}
	var v VerifyResult
	if err := x.call(ctx, n, "verify", &v); err != nil {
		return x.fail(id, err)
	}
	ev := x.evidence(ctx, n)
	if !v.OK {
		x.emit(events.Event{Ev: events.StepChanged, Step: id, Msg: "differs from the catalogue", Data: map[string]any{"drift": changesData(v.Drift)}})
		x.record(id, state.Drifted, len(v.Drift), "", "audit: verify found differences", ev)
		return outcome{status: state.Drifted}
	}
	x.emit(events.Event{Ev: events.StepDone, Step: id, Msg: "verified"})
	x.record(id, state.Done, 0, "", "audit", ev)
	return outcome{status: state.Done}
}
