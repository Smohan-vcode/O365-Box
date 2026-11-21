package providers

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestPester runs the provider Pester suite (engine/providers/*.Tests.ps1)
// when Pester 5 is available on BOX_MODULE_PATH or the default module path.
func TestPester(t *testing.T) {
	needPwsh(t)
	script := `$ErrorActionPreference = 'Stop'
if (-not (Get-Module -ListAvailable Pester | Where-Object { $_.Version.Major -ge 5 })) { 'NO-PESTER'; exit 0 }
Import-Module Pester -MinimumVersion 5.0
$r = Invoke-Pester -Path ../../providers -PassThru -Output None
"passed=$($r.PassedCount) failed=$($r.FailedCount)"
$r.Failed | ForEach-Object { 'FAILED ' + $_.ExpandedPath + ': ' + $_.ErrorRecord }
exit $r.FailedCount`
	cmd := exec.Command("pwsh", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = os.Environ()
	if mp := os.Getenv("BOX_MODULE_PATH"); mp != "" {
		cmd.Env = append(cmd.Env, "PSModulePath="+mp)
	}
	out, err := cmd.CombinedOutput()
	s := strings.TrimSpace(string(out))
	if strings.Contains(s, "NO-PESTER") {
		t.Skip("Pester 5 not installed")
	}
	if err != nil {
		t.Fatalf("Pester failed:\n%s", s)
	}
	t.Log(s)
}
