package doctor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAtLeast(t *testing.T) {
	for _, c := range []struct {
		have, min string
		want      bool
	}{{"7.4.6", "7.4.0", true}, {"7.4.0", "7.4.0", true}, {"7.3.12", "7.4.0", false}, {"8.0", "7.4.0", true}, {"7.5.0-preview.1", "7.4.0", true}} {
		if got := atLeast(c.have, c.min); got != c.want {
			t.Errorf("atLeast(%s, %s) = %v", c.have, c.min, got)
		}
	}
}

func TestMissingAndWrongVersions(t *testing.T) {
	if _, err := exec.LookPath("pwsh"); err != nil {
		t.Skip("pwsh not installed")
	}
	pins, err := LoadPins("../../../expected-state/modules.yaml")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	// A module present at the wrong version.
	md := filepath.Join(dir, "PnP.PowerShell", "1.0.0")
	_ = os.MkdirAll(md, 0o755)
	_ = os.WriteFile(filepath.Join(md, "PnP.PowerShell.psd1"), []byte("@{ ModuleVersion = '1.0.0'; RootModule = '' }"), 0o644)
	items := Check(context.Background(), pins, Options{ModulePath: dir})
	if OK(items) {
		t.Fatal("want not OK")
	}
	byName := map[string]Item{}
	for _, i := range items {
		byName[i.Name] = i
	}
	if i := byName["PnP.PowerShell"]; i.OK || i.Found != "1.0.0" || i.Want != "2.12.0" || !strings.Contains(i.Fix, "-Version 2.12.0") {
		t.Errorf("wrong version not reported: %+v", i)
	}
	if i := byName["MicrosoftTeams"]; i.OK || i.Found != "not installed" || !strings.Contains(i.Fix, dir) {
		t.Errorf("missing module not reported: %+v", i)
	}
	if i := byName["pwsh"]; !i.OK {
		t.Errorf("pwsh: %+v", i)
	}
	if i := byName["pac"]; i.Required {
		t.Error("pac must be optional until T5")
	}
}

func TestNoPwsh(t *testing.T) {
	pins, _ := LoadPins("../../../expected-state/modules.yaml")
	items := Check(context.Background(), pins, Options{Pwsh: "no-such-pwsh-binary"})
	if OK(items) || items[0].Found != "not found" || !strings.Contains(items[0].Fix, "install PowerShell") {
		t.Errorf("%+v", items[0])
	}
}
