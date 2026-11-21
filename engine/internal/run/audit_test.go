package run

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Smohan-vcode/O365-Box/engine/internal/state"
)

// TestAuditModeNeverPlansOrApplies is T16-01's exit criterion: in audit
// mode no plan or apply operation reaches a provider.
func TestAuditModeNeverPlansOrApplies(t *testing.T) {
	c := catalog(t, chain+"  - {id: gate.keys, check_only: true}\n  - {id: guarded, gate: G-02}\n")
	f := newFake()
	f.have["one"], f.have["two"] = true, true // three and side differ: verify fails
	st := openState(t)
	x, _ := executor(t, c, f, st)
	sum, err := x.Run(context.Background(), ModeAudit)
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range f.calls {
		op := call[strings.LastIndex(call, ":")+1:]
		if !ReadOnlyOperations[op] {
			t.Errorf("audit mode sent %s to the provider", call)
		}
	}
	if f.count("one:verify") != 1 || f.count("one:evidence") != 1 || f.count("one:test") != 1 {
		t.Errorf("calls = %v", f.calls)
	}
	if sum.Status[state.Done] < 2 || len(sum.Drifted) == 0 || len(sum.Waiting) != 0 {
		t.Errorf("summary = %+v", sum)
	}
	if len(f.have) != 2 {
		t.Errorf("audit changed the fake tenant: %v", f.have)
	}
}

// The guard holds at the invoker too: a direct plan or apply call through
// the audit invoker is refused before it reaches the provider.
func TestReadOnlyInvokerRefusesWrites(t *testing.T) {
	f := newFake()
	var seen []string
	r := &ReadOnlyInvoker{Next: f, Observe: func(step, op string, _ json.RawMessage, _ error) { seen = append(seen, step+":"+op) }}
	for _, op := range []string{"plan", "apply", "delete", ""} {
		if _, err := r.Invoke(context.Background(), "p", "one", op, nil, nil); !errors.Is(err, ErrReadOnly) {
			t.Errorf("%q: err = %v, want ErrReadOnly", op, err)
		}
	}
	if len(f.calls) != 0 || len(seen) != 0 {
		t.Fatalf("refused operations reached the provider: %v %v", f.calls, seen)
	}
	if _, err := r.Invoke(context.Background(), "p", "one", "verify", nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != "one:verify" {
		t.Errorf("observe = %v", seen)
	}
	// Run wraps any invoker in audit mode, and does not wrap twice.
	if readOnly(r) != Invoker(r) {
		t.Error("read-only invoker wrapped twice")
	}
}
