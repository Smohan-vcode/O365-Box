package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestSpikeS05 measures real Microsoft module workers. It runs only when
// BOX_MODULE_PATH points at a directory holding the pinned modules.
func TestSpikeS05(t *testing.T) {
	mp := os.Getenv("BOX_MODULE_PATH")
	if mp == "" {
		t.Skip("set BOX_MODULE_PATH to run spike S-05")
	}
	needPwsh(t)
	mods := []string{"Microsoft.Graph.Authentication", "ExchangeOnlineManagement", "PnP.PowerShell", "MicrosoftTeams"}
	start := func(m ...string) (*Worker, time.Duration, error) {
		t0 := time.Now()
		w, err := Start(context.Background(), Spec{Name: strings.Join(m, "+"), Script: workerScript,
			Provider: "testdata/spike/Box.Spike.psm1", Modules: m, ModulePath: mp})
		return w, time.Since(t0), err
	}

	for _, m := range mods {
		w, d, err := start(m)
		if err != nil {
			t.Fatalf("%s: %v", m, err)
		}
		t0 := time.Now()
		h, err := w.Hello(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		call := time.Since(t0)
		ver := ""
		for _, x := range h.Modules {
			if x.Name == m {
				ver = x.Version
			}
		}
		t.Logf("S05 start %-32s %s ready in %6.2fs, call %4dms", m, ver, d.Seconds(), call.Milliseconds())
		if d > 5*time.Second {
			t.Errorf("%s start-up %s exceeds 5s", m, d)
		}
		_ = w.Close()
	}

	t0 := time.Now()
	var wg sync.WaitGroup
	for _, m := range mods {
		wg.Add(1)
		go func(m string) {
			defer wg.Done()
			w, _, err := start(m)
			if err != nil {
				t.Error(err)
				return
			}
			_ = w.Close()
		}(m)
	}
	wg.Wait()
	t.Logf("S05 all four workers in parallel ready in %.2fs", time.Since(t0).Seconds())

	tok := fakeJWT()
	connect := func(w *Worker, m string) map[string]string {
		if err := w.Auth(context.Background(), map[string]any{"test": map[string]any{"access_token": tok}}); err != nil {
			t.Fatal(err)
		}
		c, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		raw, err := w.Invoke(c, Invoke{Step: m, Operation: "test"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		var r map[string]string
		_ = json.Unmarshal(raw, &r)
		return r
	}
	conn := []string{"Microsoft.Graph.Authentication", "PnP.PowerShell", "ExchangeOnlineManagement"}
	shared, _, err := start(conn...)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range conn {
		r := connect(shared, m)
		t.Logf("S05 shared process: %-32s %s %s", m, r["outcome"], trim(r["detail"]))
	}
	_ = shared.Close()
	for _, m := range conn {
		w, _, err := start(m)
		if err != nil {
			t.Fatal(err)
		}
		r := connect(w, m)
		t.Logf("S05 own process:    %-32s %s %s", m, r["outcome"], trim(r["detail"]))
		if r["outcome"] == "MissingMethodException" || r["outcome"] == "FileLoadException" {
			t.Errorf("%s conflicts even in its own process: %s", m, r["detail"])
		}
		_ = w.Close()
	}
}

func trim(s string) string {
	if len(s) > 110 {
		return s[:110] + "…"
	}
	return s
}

func fakeJWT() string {
	e := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	return e(map[string]string{"alg": "none", "typ": "JWT"}) + "." +
		e(map[string]any{"exp": 4102444800, "nbf": 1700000000, "aud": "https://graph.microsoft.com",
			"tid": "99999999-0000-4000-8000-000000000005", "scp": "User.Read"}) + ".x"
}
