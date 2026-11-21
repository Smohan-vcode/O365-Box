package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/Smohan-vcode/O365-Box/engine/internal/audit"
	"github.com/Smohan-vcode/O365-Box/engine/internal/auth"
	"github.com/Smohan-vcode/O365-Box/engine/internal/events"
	"github.com/Smohan-vcode/O365-Box/engine/internal/graph"
	"github.com/Smohan-vcode/O365-Box/engine/internal/plan"
	"github.com/Smohan-vcode/O365-Box/engine/internal/policy"
	"github.com/Smohan-vcode/O365-Box/engine/internal/providers"
	"github.com/Smohan-vcode/O365-Box/engine/internal/run"
)

// auditFlags are box audit's options.
type auditFlags struct {
	framework  string
	period     string
	out        string
	tenant     string
	cap        int
	noSettings bool
	modulePath string
	timeout    time.Duration
}

// auditEnv is what the audit reaches the outside world through; tests
// replace it with recorded responses.
type auditEnv struct {
	signIn   func(ctx context.Context, tenant string, scopes []string, out io.Writer) (*auth.Token, error)
	http     *http.Client
	v1, beta string
	dns      audit.Resolver
	steps    func(ctx context.Context, root string, f *auditFlags, out io.Writer, bySteps map[string][]policy.StepSetting, tenant map[string]any, token string) map[string]audit.StepOutcome
	now      func() time.Time
}

func defaultAuditEnv() *auditEnv {
	return &auditEnv{
		signIn: func(ctx context.Context, tenant string, scopes []string, out io.Writer) (*auth.Token, error) {
			return auth.SignInDeviceCode(ctx, nil, tenant, auth.GraphCLIClientID, auth.GraphScopes(scopes), func(dc auth.DeviceCode) {
				fmt.Fprintf(out, "SIGN-IN (read only): open %s and enter the code %s (expires in %d minutes).\n  Sign in as a person with Global Reader (plus Security Reader and a Purview reader role).\n",
					dc.VerificationURI, dc.UserCode, dc.ExpiresIn/60)
			})
		},
		dns:   net.DefaultResolver,
		steps: runAuditSteps,
		now:   time.Now,
	}
}

const auditLong = `Prepare for an audit (SPEC §26): read a Microsoft 365 tenant and write an
audit pack for one framework, changing nothing.

Read only, by construction:
  - a delegated device-code sign-in with the Microsoft Graph Command Line
    Tools client and the read-only scopes in expected-state/audit-scopes.yaml
    (no app registration, certificate or tenant write); sign in as a person
    holding Global Reader, plus Security Reader and a Purview reader role;
  - the audit's own Graph calls are GET only;
  - the policy steps run in audit mode: test, verify and evidence only;
    plan and apply are refused before they reach a provider;
  - the token is held in memory and dropped when the pack is written.

The first sign-in may ask for consent to the read-only scopes for the
Microsoft Graph Command Line Tools: a Global Reader cannot consent, so an
administrator approves them once (or through the admin consent workflow).

What it reads: every catalogue setting through its mapping's verify (the
Entra ID and Intune policy steps, over Graph); inventories (users and admins
with MFA registration and last sign-in, role assignments, guests,
Conditional Access, authentication methods, devices and compliance, sharing,
labels, retention, alerts, licences, app registrations, SPF and DMARC); and
activity over the period (directory audit, sign-ins, alerts), as far back as
the tenant keeps it, capped at --cap records each. Settings and inventories
held only in Exchange Online, Security & Compliance, SharePoint (PnP), Teams
or Power Platform PowerShell are listed as "not read" with what reading
them needs; no value is assumed.

The pack goes to --out, a new or empty folder outside this repository:
  index.html       one row per control: status, requirements, gap, evidence
  controls.csv     the same, for a spreadsheet
  evidence/<control>/*.json|csv   raw reads stamped with tenant, time, query
  gaps.md          gaps ordered by risk, each with the fix the box proposes
  manifest.sha256  the hash of every file (box audit verify <folder>)

Control statuses: met, partly met, not met, document needed (a written
procedure), not read (no evidence could be read with this sign-in).`

func newAuditCmd(g *globals) *cobra.Command {
	f := &auditFlags{}
	env := defaultAuditEnv()
	c := &cobra.Command{
		Use:   "audit --framework <id> --out <folder>",
		Short: "Prepare for an audit: read the tenant (never write) and write an audit pack for a framework",
		Long:  auditLong,
		Example: "  box audit --framework soc2 --out ~/audit-2026\n" +
			"  box audit --framework iso27001_2022 --period 2026-01-01..2026-11-21 --tenant <tenant>.onmicrosoft.com --out /tmp/pack\n" +
			"  box audit verify ~/audit-2026",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAudit(cmd.Context(), g, f, env, cmd.OutOrStdout())
		},
	}
	c.Flags().StringVar(&f.framework, "framework", "", "framework id: "+frameworkIDsHelp())
	c.Flags().StringVar(&f.period, "period", "", "audit period FROM..TO, e.g. 2026-01-01..2026-11-21 (default: the last twelve months)")
	c.Flags().StringVar(&f.out, "out", "", "new or empty folder for the pack, outside the repository")
	c.Flags().StringVar(&f.tenant, "tenant", "organizations", "tenant id or domain to sign in to")
	c.Flags().IntVar(&f.cap, "cap", 5000, "most records read per activity log (sign-ins, directory audit, alerts)")
	c.Flags().BoolVar(&f.noSettings, "no-settings", false, "skip the catalogue settings (policy step verify); inventories only")
	c.Flags().StringVar(&f.modulePath, "module-path", "", "PSModulePath for provider workers")
	c.Flags().DurationVar(&f.timeout, "step-timeout", 10*time.Minute, "timeout per policy step read")
	c.AddCommand(&cobra.Command{Use: "verify <folder>", Short: "Check an audit pack's files against its manifest.sha256", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			problems, err := audit.VerifyManifest(args[0])
			if err != nil {
				return err
			}
			for _, p := range problems {
				fmt.Fprintln(cmd.OutOrStdout(), "  ✗ "+p)
			}
			if len(problems) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "✗ audit pack: %d problem(s)\n", len(problems))
				return exitError{ExitFailure}
			}
			fmt.Fprintln(cmd.OutOrStdout(), "✓ audit pack: every file matches manifest.sha256")
			return nil
		}})
	return c
}

func frameworkIDsHelp() string {
	return "soc2, iso27001_2022, nist_csf_2, nis2, cis_v8 (policy/frameworks)"
}

func runAudit(ctx context.Context, g *globals, f *auditFlags, env *auditEnv, out io.Writer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	root, err := g.root()
	if err != nil {
		return err
	}
	if f.framework == "" {
		return usageError("--framework is required: " + frameworkIDsHelp())
	}
	dir, err := audit.CheckOutDir(root, f.out)
	if err != nil {
		return usageError(err.Error())
	}
	now := env.now
	if now == nil {
		now = time.Now
	}
	period, err := audit.ParsePeriod(f.period, now())
	if err != nil {
		return usageError(err.Error())
	}
	cat, issues, err := loadCatalogue(root)
	if err != nil {
		return err
	}
	if len(issues) > 0 {
		return fmt.Errorf("policy catalogue has %d problem(s); run box policy validate", len(issues))
	}
	fw := cat.Framework(f.framework)
	if fw == nil {
		ids, _, _ := cat.IDs()
		return usageError(fmt.Sprintf("no framework %q: choose %s", f.framework, strings.Join(ids, ", ")))
	}
	maps, _, err := policy.LoadMappings(filepath.Join(root, "expected-state", "setting-ids"))
	if err != nil {
		return err
	}
	scopes, err := audit.LoadScopes(filepath.Join(root, filepath.FromSlash(audit.ScopesFile)))
	if err != nil {
		return err
	}

	tok, err := env.signIn(ctx, f.tenant, scopes.Delegated, out)
	if err != nil {
		return fmt.Errorf("sign-in: %w", err)
	}
	// R12: the token lives in this function only and is dropped on return.
	defer func() { tok.Access = "" }()
	sess := audit.NewSession(tok.TenantID(), tok.Subject(), tok.Scopes(), tok.Claims)
	fmt.Fprintf(out, "✓ signed in (read only) to tenant %s\n", sess.Tenant)
	var notes []string
	if !sess.GlobalReader {
		notes = append(notes, "The person who signed in does not hold Global Reader; reads it may not make are listed as not read.")
	}
	if len(sess.WriteScopes) > 0 {
		notes = append(notes, "The sign-in token also carried scopes consented earlier for the Graph client that could write ("+
			strings.Join(sess.WriteScopes, ", ")+"). The audit sent GET requests only and refused plan and apply.")
	}

	hc := audit.ReadOnlyClient(env.http)
	gc := &graph.Client{HTTP: hc, Token: func() string { return tok.Access }}
	rd := &audit.Reader{Graph: gc, V1: env.v1, Beta: env.beta, DNS: env.dns, Period: period, Cap: f.cap, Now: now,
		Progress: func(id, title string) { fmt.Fprintf(out, "  … %s\n", title) }}
	fmt.Fprintf(out, "Reading inventories for %s…\n", period)
	invs := rd.ReadAll(ctx)

	bySteps := audit.AuditSelection(cat, maps)
	outcomes := map[string]audit.StepOutcome{}
	allowed, why := sess.ProvidersAllowed()
	switch {
	case f.noSettings:
		why = "skipped with --no-settings"
		fallthrough
	case !allowed:
		for st, ss := range bySteps {
			prov := ""
			if len(ss) > 0 {
				prov = maps[ss[0].ID].Provider
			}
			if audit.GraphProviders[prov] {
				outcomes[st] = audit.StepOutcome{Step: st, Provider: prov, Reason: why}
			}
		}
		notes = append(notes, "Catalogue settings read by the Entra ID and Intune policy steps were not read: "+why+".")
	default:
		name, _, _ := strings.Cut(strings.ToLower(rd.InitialDomain()), ".")
		tenant := map[string]any{"id": sess.Tenant, "initial_domain": rd.InitialDomain()}
		if name != "" {
			tenant["sharepoint"] = "https://" + name + ".sharepoint.com"
			tenant["sharepoint_admin"] = "https://" + name + "-admin.sharepoint.com"
		}
		fmt.Fprintln(out, "Reading catalogue settings through the policy steps (audit mode: test, verify, evidence)…")
		outcomes = env.steps(ctx, root, f, out, bySteps, tenant, tok.Access)
	}
	settings := audit.ReadSettings(cat, maps, bySteps, outcomes)
	notes = append(notes, notReadNote(settings)...)

	a := audit.Assess(cat, fw, settings, invs)
	pack := &audit.Pack{Tenant: sess.Tenant, Organisation: rd.OrganisationName(), Subject: sess.Subject, ReadBy: "box audit " + Version,
		Period: period, ReadAt: now().UTC(), Framework: fw, Assessment: a, Inventories: invs, Settings: settings, Steps: outcomes, Notes: notes}
	tok.Access, gc = "", nil
	files, err := audit.Write(dir, pack)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "✓ sign-in token dropped (never written)")
	if g.asJSON {
		return writeJSON(out, map[string]any{"command": "audit", "framework": fw.ID, "tenant": sess.Tenant, "period": period.String(),
			"out": dir, "files": len(files), "controls": a.Counts})
	}
	var parts []string
	for _, s := range []string{audit.Met, audit.PartlyMet, audit.NotMet, audit.DocumentNeeded, audit.NotRead} {
		if a.Counts[s] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", a.Counts[s], s))
		}
	}
	fmt.Fprintf(out, "✓ audit pack for %s: %d control(s): %s\n", fw.Title, len(a.Controls), strings.Join(parts, ", "))
	fmt.Fprintf(out, "  %d file(s) in %s; open %s\n", len(files), dir, filepath.Join(dir, "index.html"))
	for _, n := range notes {
		fmt.Fprintln(out, "  note: "+n)
	}
	return nil
}

// notReadNote summarises the settings not read, by reason.
func notReadNote(settings map[string]*audit.SettingRead) []string {
	by, addon := map[string]int{}, 0
	for _, s := range settings {
		switch {
		case s.Status != audit.NotRead:
		case strings.HasPrefix(s.Detail, "needs a paid add-on"):
			addon++
		case strings.HasPrefix(s.Detail, "needs "):
			by[s.Provider]++
		}
	}
	var out []string
	if addon > 0 {
		out = append(out, fmt.Sprintf("%d add-on setting(s) are outside the subscription and have no read (not read).", addon))
	}
	for _, p := range sortedKeys(by) {
		out = append(out, fmt.Sprintf("%d %s setting(s) not read: needs %s.", by[p], p, audit.NeedsFor(p, "")))
	}
	return out
}

// runAuditSteps runs the Entra ID and Intune policy steps in audit mode
// with the delegated token and returns each step's test and verify result.
func runAuditSteps(ctx context.Context, root string, f *auditFlags, out io.Writer, bySteps map[string][]policy.StepSetting, tenant map[string]any, token string) map[string]audit.StepOutcome {
	outcomes := map[string]audit.StepOutcome{}
	cat, err := plan.Load(filepath.Join(root, "engine", "providers"), filepath.Join(root, "engine", "gates.yaml"))
	reason := ""
	if err != nil {
		reason = "provider manifests: " + err.Error()
	} else if _, err := exec.LookPath("pwsh"); err != nil {
		reason = "PowerShell 7 (pwsh) is not installed; the policy steps run in it"
	}
	defaults, err := engineDefaults(root)
	if err != nil && reason == "" {
		reason = "engine defaults: " + err.Error()
	}
	g := &plan.Graph{Nodes: map[string]*plan.Node{}}
	for id := range bySteps {
		var st *plan.Step
		if cat != nil {
			st = cat.Steps[id]
		}
		switch {
		case st == nil:
			// Not built yet (T14): ReadSettings reports what reading it needs.
			continue
		case !audit.GraphProviders[st.Provider]:
			outcomes[id] = audit.StepOutcome{Step: id, Provider: st.Provider, Reason: "needs " + audit.NeedsFor(st.Provider, id)}
		case reason != "":
			outcomes[id] = audit.StepOutcome{Step: id, Provider: st.Provider, Reason: reason}
		default:
			g.Nodes[id] = &plan.Node{Step: st}
			g.Order = append(g.Order, id)
		}
	}
	if len(g.Order) == 0 {
		return outcomes
	}
	sort.Strings(g.Order)
	red := events.NewRedactor(events.SinkFunc(func(e events.Event) {
		switch e.Ev {
		case events.StepDone, events.StepChanged:
			fmt.Fprintf(out, "  ✓ %s read\n", e.Step)
		case events.StepFailed:
			fmt.Fprintf(out, "  ✗ %s not read: %s\n", e.Step, e.Msg)
		case events.StepSkipped:
			fmt.Fprintf(out, "  - %s: %s\n", e.Step, e.Msg)
		}
	}), nil)
	red.AddSecret(token)
	specs := map[string]providers.PoolSpec{}
	for name, man := range cat.Manifests {
		if audit.GraphProviders[name] {
			specs[name] = providers.PoolSpec{Provider: man.ModuleFile(), Modules: man.Modules}
		}
	}
	pool := &providers.Pool{
		Script:     filepath.Join(root, "engine", "providers", "runtime", "BoxWorker.ps1"),
		ModulePath: f.modulePath,
		Specs:      specs,
		CredsFunc: func(context.Context, string) (map[string]any, error) {
			t := map[string]any{"access_token": token}
			return map[string]any{"graph": t, "app": t}, nil
		},
	}
	defer pool.Close()
	var mu sync.Mutex
	// Steps report settings they left out as warnings; the audit keeps them
	// so such a setting is "not read", never "pass".
	warnings := map[string][]string{}
	pool.OnLog = func(_, step, level, msg string) {
		if level == "warning" {
			mu.Lock()
			warnings[step] = append(warnings[step], red.Text(msg))
			mu.Unlock()
		}
	}
	inv := &run.ReadOnlyInvoker{Next: pool, Observe: func(step, op string, raw json.RawMessage, err error) {
		mu.Lock()
		defer mu.Unlock()
		o := outcomes[step]
		o.Step = step
		o.Provider = g.Nodes[step].Step.Provider
		switch {
		case err != nil:
			o.Ran, o.Reason = false, red.Text(op+": "+err.Error())
		case op == "test":
			o.Test = raw
			var t run.TestResult
			if json.Unmarshal(raw, &t) == nil && !t.OK {
				o.Reason = "test: " + t.Reason
			}
		case op == "verify":
			o.Ran, o.Verify, o.Reason = true, raw, ""
		}
		outcomes[step] = o
	}}
	input := func(st *plan.Step) any {
		return map[string]any{
			"config":      map[string]any{"modules": map[string]any{"devices": true}},
			"environment": "audit",
			"defaults":    defaults,
			"tenant":      tenant,
			// Settings are checked in their enforced form. The pilot group is
			// required input but not compared at the enforce stage.
			"policy": map[string]any{"plan": "audit", "rollout": map[string]any{"stage": policy.ModeEnforce, "pilot_group": "grp-admins"},
				"settings": bySteps[st.ID]},
		}
	}
	x := &run.Executor{Graph: g, Catalog: cat, Invoker: inv, Events: red, Input: input, Parallel: 2, Timeout: f.timeout}
	_, _ = x.Run(ctx, run.ModeAudit)
	for step, w := range warnings {
		if o, ok := outcomes[step]; ok {
			o.Warnings = w
			outcomes[step] = o
		}
	}
	return outcomes
}
