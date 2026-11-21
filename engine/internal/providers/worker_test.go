package providers

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	workerScript = "../../providers/runtime/BoxWorker.ps1"
	fakeProvider = "testdata/fake/Box.Fake.psm1"
	secretToken  = "SECRET-token-0123456789abcdef"
)

func needPwsh(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("pwsh"); err != nil {
		t.Skip("pwsh not installed")
	}
}

func startFake(t *testing.T, raw *[]string) *Worker {
	t.Helper()
	needPwsh(t)
	var mu sync.Mutex
	w, err := Start(context.Background(), Spec{Name: "fake", Script: workerScript, Provider: fakeProvider,
		OnRaw: func(l string) {
			mu.Lock()
			defer mu.Unlock()
			if raw != nil {
				*raw = append(*raw, l)
			}
		}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestHelloAndEcho(t *testing.T) {
	w := startFake(t, nil)
	h, err := w.Hello(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	if h.Provider != "Box.Fake" || h.PID != w.PID() || !strings.HasPrefix(h.Pwsh, "7.") {
		t.Errorf("hello = %+v", h)
	}
	raw, err := w.Invoke(ctx(t), Invoke{Step: "echo", Operation: "plan", Input: map[string]any{"n": 2, "s": "x"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Step, Operation string
		Input           map[string]any
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Step != "echo" || got.Operation != "plan" || got.Input["s"] != "x" || got.Input["n"] != float64(2) {
		t.Errorf("echo = %+v", got)
	}
}

func TestProgressAndLogs(t *testing.T) {
	var raw []string
	w := startFake(t, &raw)
	var evs []Event
	if _, err := w.Invoke(ctx(t), Invoke{Step: "progress", Operation: "apply"}, func(e Event) { evs = append(evs, e) }); err != nil {
		t.Fatal(err)
	}
	if len(evs) != 3 || evs[2].Done != 3 || evs[2].Of != 3 || evs[0].Msg != "item 1" {
		t.Errorf("progress = %+v", evs)
	}
	evs = nil
	res, err := w.Invoke(ctx(t), Invoke{Step: "logs", Operation: "apply"}, func(e Event) { evs = append(evs, e) })
	if err != nil {
		t.Fatal(err)
	}
	if string(res) != `"ok"` {
		t.Errorf("result %s", res)
	}
	levels := map[string]string{}
	for _, e := range evs {
		levels[e.Msg] = e.Level
	}
	for msg, lvl := range map[string]string{"host line": "info", "careful": "warning", "hidden": "verbose", "info line": "info"} {
		if levels[msg] != lvl {
			t.Errorf("log %q level %q, want %q (all: %v)", msg, levels[msg], lvl, levels)
		}
	}
	if len(raw) != 0 {
		t.Errorf("records leaked to raw output: %v", raw)
	}
}

func TestStepErrorAndStrayConsole(t *testing.T) {
	var raw []string
	w := startFake(t, &raw)
	_, err := w.Invoke(ctx(t), Invoke{Step: "fail", Operation: "apply"}, nil)
	var we *WorkerError
	if !errors.As(err, &we) || !strings.Contains(we.Message, "step failed on purpose") {
		t.Errorf("err = %v", err)
	}
	res, err := w.Invoke(ctx(t), Invoke{Step: "console", Operation: "plan"}, nil)
	if err != nil || string(res) != `"after console"` {
		t.Errorf("console step: %s %v", res, err)
	}
	time.Sleep(100 * time.Millisecond)
	if strings.Join(raw, "\n") != "stray console output" {
		t.Errorf("raw = %q", raw)
	}
	if _, err := w.Hello(ctx(t)); err != nil {
		t.Errorf("worker unusable after errors: %v", err)
	}
}

func TestCredentialsStayInMemoryAndRedacted(t *testing.T) {
	tmp := t.TempDir()
	needPwsh(t)
	var raw []string
	var mu sync.Mutex
	w, err := Start(context.Background(), Spec{Name: "fake", Script: workerScript, Provider: fakeProvider,
		Env:   []string{"TMPDIR=" + tmp, "HOME=" + tmp},
		OnRaw: func(l string) { mu.Lock(); raw = append(raw, l); mu.Unlock() }})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.Auth(ctx(t), map[string]any{"graph": map[string]any{"access_token": secretToken}}); err != nil {
		t.Fatal(err)
	}
	res, err := w.Invoke(ctx(t), Invoke{Step: "auth", Operation: "test"}, nil)
	if err != nil || !strings.Contains(string(res), `"has":true`) {
		t.Fatalf("provider cannot read credential: %s %v", res, err)
	}
	var logs []string
	_, err = w.Invoke(ctx(t), Invoke{Step: "leak", Operation: "apply"}, func(e Event) { logs = append(logs, e.Msg) })
	if err == nil {
		t.Fatal("want error from leak step")
	}
	all := err.Error() + strings.Join(logs, "\n") + strings.Join(raw, "\n")
	if strings.Contains(all, secretToken) {
		t.Errorf("secret visible in output: %s", all)
	}
	if !strings.Contains(all, "[redacted]") {
		t.Errorf("want redaction marker in %s", all)
	}
	if runtime.GOOS == "linux" {
		for _, f := range []string{"cmdline", "environ"} {
			b, err := os.ReadFile(filepath.Join("/proc", itoa(w.PID()), f))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(b), secretToken) {
				t.Errorf("secret in /proc/%d/%s", w.PID(), f)
			}
		}
	}
	_ = filepath.Walk(tmp, func(p string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			if b, _ := os.ReadFile(p); strings.Contains(string(b), secretToken) {
				t.Errorf("secret written to %s", p)
			}
		}
		return nil
	})
}

func itoa(n int) string { return intToString(n) }

func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestTimeoutKillsWorker(t *testing.T) {
	w := startFake(t, nil)
	c, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := w.Invoke(c, Invoke{Step: "slow", Operation: "apply"}, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	select {
	case <-w.dead:
	case <-time.After(10 * time.Second):
		t.Fatal("worker still running after timeout")
	}
	if _, err := w.Hello(ctx(t)); !errors.Is(err, ErrClosed) {
		t.Errorf("call after kill: %v", err)
	}
}

func TestStartFailures(t *testing.T) {
	needPwsh(t)
	_, err := Start(context.Background(), Spec{Name: "missing", Script: workerScript, Provider: "testdata/none.psm1"})
	if err == nil || !strings.Contains(err.Error(), "failed to start") {
		t.Errorf("missing provider: %v", err)
	}
	_, err = Start(context.Background(), Spec{Name: "nomod", Script: workerScript, Provider: fakeProvider, Modules: []string{"No.Such.Module"}})
	if err == nil || !strings.Contains(err.Error(), "No.Such.Module") {
		t.Errorf("missing module: %v", err)
	}
}

func TestParallelWorkers(t *testing.T) {
	needPwsh(t)
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w, err := Start(context.Background(), Spec{Name: "fake", Script: workerScript, Provider: fakeProvider})
			if err != nil {
				errs <- err
				return
			}
			defer w.Close()
			if _, err := w.Invoke(context.Background(), Invoke{Step: "progress", Operation: "apply"}, nil); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
