package providers

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadOnlyWorker: a worker started for box audit runs test, verify and
// evidence only, and a verify that calls a module cmdlet able to write, by
// name, by a name built at run time, or one that appears only after
// connecting, or a REST call other than GET, fails without writing.
func TestReadOnlyWorker(t *testing.T) {
	needPwsh(t)
	mods, err := filepath.Abs("testdata/readonly/modules")
	if err != nil {
		t.Fatal(err)
	}
	p := &Pool{Script: workerScript, ModulePath: mods, Specs: map[string]PoolSpec{
		"ro": {Provider: "testdata/readonly/Box.ReadOnlyFake.psm1", Modules: []string{"FakeVendor"}, ReadOnly: true},
		"rw": {Provider: "testdata/readonly/Box.ReadOnlyFake.psm1", Modules: []string{"FakeVendor"}},
	}}
	t.Cleanup(p.Close)
	c := context.Background()

	raw, err := p.Invoke(c, "ro", "read", "verify", nil, nil)
	if err != nil {
		t.Fatalf("a read was refused: %v", err)
	}
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	if got["value"] != "read" || got["helper"] != "box-ro-test" {
		t.Errorf("read = %s", raw)
	}
	for step, want := range map[string]string{
		"write":   "Set-FakeThing was refused",
		"dynamic": "Remove-FakeThing was refused",
		"late":    "Set-LateCmdlet was refused",
		"rest":    "Invoke-RestMethod -Method Patch was refused",
	} {
		raw, err := p.Invoke(c, "ro", step, "verify", nil, nil)
		if err == nil || !strings.Contains(err.Error(), want) || strings.Contains(string(raw), "WROTE") {
			t.Errorf("%s: err = %v, result %s; want %q", step, err, raw, want)
		}
	}
	if raw, err := p.Invoke(c, "ro", "restget", "verify", nil, nil); err != nil || !strings.Contains(string(raw), "get attempted") {
		t.Errorf("a GET was refused: %v %s", err, raw)
	}
	for _, op := range []string{"plan", "apply"} {
		if _, err := p.Invoke(c, "ro", "read", op, nil, nil); err == nil || !strings.Contains(err.Error(), "only test, verify and evidence") {
			t.Errorf("%s: err = %v", op, err)
		}
	}
	// The shipped providers start in read-only mode (their Microsoft
	// modules are not needed to install the guard) and refuse apply.
	for _, prov := range []string{"exchange", "sharepoint", "teams", "identity"} {
		path, _ := filepath.Glob("../../providers/" + prov + "/Box." + prov + ".psm1")
		if len(path) != 1 {
			t.Fatalf("no module for %s", prov)
		}
		p.Specs[prov] = PoolSpec{Provider: path[0], ReadOnly: true}
		if _, err := p.Invoke(c, prov, "policy."+prov, "apply", nil, nil); err == nil || !strings.Contains(err.Error(), "only test, verify and evidence") {
			t.Errorf("%s apply: err = %v", prov, err)
		}
	}
	// The same provider without read-only mode is not restricted.
	if raw, err := p.Invoke(c, "rw", "write", "apply", nil, nil); err != nil || !strings.Contains(string(raw), "WROTE") {
		t.Errorf("normal worker: %v %s", err, raw)
	}
}
