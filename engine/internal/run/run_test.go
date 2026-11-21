package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Smohan-vcode/O365-Box/engine/internal/events"
	"github.com/Smohan-vcode/O365-Box/engine/internal/plan"
	"github.com/Smohan-vcode/O365-Box/engine/internal/state"
)

// fake is an in-memory tenant: each step owns one resource that apply
// creates. Behaviour per step can be changed to simulate failures.
type fake struct {
	mu        sync.Mutex
	have      map[string]bool
	failApply map[string]int // remaining failures
	failTest  map[string]string
	gateOpen  map[string]bool
	calls     []string
	block     chan struct{} // if set, apply of "block" waits on it
	token     string
}

func newFake() *fake {
	return &fake{have: map[string]bool{}, failApply: map[string]int{}, failTest: map[string]string{}, gateOpen: map[string]bool{}}
}

func (f *fake) Invoke(ctx context.Context, provider, step, op string, input any, progress func(string, int, int)) (json.RawMessage, error) {
	f.mu.Lock()
	f.calls = append(f.calls, step+":"+op)
	f.mu.Unlock()
	res := func(v any) (json.RawMessage, error) { b, _ := json.Marshal(v); return b, nil }
	switch op {
	case "test":
		if r, ok := f.failTest[step]; ok {
			return res(TestResult{OK: false, Reason: r})
		}
		return res(TestResult{OK: true})
	case "plan":
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.have[step] {
			return res(PlanResult{Changes: []Change{}})
		}
		return res(PlanResult{Changes: []Change{{Action: "create", Target: step}}})
	case "apply":
		if step == "block" && f.block != nil {
			select {
			case <-f.block:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.failApply[step] > 0 {
			f.failApply[step]--
			return nil, fmt.Errorf("transient failure with %s", f.token)
		}
		progress("creating "+step, 1, 1)
		f.have[step] = true
		return res(PlanResult{Changes: []Change{{Action: "create", Target: step}}})
	case "verify":
		f.mu.Lock()
		defer f.mu.Unlock()
		if strings.HasPrefix(step, "gate.") {
			return res(VerifyResult{OK: f.gateOpen[step]})
		}
		return res(VerifyResult{OK: f.have[step]})
	case "evidence":
		return res(map[string]any{"items": []any{map[string]any{"kind": "export", "ref": "evidence/" + step + ".json"}}})
	}
	return nil, errors.New("unknown op " + op)
}

func (f *fake) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func catalog(t *testing.T, steps string) *plan.Catalog {
	t.Helper()
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "p"), 0o755)
	if err := os.WriteFile(filepath.Join(dir, "p", "steps.yaml"), []byte("provider: p\nsteps:\n"+steps), 0o644); err != nil {
		t.Fatal(err)
	}
	gf := ""
	if strings.Contains(steps, "gate.keys") {
		gf = filepath.Join(dir, "gates.yaml")
		_ = os.WriteFile(gf, []byte("gates:\n  - {id: G-02, title: Keys, guide: Register keys, check: gate.keys}\n"), 0o644)
	}
	c, err := plan.Load(dir, gf)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

const chain = `  - {id: one}
  - {id: two, depends_on: [one]}
  - {id: three, depends_on: [two]}
  - {id: side, depends_on: [one]}
`

func executor(t *testing.T, c *plan.Catalog, f *fake, st *state.File) (*Executor, *events.Recorder) {
	t.Helper()
	g, err := plan.Build(c, plan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	rec := &events.Recorder{}
	return &Executor{Graph: g, Catalog: c, Invoker: f, Events: rec, State: st, Retries: 2, Backoff: time.Millisecond, GatePoll: 5 * time.Millisecond}, rec
}

func openState(t *testing.T) *state.File {
	st, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestApplyThenNoChanges(t *testing.T) {
	c := catalog(t, chain)
	f := newFake()
	st := openState(t)
	x, rec := executor(t, c, f, st)
	sum, err := x.Run(context.Background(), ModeApply)
	if err != nil || !sum.OK() || sum.Changes != 4 || sum.Status[state.Done] != 4 {
		t.Fatalf("first run: %+v %v", sum, err)
	}
	if len(rec.Of(events.StepProg)) != 4 || len(rec.Of(events.StepChanged)) != 4 {
		t.Error("progress or change events missing")
	}
	if r := st.Get("three"); r == nil || r.Status != state.Done || len(r.Evidence) != 1 {
		t.Errorf("state = %+v", r)
	}
	applies := f.count("one:apply")
	x2, _ := executor(t, c, f, st)
	sum, err = x2.Run(context.Background(), ModeApply)
	if err != nil || sum.Changes != 0 || sum.Status[state.Done] != 4 {
		t.Fatalf("second run: %+v %v", sum, err)
	}
	if f.count("one:apply") != applies {
		t.Error("second run applied again")
	}
}

func TestFailedDependencyBlocksDependents(t *testing.T) {
	c := catalog(t, chain)
	f := newFake()
	f.failApply["two"] = 99
	x, rec := executor(t, c, f, openState(t))
	sum, _ := x.Run(context.Background(), ModeApply)
	if len(sum.Failed) != 1 || sum.Failed[0] != "two" || sum.Status[state.Blocked] != 1 || sum.Status[state.Done] != 2 {
		t.Fatalf("summary %+v", sum)
	}
	if f.count("three:") != 0 {
		t.Error("dependent of a failed step was started")
	}
	if f.count("two:apply") != 3 {
		t.Errorf("apply attempts = %d, want 3 (1 + 2 retries)", f.count("two:apply"))
	}
	skipped := rec.Of(events.StepSkipped)
	if len(skipped) != 1 || skipped[0].Step != "three" || !strings.Contains(skipped[0].Msg, "dependency two") {
		t.Errorf("skip events %+v", skipped)
	}
}

func TestRetrySucceeds(t *testing.T) {
	c := catalog(t, chain)
	f := newFake()
	f.failApply["one"] = 2
	x, rec := executor(t, c, f, openState(t))
	sum, _ := x.Run(context.Background(), ModeApply)
	if !sum.OK() {
		t.Fatalf("%+v", sum)
	}
	warnings := 0
	for _, e := range rec.Of(events.Log) {
		if e.Level == "warning" {
			warnings++
		}
	}
	if warnings != 2 {
		t.Errorf("retry warnings = %d", warnings)
	}
}

func TestResumeAfterInterruption(t *testing.T) {
	c := catalog(t, "  - {id: one}\n  - {id: block, depends_on: [one]}\n  - {id: after, depends_on: [block]}\n")
	f := newFake()
	f.block = make(chan struct{})
	st := openState(t)
	x, _ := executor(t, c, f, st)
	x.Timeout = 50 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for f.count("block:apply") == 0 {
			time.Sleep(time.Millisecond)
		}
		cancel()
	}()
	sum, _ := x.Run(ctx, ModeApply)
	if st.Get("one") == nil || st.Get("one").Status != state.Done || f.count("after:") != 0 {
		t.Fatalf("interrupted run: %+v", sum)
	}
	close(f.block)
	appliesOne := f.count("one:apply")
	x2, _ := executor(t, c, f, st)
	sum, err := x2.Run(context.Background(), ModeApply)
	if err != nil || !sum.OK() {
		t.Fatalf("resume: %+v %v", sum, err)
	}
	if f.count("one:apply") != appliesOne {
		t.Error("resume re-applied a finished step")
	}
	if st.Get("after").Status != state.Done {
		t.Error("resume did not continue to the end")
	}
}

func TestGateNonInteractiveAndInteractive(t *testing.T) {
	c := catalog(t, "  - {id: gate.keys, check_only: true}\n  - {id: guarded, gate: G-02}\n  - {id: next, depends_on: [guarded]}\n")
	f := newFake()
	x, rec := executor(t, c, f, openState(t))
	sum, _ := x.Run(context.Background(), ModeApply)
	if len(sum.Waiting) != 1 || sum.Waiting[0] != "G-02" || f.count("guarded:apply") != 0 || f.count("next:") != 0 {
		t.Fatalf("non-interactive: %+v", sum)
	}
	if w := rec.Of(events.GateWaiting); len(w) != 1 || w[0].Data["guide"] != "Register keys" {
		t.Errorf("gate.waiting %+v", w)
	}
	x2, rec2 := executor(t, c, f, openState(t))
	x2.Interactive = true
	go func() {
		time.Sleep(20 * time.Millisecond)
		f.mu.Lock()
		f.gateOpen["gate.keys"] = true
		f.mu.Unlock()
	}()
	sum, _ = x2.Run(context.Background(), ModeApply)
	if !sum.OK() || len(rec2.Of(events.GatePassed)) != 1 {
		t.Fatalf("interactive: %+v", sum)
	}
}

func TestPlanAndDriftChangeNothing(t *testing.T) {
	c := catalog(t, chain)
	f := newFake()
	x, rec := executor(t, c, f, openState(t))
	sum, _ := x.Run(context.Background(), ModePlan)
	if sum.Changes != 4 || sum.Status[state.Planned] != 4 || f.count("one:apply")+f.count("two:apply") != 0 {
		t.Fatalf("plan: %+v", sum)
	}
	if p := rec.Of(events.StepPlanned); len(p) != 4 || p[0].Data["plan_hash"] == "" {
		t.Error("plan events missing")
	}
	f.have = map[string]bool{"one": true, "two": true, "three": true}
	x, _ = executor(t, c, f, openState(t))
	sum, _ = x.Run(context.Background(), ModeDrift)
	if len(sum.Drifted) != 1 || sum.Drifted[0] != "side" || f.count("side:apply") != 0 {
		t.Fatalf("drift: %+v", sum)
	}
}

func TestOptionalStepSkippedOnFailedTest(t *testing.T) {
	c := catalog(t, "  - {id: one}\n  - {id: opt, optional: true}\n  - {id: req}\n")
	f := newFake()
	f.failTest["opt"] = "not available"
	f.failTest["req"] = "no permission"
	x, _ := executor(t, c, f, openState(t))
	sum, _ := x.Run(context.Background(), ModeApply)
	if sum.Status[state.Skipped] != 1 || len(sum.Failed) != 1 || sum.Failed[0] != "req" {
		t.Fatalf("%+v", sum)
	}
}

func TestRedactionReachesEveryEvent(t *testing.T) {
	c := catalog(t, chain)
	f := newFake()
	f.token = "TOPSECRET-value-123"
	f.failApply["one"] = 99
	rec := &events.Recorder{}
	red := events.NewRedactor(rec, []string{"ssn"})
	red.AddSecret(f.token)
	g, _ := plan.Build(c, plan.Options{})
	x := &Executor{Graph: g, Catalog: c, Invoker: f, Events: red, Retries: 1, Backoff: time.Millisecond}
	_, _ = x.Run(context.Background(), ModeApply)
	red.Emit(events.Event{Ev: events.Log, Data: map[string]any{"ssn": "123-45-6789", "nested": map[string]any{"SSN": "x"}}})
	b, _ := json.Marshal(rec.Events)
	if strings.Contains(string(b), f.token) || strings.Contains(string(b), "123-45-6789") {
		t.Errorf("secret or never_store value in events: %s", b)
	}
	if !strings.Contains(string(b), "[redacted]") || !strings.Contains(string(b), "[not stored]") {
		t.Error("redaction markers missing")
	}
}

func TestParallelism(t *testing.T) {
	var steps strings.Builder
	for i := 0; i < 8; i++ {
		fmt.Fprintf(&steps, "  - {id: s%d}\n", i)
	}
	c := catalog(t, steps.String())
	inv := &slow{d: 50 * time.Millisecond}
	g, _ := plan.Build(c, plan.Options{})
	x := &Executor{Graph: g, Catalog: c, Invoker: inv, Parallel: 4}
	t0 := time.Now()
	sum, _ := x.Run(context.Background(), ModePlan)
	if !sum.OK() {
		t.Fatal(sum)
	}
	if inv.max != 4 {
		t.Errorf("max concurrent = %d, want 4", inv.max)
	}
	if d := time.Since(t0); d > 600*time.Millisecond {
		t.Errorf("parallel run took %s", d)
	}
}

type slow struct {
	mu       sync.Mutex
	d        time.Duration
	cur, max int
}

func (s *slow) Invoke(ctx context.Context, provider, step, op string, input any, progress func(string, int, int)) (json.RawMessage, error) {
	s.mu.Lock()
	s.cur++
	if s.cur > s.max {
		s.max = s.cur
	}
	s.mu.Unlock()
	time.Sleep(s.d)
	s.mu.Lock()
	s.cur--
	s.mu.Unlock()
	if op == "test" {
		return json.RawMessage(`{"ok":true}`), nil
	}
	return json.RawMessage(`{"changes":[]}`), nil
}
