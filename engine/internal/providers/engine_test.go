package providers

import (
	"context"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/Smohan-vcode/O365-Box/engine/internal/plan"
)

const engineProviders = "../../providers"

// TestEngineProvidersMatchManifests starts every real provider (without its
// Microsoft module unless BOX_MODULE_PATH is set) and checks that the step
// ids its module implements equal those its steps.yaml declares.
func TestEngineProvidersMatchManifests(t *testing.T) {
	needPwsh(t)
	cat, err := plan.Load(engineProviders, "../../gates.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"dns", "exchange", "identity", "intune", "powerplatform", "sharepoint", "teams"}
	var got []string
	for name := range cat.Manifests {
		got = append(got, name)
	}
	sort.Strings(got)
	if len(got) != len(want) {
		t.Fatalf("providers = %v, want %v", got, want)
	}
	mp := os.Getenv("BOX_MODULE_PATH")
	for _, name := range got {
		m := cat.Manifests[name]
		mods := m.Modules
		if mp == "" {
			mods = nil
		}
		w, err := Start(context.Background(), Spec{Name: name, Script: workerScript, Provider: m.ModuleFile(), Modules: mods, ModulePath: mp})
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		impl, err := w.Steps(context.Background())
		_ = w.Close()
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		declared := map[string]bool{}
		for _, s := range m.Steps {
			declared[s.ID] = true
		}
		have := map[string]bool{}
		for _, id := range impl {
			have[id] = true
			if !declared[id] {
				t.Errorf("%s implements %s, which steps.yaml does not declare", name, id)
			}
		}
		for id := range declared {
			if !have[id] {
				t.Errorf("%s declares %s but its module has no handler", name, id)
			}
		}
	}
}

// TestProvidersInParallelWithRealModules is T0-03's exit criterion: the
// providers run in parallel, each with its Microsoft module, without
// conflicts. Runs when BOX_MODULE_PATH is set.
func TestProvidersInParallelWithRealModules(t *testing.T) {
	mp := os.Getenv("BOX_MODULE_PATH")
	if mp == "" {
		t.Skip("set BOX_MODULE_PATH to run with the real modules")
	}
	needPwsh(t)
	cat, err := plan.Load(engineProviders, "")
	if err != nil {
		t.Fatal(err)
	}
	specs := map[string]PoolSpec{}
	for name, m := range cat.Manifests {
		specs[name] = PoolSpec{Provider: m.ModuleFile(), Modules: m.Modules}
	}
	pool := &Pool{Script: workerScript, ModulePath: mp, Specs: specs}
	defer pool.Close()
	t0 := time.Now()
	var wg sync.WaitGroup
	for name, m := range cat.Manifests {
		wg.Add(1)
		go func(name string, m *plan.Manifest) {
			defer wg.Done()
			w, err := pool.worker(context.Background(), name)
			if err != nil {
				t.Errorf("%s: %v", name, err)
				return
			}
			h, err := w.Hello(context.Background())
			if err != nil {
				t.Errorf("%s: %v", name, err)
				return
			}
			for _, want := range m.Modules {
				found := false
				for _, x := range h.Modules {
					found = found || x.Name == want
				}
				if !found {
					t.Errorf("%s: module %s not loaded (%+v)", name, want, h.Modules)
				}
			}
		}(name, m)
	}
	wg.Wait()
	t.Logf("%d providers ready in parallel in %.2fs", len(cat.Manifests), time.Since(t0).Seconds())
}
