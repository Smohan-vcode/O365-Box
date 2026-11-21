package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// PoolSpec describes how to start the worker for one provider.
type PoolSpec struct {
	Provider string   // path to the provider module
	Modules  []string // Microsoft module to import (at most one, per S-05)
	// ReadOnly starts the worker in read-only mode (box audit, SPEC §26):
	// it runs only test, verify and evidence, and refuses every module
	// cmdlet whose verb could change the tenant and every Graph call that
	// is not a GET (BoxWorker.ps1).
	ReadOnly bool
}

// ReadOnlyEnv is the environment variable that puts a worker in read-only
// mode.
const ReadOnlyEnv = "BOX_READ_ONLY=1"

// Pool starts one worker per provider on first use, reuses it, and
// restarts it after it exits. It implements run.Invoker.
type Pool struct {
	Script     string // engine/providers/runtime/BoxWorker.ps1
	Pwsh       string
	ModulePath string
	Specs      map[string]PoolSpec
	Creds      map[string]map[string]any // credentials per provider, sent after start
	// CredsFunc, when set, supplies credentials per provider; it is called
	// before every call and the worker is re-authenticated when they change
	// (tokens are refreshed before they expire).
	CredsFunc func(ctx context.Context, provider string) (map[string]any, error)
	OnRaw     func(provider, line string)
	OnLog     func(provider, step, level, msg string)

	mu      sync.Mutex
	workers map[string]*Worker
	locks   map[string]*sync.Mutex
	sent    map[*Worker]string
}

// authenticate sends credentials when they differ from those last sent.
func (p *Pool) authenticate(ctx context.Context, provider string, w *Worker) error {
	var creds map[string]any
	if p.CredsFunc != nil {
		c, err := p.CredsFunc(ctx, provider)
		if err != nil {
			return fmt.Errorf("credentials for %s: %w", provider, err)
		}
		creds = c
	} else {
		creds = p.Creds[provider]
	}
	if len(creds) == 0 {
		return nil
	}
	b, _ := json.Marshal(creds)
	p.mu.Lock()
	if p.sent == nil {
		p.sent = map[*Worker]string{}
	}
	same := p.sent[w] == string(b)
	p.mu.Unlock()
	if same {
		return nil
	}
	if err := w.Auth(ctx, creds); err != nil {
		return err
	}
	p.mu.Lock()
	p.sent[w] = string(b)
	p.mu.Unlock()
	return nil
}

func (p *Pool) lock(provider string) *sync.Mutex {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.locks == nil {
		p.locks = map[string]*sync.Mutex{}
	}
	l := p.locks[provider]
	if l == nil {
		l = &sync.Mutex{}
		p.locks[provider] = l
	}
	return l
}

func (p *Pool) worker(ctx context.Context, provider string) (*Worker, error) {
	p.mu.Lock()
	if p.workers == nil {
		p.workers = map[string]*Worker{}
	}
	w := p.workers[provider]
	p.mu.Unlock()
	if w != nil {
		select {
		case <-w.dead:
		default:
			return w, nil
		}
	}
	spec, ok := p.Specs[provider]
	if !ok {
		return nil, fmt.Errorf("no provider %q", provider)
	}
	var env []string
	if spec.ReadOnly {
		env = append(env, ReadOnlyEnv)
	}
	w, err := Start(ctx, Spec{Name: provider, Pwsh: p.Pwsh, Script: p.Script, Provider: spec.Provider,
		Modules: spec.Modules, ModulePath: p.ModulePath, Env: env,
		OnRaw: func(l string) {
			if p.OnRaw != nil {
				p.OnRaw(provider, l)
			}
		}})
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.workers[provider] = w
	p.mu.Unlock()
	return w, nil
}

// Invoke runs one step operation. Calls to the same provider are serialised:
// a worker handles one request at a time.
func (p *Pool) Invoke(ctx context.Context, provider, step, operation string, input any, progress func(string, int, int)) (json.RawMessage, error) {
	l := p.lock(provider)
	l.Lock()
	defer l.Unlock()
	w, err := p.worker(ctx, provider)
	if err != nil {
		return nil, err
	}
	if err := p.authenticate(ctx, provider, w); err != nil {
		return nil, err
	}
	raw, err := w.Invoke(ctx, Invoke{Step: step, Operation: operation, Input: input}, func(e Event) {
		switch e.Ev {
		case "progress":
			if progress != nil {
				progress(e.Msg, e.Done, e.Of)
			}
		case "log":
			if p.OnLog != nil {
				p.OnLog(provider, step, e.Level, e.Msg)
			}
		}
	})
	if errors.Is(err, ErrClosed) {
		return nil, fmt.Errorf("%s worker exited during %s %s", provider, step, operation)
	}
	return raw, err
}

// Close stops every worker.
func (p *Pool) Close() {
	p.mu.Lock()
	ws := p.workers
	p.workers = nil
	p.mu.Unlock()
	for _, w := range ws {
		_ = w.Close()
	}
}
