// Package doctor checks the local prerequisites the providers run on:
// pwsh, the pinned Microsoft modules and the Power Platform CLI (T0-03).
package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Pins is expected-state/modules.yaml.
type Pins struct {
	Pwsh struct {
		Minimum string `yaml:"minimum"`
		Tested  string `yaml:"tested"`
	} `yaml:"pwsh"`
	Modules []struct {
		Name     string `yaml:"name"`
		Version  string `yaml:"version"`
		Provider string `yaml:"provider"`
	} `yaml:"modules"`
	Tools []struct {
		Name    string `yaml:"name"`
		Purpose string `yaml:"purpose"`
	} `yaml:"tools"`
}

// LoadPins reads the pinned versions.
func LoadPins(path string) (*Pins, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Pins
	if err := yaml.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &p, nil
}

// Item is one check.
type Item struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"` // runtime | module | tool
	Want     string `json:"want"`
	Found    string `json:"found,omitempty"`
	OK       bool   `json:"ok"`
	Required bool   `json:"required"`
	Fix      string `json:"fix,omitempty"`
}

// Options control where to look.
type Options struct {
	Pwsh       string // default "pwsh"
	ModulePath string // PSModulePath to search; empty uses pwsh's default
	Timeout    time.Duration
}

// Check runs every check.
func Check(ctx context.Context, pins *Pins, opt Options) []Item {
	if opt.Pwsh == "" {
		opt.Pwsh = "pwsh"
	}
	if opt.Timeout == 0 {
		opt.Timeout = 60 * time.Second
	}
	var items []Item
	pw := Item{Name: "pwsh", Kind: "runtime", Want: ">= " + pins.Pwsh.Minimum, Required: true,
		Fix: "install PowerShell 7.4 or later: https://learn.microsoft.com/powershell/scripting/install/installing-powershell"}
	path, err := exec.LookPath(opt.Pwsh)
	if err != nil {
		pw.Found = "not found"
		items = append(items, pw)
		for _, m := range pins.Modules {
			items = append(items, Item{Name: m.Name, Kind: "module", Want: m.Version, Found: "unknown (no pwsh)", Required: true, Fix: installCmd(m.Name, m.Version, opt.ModulePath)})
		}
		return append(items, tools(pins)...)
	}
	ver, err := pwshOut(ctx, path, opt, `$PSVersionTable.PSVersion.ToString()`)
	if err != nil {
		pw.Found = "error: " + err.Error()
	} else {
		pw.Found = strings.TrimSpace(ver)
		pw.OK = atLeast(pw.Found, pins.Pwsh.Minimum)
	}
	items = append(items, pw)

	listing, err := pwshOut(ctx, path, opt, `Get-Module -ListAvailable | ForEach-Object { [pscustomobject]@{ n = $_.Name; v = $_.Version.ToString() } } | ConvertTo-Json -Compress -AsArray`)
	found := map[string][]string{}
	if err == nil {
		var mods []struct{ N, V string }
		if json.Unmarshal([]byte(strings.TrimSpace(listing)), &mods) == nil {
			for _, m := range mods {
				found[strings.ToLower(m.N)] = append(found[strings.ToLower(m.N)], m.V)
			}
		}
	}
	for _, m := range pins.Modules {
		it := Item{Name: m.Name, Kind: "module", Want: m.Version, Required: true, Fix: installCmd(m.Name, m.Version, opt.ModulePath)}
		vs := found[strings.ToLower(m.Name)]
		switch {
		case err != nil:
			it.Found = "error listing modules: " + err.Error()
		case len(vs) == 0:
			it.Found = "not installed"
		default:
			it.Found = strings.Join(vs, ", ")
			for _, v := range vs {
				it.OK = it.OK || v == m.Version
			}
		}
		if it.OK {
			it.Fix = ""
		}
		items = append(items, it)
	}
	return append(items, tools(pins)...)
}

func tools(pins *Pins) []Item {
	var out []Item
	for _, t := range pins.Tools {
		it := Item{Name: t.Name, Kind: "tool", Want: t.Purpose, Required: false}
		if p, err := exec.LookPath(t.Name); err == nil {
			it.OK, it.Found = true, p
		} else {
			it.Found = "not found"
			if t.Name == "pac" {
				it.Fix = "dotnet tool install --global Microsoft.PowerApps.CLI.Tool"
			}
		}
		out = append(out, it)
	}
	return out
}

// OK reports whether every required item passed.
func OK(items []Item) bool {
	for _, i := range items {
		if i.Required && !i.OK {
			return false
		}
	}
	return true
}

func installCmd(name, version, modulePath string) string {
	if dir := firstDir(modulePath); dir != "" {
		return fmt.Sprintf("pwsh -NoProfile -Command \"Save-PSResource -Name %s -Version %s -Path '%s' -TrustRepository\"", name, version, dir)
	}
	return fmt.Sprintf("pwsh -NoProfile -Command \"Install-PSResource -Name %s -Version %s -Scope CurrentUser -TrustRepository\"", name, version)
}

func firstDir(modulePath string) string {
	if modulePath == "" {
		return ""
	}
	return strings.Split(modulePath, string(os.PathListSeparator))[0]
}

// Install installs every missing required module.
func Install(ctx context.Context, pins *Pins, opt Options, items []Item) error {
	if opt.Pwsh == "" {
		opt.Pwsh = "pwsh"
	}
	for _, it := range items {
		if it.Kind != "module" || it.OK {
			continue
		}
		dir := firstDir(opt.ModulePath)
		script := fmt.Sprintf("Install-PSResource -Name '%s' -Version '%s' -Scope CurrentUser -TrustRepository", it.Name, it.Want)
		if dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			script = fmt.Sprintf("Save-PSResource -Name '%s' -Version '%s' -Path '%s' -TrustRepository", it.Name, it.Want, dir)
		}
		o := opt
		o.Timeout = 10 * time.Minute
		if _, err := pwshOut(ctx, opt.Pwsh, o, script); err != nil {
			return fmt.Errorf("install %s %s: %w", it.Name, it.Want, err)
		}
	}
	return nil
}

func pwshOut(ctx context.Context, pwsh string, opt Options, script string) (string, error) {
	c, cancel := context.WithTimeout(ctx, opt.Timeout)
	defer cancel()
	cmd := exec.CommandContext(c, pwsh, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = os.Environ()
	if opt.ModulePath != "" {
		cmd.Env = append(cmd.Env, "PSModulePath="+opt.ModulePath)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", firstLine(msg))
	}
	return out.String(), nil
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return l
}

// atLeast compares dotted versions numerically.
func atLeast(have, min string) bool {
	h, m := parts(have), parts(min)
	for i := 0; i < 3; i++ {
		if h[i] != m[i] {
			return h[i] > m[i]
		}
	}
	return true
}

func parts(v string) [3]int {
	var out [3]int
	v, _, _ = strings.Cut(v, "-")
	for i, p := range strings.SplitN(v, ".", 3) {
		out[i], _ = strconv.Atoi(p)
	}
	return out
}
