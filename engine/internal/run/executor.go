package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/Smohan-vcode/O365-Box/engine/internal/events"
	"github.com/Smohan-vcode/O365-Box/engine/internal/plan"
	"github.com/Smohan-vcode/O365-Box/engine/internal/state"
)

// Mode is what a run does.
type Mode string

const (
	ModePlan  Mode = "plan"  // test and plan; change nothing
	ModeApply Mode = "apply" // full contract
	ModeDrift Mode = "drift" // plan and verify; change nothing
	ModeCheck Mode = "check" // verify only
)

// Invoker runs one operation of a step on its provider.
type Invoker interface {
	Invoke(ctx context.Context, provider, step, operation string, input any, progress func(msg string, done, of int)) (json.RawMessage, error)
}

// Executor runs a graph.
type Executor struct {
	Graph    *plan.Graph
	Catalog  *plan.Catalog
	Invoker  Invoker
	Events   events.Sink
	State    *state.File
	Input    func(step *plan.Step) any // builds each step's input from the configuration
	Parallel int                       // default 4
	Retries  int                       // apply retries after the first attempt; default 2
	Backoff  time.Duration             // first retry delay; default 1s, doubles
	Timeout  time.Duration             // per operation; default 10m
	// Interactive gates poll their check every GatePoll until it passes or
	// the context ends. Non-interactive runs stop at a waiting gate.
	Interactive bool
	GatePoll    time.Duration
	RunID       string
	Now         func() time.Time
}

// Summary is the outcome of a run.
type Summary struct {
	Run      string         `json:"run"`
	Mode     Mode           `json:"mode"`
	Status   map[string]int `json:"status"`  // count by state status
	Changes  int            `json:"changes"` // planned or applied
	Failed   []string       `json:"failed,omitempty"`
	Waiting  []string       `json:"waiting,omitempty"` // gates
	Drifted  []string       `json:"drifted,omitempty"`
	Duration time.Duration  `json:"duration"`
}

// OK reports whether nothing failed, drifted or is waiting.
func (s Summary) OK() bool { return len(s.Failed) == 0 && len(s.Waiting) == 0 && len(s.Drifted) == 0 }

type outcome struct {
	status  string
	changes int
}

// Run executes every node of the graph in dependency order.
func (x *Executor) Run(ctx context.Context, mode Mode) (Summary, error) {
	x.defaults()
	start := x.Now()
	x.emit(events.Event{Ev: events.RunStarted, Data: map[string]any{"mode": string(mode), "steps": len(x.Graph.Order)}})

	var (
		mu       sync.Mutex
		results  = map[string]outcome{}
		started  = map[string]bool{}
		sum      = Summary{Run: x.RunID, Mode: mode, Status: map[string]int{}}
		wg       sync.WaitGroup
		sem      = make(chan struct{}, x.Parallel)
		progress = make(chan struct{}, len(x.Graph.Order)+1)
	)
	finished := func(id string, o outcome) {
		mu.Lock()
		results[id] = o
		sum.Status[o.status]++
		sum.Changes += o.changes
		switch o.status {
		case state.Failed:
			sum.Failed = append(sum.Failed, id)
		case state.Drifted:
			sum.Drifted = append(sum.Drifted, id)
		case state.Waiting:
			if g := x.Graph.Nodes[id].Step.Gate; g != "" {
				sum.Waiting = append(sum.Waiting, g)
			}
		}
		mu.Unlock()
		progress <- struct{}{}
	}

	for {
		mu.Lock()
		if len(results) == len(x.Graph.Order) {
			mu.Unlock()
			break
		}
		var ready []string
		for _, id := range x.Graph.Order {
			if started[id] {
				continue
			}
			n := x.Graph.Nodes[id]
			blocked, waitingOn := "", false
			for _, d := range n.DependsOn {
				r, ok := results[d]
				if !ok {
					waitingOn = true
					break
				}
				if r.status == state.Failed || r.status == state.Blocked || r.status == state.Waiting {
					blocked = d
				}
			}
			if waitingOn {
				continue
			}
			started[id] = true
			if blocked != "" {
				mu.Unlock()
				x.skip(id, state.Blocked, fmt.Sprintf("dependency %s did not finish (%s)", blocked, results[blocked].status))
				finished(id, outcome{status: state.Blocked})
				mu.Lock()
				continue
			}
			ready = append(ready, id)
		}
		mu.Unlock()
		for _, id := range ready {
			id := id
			wg.Add(1)
			go func() {
				defer wg.Done()
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					finished(id, outcome{status: state.Failed})
					return
				}
				o := x.step(ctx, mode, x.Graph.Nodes[id])
				<-sem
				finished(id, o)
			}()
		}
		if len(ready) == 0 {
			<-progress
		}
	}
	wg.Wait()
	sort.Strings(sum.Failed)
	sort.Strings(sum.Drifted)
	sort.Strings(sum.Waiting)
	sum.Duration = x.Now().Sub(start)
	x.emit(events.Event{Ev: events.RunFinished, Data: map[string]any{
		"mode": string(mode), "changes": sum.Changes, "failed": toAny(sum.Failed),
		"waiting": toAny(sum.Waiting), "drifted": toAny(sum.Drifted), "status": statusAny(sum.Status)}})
	return sum, ctx.Err()
}

func (x *Executor) defaults() {
	if x.Parallel <= 0 {
		x.Parallel = 4
	}
	if x.Retries < 0 {
		x.Retries = 0
	}
	if x.Backoff == 0 {
		x.Backoff = time.Second
	}
	if x.Timeout == 0 {
		x.Timeout = 10 * time.Minute
	}
	if x.GatePoll == 0 {
		x.GatePoll = 15 * time.Second
	}
	if x.Now == nil {
		x.Now = time.Now
	}
	if x.RunID == "" {
		x.RunID = "r-" + strconv.FormatInt(x.Now().Unix(), 36)
	}
	if x.Input == nil {
		x.Input = func(*plan.Step) any { return nil }
	}
}

func (x *Executor) emit(e events.Event) {
	if e.T.IsZero() {
		e.T = x.Now().UTC()
	}
	e.Run = x.RunID
	if x.Events != nil {
		x.Events.Emit(e)
	}
}

func (x *Executor) record(id, status string, changes int, hash, reason string, evidence []string) {
	if x.State == nil {
		return
	}
	if err := x.State.Put(state.Record{Step: id, Status: status, Changes: changes, PlanHash: hash,
		Reason: reason, Evidence: evidence, Run: x.RunID, At: x.Now().UTC()}); err != nil {
		x.emit(events.Event{Ev: events.Log, Level: "error", Step: id, Msg: "state not saved: " + err.Error()})
	}
}

func (x *Executor) skip(id, status, reason string) {
	x.emit(events.Event{Ev: events.StepSkipped, Step: id, Msg: reason, Data: map[string]any{"status": status}})
	x.record(id, status, 0, "", reason, nil)
}

func (x *Executor) fail(id string, err error) outcome {
	x.emit(events.Event{Ev: events.StepFailed, Step: id, Msg: err.Error()})
	x.record(id, state.Failed, 0, "", err.Error(), nil)
	return outcome{status: state.Failed}
}

func (x *Executor) call(ctx context.Context, n *plan.Node, op string, out any) error {
	c, cancel := context.WithTimeout(ctx, x.Timeout)
	defer cancel()
	raw, err := x.Invoker.Invoke(c, n.Step.Provider, n.Step.ID, op, x.Input(n.Step), func(msg string, done, of int) {
		x.emit(events.Event{Ev: events.StepProg, Step: n.Step.ID, Msg: msg, Done: done, Of: of})
	})
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if out != nil && len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("%s: unexpected result %s: %w", op, truncate(string(raw)), err)
		}
	}
	return nil
}

func (x *Executor) step(ctx context.Context, mode Mode, n *plan.Node) outcome {
	id := n.Step.ID
	if n.Skip != "" {
		x.skip(id, state.Skipped, n.Skip)
		return outcome{status: state.Skipped}
	}
	if err := ctx.Err(); err != nil {
		return x.fail(id, err)
	}
	data := map[string]any{"title": n.Step.Title, "provider": n.Step.Provider, "mode": string(mode)}
	if n.Replaces != "" {
		data["replaces"] = n.Replaces
	}
	x.emit(events.Event{Ev: events.StepStarted, Step: id, Data: data})

	if mode == ModeCheck {
		var v VerifyResult
		if err := x.call(ctx, n, "verify", &v); err != nil {
			return x.fail(id, err)
		}
		if !v.OK {
			x.emit(events.Event{Ev: events.StepChanged, Step: id, Msg: "drift found", Data: map[string]any{"drift": changesData(v.Drift)}})
			x.record(id, state.Drifted, len(v.Drift), "", "verify failed", nil)
			return outcome{status: state.Drifted, changes: len(v.Drift)}
		}
		x.emit(events.Event{Ev: events.StepDone, Step: id, Msg: "verified"})
		return outcome{status: state.Done}
	}

	if gid := n.Step.Gate; gid != "" && mode == ModeApply {
		if o, ok := x.gate(ctx, n, x.Catalog.Gates[gid]); !ok {
			return o
		}
	}

	var t TestResult
	if err := x.call(ctx, n, "test", &t); err != nil {
		return x.fail(id, err)
	}
	if !t.OK {
		if n.Step.Optional {
			x.skip(id, state.Skipped, "test: "+t.Reason)
			return outcome{status: state.Skipped}
		}
		return x.fail(id, fmt.Errorf("test: %s", t.Reason))
	}

	var p PlanResult
	if err := x.call(ctx, n, "plan", &p); err != nil {
		return x.fail(id, err)
	}
	hash := hashChanges(p.Changes)
	x.emit(events.Event{Ev: events.StepPlanned, Step: id, Msg: fmt.Sprintf("%d change(s)", len(p.Changes)),
		Data: map[string]any{"changes": changesData(p.Changes), "plan_hash": hash}})

	switch mode {
	case ModePlan:
		x.emit(events.Event{Ev: events.StepDone, Step: id, Msg: "planned"})
		return outcome{status: state.Planned, changes: len(p.Changes)}
	case ModeDrift:
		var v VerifyResult
		if err := x.call(ctx, n, "verify", &v); err != nil {
			return x.fail(id, err)
		}
		if len(p.Changes) > 0 || !v.OK {
			x.emit(events.Event{Ev: events.StepChanged, Step: id, Msg: "drift found", Data: map[string]any{"drift": changesData(append(p.Changes, v.Drift...))}})
			x.record(id, state.Drifted, len(p.Changes), hash, "declared state differs", nil)
			return outcome{status: state.Drifted, changes: len(p.Changes)}
		}
		x.emit(events.Event{Ev: events.StepDone, Step: id, Msg: "no drift"})
		return outcome{status: state.Done}
	}

	// ModeApply. Resume: nothing to change and verify passes means done.
	applied := 0
	if len(p.Changes) == 0 {
		var v VerifyResult
		if err := x.call(ctx, n, "verify", &v); err != nil {
			return x.fail(id, err)
		}
		if v.OK {
			ev := x.evidence(ctx, n)
			x.emit(events.Event{Ev: events.StepDone, Step: id, Msg: "no changes", Data: map[string]any{"changes": 0}})
			x.record(id, state.Done, 0, "", "", ev)
			return outcome{status: state.Done}
		}
	}
	var a PlanResult
	var err error
	delay := x.Backoff
	for attempt := 0; ; attempt++ {
		a = PlanResult{}
		err = x.call(ctx, n, "apply", &a)
		if err == nil || attempt >= x.Retries || ctx.Err() != nil {
			break
		}
		x.emit(events.Event{Ev: events.Log, Level: "warning", Step: id,
			Msg: fmt.Sprintf("apply failed, retrying in %s: %v", delay, err)})
		select {
		case <-time.After(delay):
		case <-ctx.Done():
		}
		delay *= 2
	}
	if err != nil {
		return x.fail(id, err)
	}
	applied = len(a.Changes)
	for _, c := range a.Changes {
		x.emit(events.Event{Ev: events.StepChanged, Step: id, Msg: c.Action + " " + c.Target,
			Data: map[string]any{"action": c.Action, "target": c.Target, "detail": c.Detail}})
	}
	var v VerifyResult
	if err := x.call(ctx, n, "verify", &v); err != nil {
		return x.fail(id, err)
	}
	if !v.OK {
		return x.fail(id, errors.New("verify failed after apply"))
	}
	ev := x.evidence(ctx, n)
	x.emit(events.Event{Ev: events.StepDone, Step: id, Msg: fmt.Sprintf("%d change(s) applied", applied),
		Data: map[string]any{"changes": applied}})
	x.record(id, state.Done, applied, hash, "", ev)
	return outcome{status: state.Done, changes: applied}
}

func (x *Executor) evidence(ctx context.Context, n *plan.Node) []string {
	var e EvidenceResult
	if err := x.call(ctx, n, "evidence", &e); err != nil {
		x.emit(events.Event{Ev: events.Log, Level: "warning", Step: n.Step.ID, Msg: "evidence: " + err.Error()})
		return nil
	}
	var refs []string
	var items []any
	for _, it := range e.Items {
		refs = append(refs, it.Ref)
		items = append(items, map[string]any{"kind": it.Kind, "ref": it.Ref, "summary": it.Summary})
	}
	if len(items) > 0 {
		x.emit(events.Event{Ev: events.Log, Level: "evidence", Step: n.Step.ID, Msg: e.Items[0].Summary, Data: map[string]any{"evidence": items}})
	}
	return refs
}

// gate checks a gate before a step. It returns false with the outcome when
// the step must not run.
func (x *Executor) gate(ctx context.Context, n *plan.Node, g *plan.Gate) (outcome, bool) {
	check := func() (bool, error) {
		if g.Check == "" {
			r := x.State.GateRecord(g.ID)
			return r != nil && r.Status == state.Done, nil
		}
		cs := x.Catalog.Steps[g.Check]
		var v VerifyResult
		err := x.call(ctx, &plan.Node{Step: cs}, "verify", &v)
		return v.OK, err
	}
	ok, err := check()
	if err != nil {
		return x.fail(n.Step.ID, fmt.Errorf("gate %s check: %w", g.ID, err)), false
	}
	if ok {
		x.emit(events.Event{Ev: events.GatePassed, Step: n.Step.ID, Gate: g.ID, Msg: g.Title})
		return outcome{}, true
	}
	x.emit(events.Event{Ev: events.GateWaiting, Step: n.Step.ID, Gate: g.ID, Msg: g.Title,
		Data: map[string]any{"guide": g.Guide, "expected": g.Expected}})
	if !x.Interactive {
		x.record(n.Step.ID, state.Waiting, 0, "", "waiting on gate "+g.ID, nil)
		return outcome{status: state.Waiting}, false
	}
	t := time.NewTicker(x.GatePoll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			x.record(n.Step.ID, state.Waiting, 0, "", "waiting on gate "+g.ID, nil)
			return outcome{status: state.Waiting}, false
		case <-t.C:
			ok, err := check()
			if err != nil {
				return x.fail(n.Step.ID, fmt.Errorf("gate %s check: %w", g.ID, err)), false
			}
			if ok {
				x.emit(events.Event{Ev: events.GatePassed, Step: n.Step.ID, Gate: g.ID, Msg: g.Title})
				return outcome{}, true
			}
		}
	}
}

func truncate(s string) string {
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

func toAny(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

func statusAny(m map[string]int) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	return out
}
