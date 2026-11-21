package providers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Spec describes how to start one worker.
type Spec struct {
	Name       string   // provider name, for messages
	Pwsh       string   // pwsh executable; default "pwsh" on PATH
	Script     string   // path to engine/providers/runtime/BoxWorker.ps1
	Provider   string   // path to the provider module
	Modules    []string // Microsoft modules to import at start
	ModulePath string   // PSModulePath for the worker; empty keeps the default
	Env        []string // extra environment, never credentials
	OnRaw      func(line string)
	StartTime  time.Duration // start-up timeout; default 60s
}

// ErrClosed is returned by calls on a worker that has exited.
var ErrClosed = errors.New("worker has exited")

// Worker is a running provider process.
type Worker struct {
	spec  Spec
	cmd   *exec.Cmd
	stdin io.WriteCloser

	mu      sync.Mutex
	nextID  int
	pending map[string]chan Event
	dead    chan struct{}
	deadErr error
	writeMu sync.Mutex
}

// baseEnv is the minimum environment a worker gets: no credentials ever
// travel this way (SPEC §21).
func baseEnv(spec Spec) []string {
	keep := []string{"PATH", "HOME", "TMPDIR", "LANG", "USERPROFILE", "APPDATA", "LOCALAPPDATA",
		"SystemRoot", "HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "https_proxy", "http_proxy", "no_proxy",
		"SSL_CERT_FILE", "SSL_CERT_DIR"}
	var env []string
	for _, k := range keep {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	// BOX_PROVIDER_* carries non-secret provider settings.
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "BOX_PROVIDER_") {
			env = append(env, kv)
		}
	}
	env = append(env, "POWERSHELL_TELEMETRY_OPTOUT=1", "POWERSHELL_UPDATECHECK=Off", "PNPPOWERSHELL_UPDATECHECK=Off")
	if spec.ModulePath != "" {
		env = append(env, "PSModulePath="+spec.ModulePath)
	} else if v, ok := os.LookupEnv("PSModulePath"); ok {
		env = append(env, "PSModulePath="+v)
	}
	return append(env, spec.Env...)
}

// Start launches a worker and waits until it reports ready.
func Start(ctx context.Context, spec Spec) (*Worker, error) {
	pwsh := spec.Pwsh
	if pwsh == "" {
		pwsh = "pwsh"
	}
	// PowerShell reads a relative module path as a module name: pass absolute paths.
	script, err := filepath.Abs(spec.Script)
	if err != nil {
		return nil, err
	}
	provider, err := filepath.Abs(spec.Provider)
	if err != nil {
		return nil, err
	}
	args := []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-File", script, "-Provider", provider}
	if len(spec.Modules) > 0 {
		args = append(args, "-Modules", strings.Join(spec.Modules, ","))
	}
	cmd := exec.Command(pwsh, args...)
	cmd.Env = baseEnv(spec)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s worker: %w", spec.Name, err)
	}
	w := &Worker{spec: spec, cmd: cmd, stdin: stdin, pending: map[string]chan Event{}, dead: make(chan struct{})}
	ready := make(chan Event, 1)
	w.pending["start"] = ready
	go w.readRaw(stderr)
	go w.read(stdout)

	timeout := spec.StartTime
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	select {
	case ev := <-ready:
		if ev.Ev == "error" {
			_ = w.Kill()
			return nil, fmt.Errorf("%s worker failed to start: %w", spec.Name, ev.Error)
		}
		return w, nil
	case <-w.dead:
		return nil, fmt.Errorf("%s worker exited during start: %w", spec.Name, w.deadErr)
	case <-time.After(timeout):
		_ = w.Kill()
		return nil, fmt.Errorf("%s worker did not start within %s", spec.Name, timeout)
	case <-ctx.Done():
		_ = w.Kill()
		return nil, ctx.Err()
	}
}

// PID is the worker's process id.
func (w *Worker) PID() int { return w.cmd.Process.Pid }

func (w *Worker) read(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		i := strings.Index(line, Marker)
		if i < 0 {
			w.raw(line)
			continue
		}
		if i > 0 {
			w.raw(line[:i])
		}
		var ev Event
		if err := json.Unmarshal([]byte(line[i+len(Marker):]), &ev); err != nil {
			w.raw(line)
			continue
		}
		w.mu.Lock()
		ch := w.pending[ev.ID]
		w.mu.Unlock()
		if ch != nil {
			ch <- ev
		}
	}
	err := w.cmd.Wait()
	if err == nil {
		err = ErrClosed
	}
	w.mu.Lock()
	w.deadErr = err
	w.mu.Unlock()
	close(w.dead)
}

func (w *Worker) readRaw(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		w.raw(sc.Text())
	}
}

func (w *Worker) raw(line string) {
	if w.spec.OnRaw != nil && strings.TrimSpace(line) != "" {
		w.spec.OnRaw(line)
	}
}

// Call sends one request and waits for its result. Progress and log events
// are passed to onEvent as they arrive.
func (w *Worker) Call(ctx context.Context, op string, data any, onEvent func(Event)) (json.RawMessage, error) {
	w.mu.Lock()
	w.nextID++
	id := "r" + strconv.Itoa(w.nextID)
	ch := make(chan Event, 64)
	w.pending[id] = ch
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		delete(w.pending, id)
		w.mu.Unlock()
	}()

	b, err := json.Marshal(Request{ID: id, Op: op, Data: data})
	if err != nil {
		return nil, err
	}
	w.writeMu.Lock()
	_, err = w.stdin.Write(append(b, '\n'))
	w.writeMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("%s worker: %w", w.spec.Name, ErrClosed)
	}
	for {
		select {
		case ev := <-ch:
			switch ev.Ev {
			case "result":
				return ev.Data, nil
			case "error":
				return nil, ev.Error
			default:
				if onEvent != nil {
					onEvent(ev)
				}
			}
		case <-w.dead:
			return nil, fmt.Errorf("%s worker: %w", w.spec.Name, ErrClosed)
		case <-ctx.Done():
			// A step that does not answer leaves the worker in an unknown
			// state: stop it so the next call starts clean.
			_ = w.Kill()
			return nil, ctx.Err()
		}
	}
}

// Hello asks the worker for its versions and loaded modules.
func (w *Worker) Hello(ctx context.Context) (*Hello, error) {
	raw, err := w.Call(ctx, "hello", nil, nil)
	if err != nil {
		return nil, err
	}
	var h Hello
	return &h, json.Unmarshal(raw, &h)
}

// Auth hands credentials to the worker over stdin. They are never written
// to arguments, environment or disk.
func (w *Worker) Auth(ctx context.Context, creds map[string]any) error {
	_, err := w.Call(ctx, "auth", creds, nil)
	return err
}

// Invoke runs one step operation in the provider.
func (w *Worker) Invoke(ctx context.Context, inv Invoke, onEvent func(Event)) (json.RawMessage, error) {
	return w.Call(ctx, "invoke", inv, onEvent)
}

// Close asks the worker to exit and waits briefly, then kills it.
func (w *Worker) Close() error {
	select {
	case <-w.dead:
		return nil
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = w.Call(ctx, "shutdown", nil, nil)
	_ = w.stdin.Close()
	select {
	case <-w.dead:
		return nil
	case <-time.After(5 * time.Second):
		return w.Kill()
	}
}

// Kill stops the worker immediately.
func (w *Worker) Kill() error {
	if w.cmd.Process == nil {
		return nil
	}
	err := w.cmd.Process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}
