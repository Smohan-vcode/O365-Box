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
	"github.com/Smohan-vcode/O365-Box/engine/internal/config"
	"github.com/Smohan-vcode/O365-Box/engine/internal/docs"
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
	graphOnly  bool
	partial    bool
	modulePath string
	timeout    time.Duration
}

// auditEnv is what the audit reaches the outside world through; tests
// replace it with recorded responses.
type auditEnv struct {
	signIn func(ctx context.Context, tenant string, scopes []string, out io.Writer) (*auth.Token, error)
	// signInClient is one more device-code sign-in, with another public
	// client, for the workloads named in title (T16-06).
	signInClient func(ctx context.Context, tenant, client string, scopes []string, title string, out io.Writer) (*auth.Token, error)
	// redeem exchanges a refresh token for a token to another resource.
	redeem   func(ctx context.Context, tenant, client, refresh string, scopes []string) (*auth.Token, error)
	http     *http.Client
	v1, beta string
	dns      audit.Resolver
	steps    func(ctx context.Context, root string, f *auditFlags, out io.Writer, bySteps map[string][]policy.StepSetting, tenant map[string]any, creds auditCreds) map[string]audit.StepOutcome
	now      func() time.Time
}

// auditCreds is what the policy steps read with: the audit's read-only
// Graph token and, when the sign-in is reader-only, the workload tokens.
type auditCreds struct {
	graph  string
	tokens *audit.Tokens // nil: no workload token
}

func defaultAuditEnv() *auditEnv {
	return &auditEnv{
		signIn: func(ctx context.Context, tenant string, scopes []string, out io.Writer) (*auth.Token, error) {
			// offline_access returns a refresh token, kept in memory to get
			// the workload tokens without another sign-in where Entra allows.
			return auth.SignInDeviceCode(ctx, nil, tenant, auth.GraphCLIClientID, append(auth.GraphScopes(scopes), "offline_access"), func(dc auth.DeviceCode) {
				fmt.Fprintf(out, "SIGN-IN (read only): open %s and enter the code %s (expires in %d minutes).\n  Sign in as a person with Global Reader (plus Security Reader and a Purview reader role).\n",
					dc.VerificationURI, dc.UserCode, dc.ExpiresIn/60)
			})
		},
		signInClient: func(ctx context.Context, tenant, client string, scopes []string, title string, out io.Writer) (*auth.Token, error) {
			return auth.SignInDeviceCode(ctx, nil, tenant, client, scopes, func(dc auth.DeviceCode) {
				fmt.Fprintf(out, "SIGN-IN (read only, %s): open %s and enter the code %s (expires in %d minutes).\n  Sign in as the same reader account.\n",
					title, dc.VerificationURI, dc.UserCode, dc.ExpiresIn/60)
			})
		},
		redeem: func(ctx context.Context, tenant, client, refresh string, scopes []string) (*auth.Token, error) {
			return auth.Redeem(ctx, nil, tenant, client, refresh, scopes)
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

Exchange Online, Security & Compliance (Purview), SharePoint (PnP) and Teams
are read through their own PowerShell modules, each with a delegated token
for its resource (expected-state/audit-scopes.yaml, resources). Their scopes
do not limit them to reads, so they are requested only when every directory
role of the account that signed in can only read, each must be issued to
that same account, and the provider workers run read-only (test, verify and
evidence only; module cmdlets that could write and REST calls other than GET
are refused). Each Microsoft PowerShell client takes one more device-code
sign-in as the same account (Exchange and Purview share one); --graph-only
skips them and lists those settings as not read.

What it reads: every catalogue setting through its mapping's verify (the
Entra ID and Intune policy steps over Graph; SharePoint and Teams through
their modules; Exchange and Purview once their policy steps are built);
inventories (users and admins with MFA registration and last sign-in, role
assignments, guests, Conditional Access, authentication methods, devices and
compliance, sharing, labels, retention, alerts, licences, app registrations,
SPF and DMARC); and activity over the period (directory audit, sign-ins,
alerts), as far back as the tenant keeps it, capped at --cap records each.
Settings that cannot be read with this sign-in (a missing role, a policy
step not built yet, Power Platform, a site's content) are listed as "not
read" with the reason; no value is assumed.

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
	c.Flags().BoolVar(&f.partial, "partial", false, "build the pack even when the account that signs in can change the tenant (its settings are listed as not read)")
	c.Flags().BoolVar(&f.noSettings, "no-settings", false, "skip the catalogue settings (policy step verify); inventories only")
	c.Flags().BoolVar(&f.graphOnly, "graph-only", false, "read over Microsoft Graph only: no Exchange, Purview, SharePoint or Teams sign-in (their settings are listed as not read)")
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
	defer tok.Drop()
	sess := audit.NewSession(tok.TenantID(), tok.Subject(), tok.Scopes(), tok.Claims)
	who, _ := tok.Claims["preferred_username"].(string)
	if who == "" {
		who, _ = tok.Claims["upn"].(string)
	}
	fmt.Fprintf(out, "✓ signed in (read only) to tenant %s as %s; roles: %s\n", sess.Tenant, who, audit.RoleNames(sess.Roles))
	if sess.WriterRole && !f.partial {
		// A device-code page often reuses the browser's admin session. Stop
		// before reading rather than produce a half-read pack.
		fmt.Fprintln(out, "✗ this account can change the tenant, so most settings could not be read safely.")
		fmt.Fprintln(out, "  Sign in as a reader-only account (Global Reader, Security Reader): use a private browser window")
		fmt.Fprintln(out, "  so the code page does not reuse an administrator's session, then run box audit again.")
		fmt.Fprintln(out, "  (--partial builds the pack anyway, with those settings listed as not read.)")
		return exitError{ExitValidation}
	}
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
	allowed, why := sess.ProvidersAllowed()

	// The workload tokens are asked for now, so every sign-in happens before
	// the long reads.
	var toks *audit.Tokens
	if allowed && !f.noSettings && !f.graphOnly && len(scopes.Resources) > 0 {
		_, spAdmin := sharePointHosts(initialDomain(ctx, gc, env.v1))
		toks = workloadTokens(ctx, env, f, out, sess, tok, scopes.Resources, spAdmin)
		defer toks.Drop()
		for _, r := range scopes.Resources {
			if reason := toks.Reason(r.Name); reason != "" {
				notes = append(notes, reason+".")
			}
		}
	}

	rd := &audit.Reader{Graph: gc, V1: env.v1, Beta: env.beta, DNS: env.dns, Period: period, Cap: f.cap, Now: now,
		Progress: func(id, title string) { fmt.Fprintf(out, "  … %s\n", title) }}
	fmt.Fprintf(out, "Reading inventories for %s…\n", period)
	invs := rd.ReadAll(ctx)
	fmt.Fprintln(out, "  … Policy documents and control task records (Compliance and Admin sites)")
	recs, pinvs := rd.ReadPolicies(ctx, policyLocation(root, rd.InitialDomain(), &notes))
	invs = append(invs, pinvs...)

	bySteps := audit.AuditSelection(cat, maps)
	outcomes := map[string]audit.StepOutcome{}
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
			if audit.GraphProviders[prov] || audit.TokenProviders[prov] {
				outcomes[st] = audit.StepOutcome{Step: st, Provider: prov, Reason: why}
			}
		}
		notes = append(notes, "Catalogue settings read by the Entra ID, Intune, Exchange, Purview, SharePoint and Teams policy steps were not read: "+why+".")
	default:
		tenant := map[string]any{"id": sess.Tenant, "initial_domain": rd.InitialDomain()}
		if root, admin := sharePointHosts(rd.InitialDomain()); root != "" {
			tenant["sharepoint"], tenant["sharepoint_admin"] = root, admin
		}
		fmt.Fprintln(out, "Reading catalogue settings through the policy steps (audit mode: test, verify, evidence)…")
		outcomes = env.steps(ctx, root, f, out, bySteps, tenant, auditCreds{graph: tok.Access, tokens: toks})
		explainUnbuilt(bySteps, maps, outcomes, toks, f.graphOnly)
	}
	settings := audit.ReadSettings(cat, maps, bySteps, outcomes)
	notes = append(notes, notReadNote(settings)...)

	a := audit.AssessWith(cat, fw, settings, invs, recs)
	pack := &audit.Pack{Tenant: sess.Tenant, Organisation: rd.OrganisationName(), Subject: sess.Subject, ReadBy: "box audit " + Version,
		Period: period, ReadAt: now().UTC(), Framework: fw, Assessment: a, Inventories: invs, Settings: settings, Steps: outcomes, Notes: notes}
	tok.Drop()
	toks.Drop()
	gc = nil
	files, err := audit.Write(dir, pack)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "✓ sign-in token dropped (never written), with every workload token")
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

// workloadTokens asks for the Exchange, Purview, SharePoint and Teams tokens
// (T16-06) and prints each outcome. Tokens refuses them unless the sign-in
// is reader-only.
func workloadTokens(ctx context.Context, env *auditEnv, f *auditFlags, out io.Writer, sess audit.Session, main *auth.Token, list []audit.Resource, spAdmin string) *audit.Tokens {
	tenant := sess.Tenant
	if tenant == "" {
		tenant = f.tenant
	}
	src := audit.TokenSource{}
	if env.redeem != nil {
		src.Redeem = func(ctx context.Context, client, refresh string, scopes []string) (*auth.Token, error) {
			return env.redeem(ctx, tenant, client, refresh, scopes)
		}
	}
	if env.signInClient != nil {
		src.SignIn = func(ctx context.Context, client string, scopes []string, title string) (*auth.Token, error) {
			return env.signInClient(ctx, tenant, client, scopes, title, out)
		}
	}
	toks := audit.NewTokens(sess, auth.GraphCLIClientID, main, src)
	toks.Now = env.now
	fmt.Fprintln(out, "Signing in read-only to Exchange Online, Security & Compliance, SharePoint and Teams…")
	toks.Acquire(ctx, list, spAdmin, func(r audit.Resource, err error) {
		title := r.Title
		if title == "" {
			title = r.Name
		}
		if err != nil {
			fmt.Fprintf(out, "  ✗ %s: not signed in: %v\n", title, err)
			return
		}
		fmt.Fprintf(out, "  ✓ %s: signed in read-only as the same account\n", title)
	})
	return toks
}

// initialDomain reads the tenant's initial domain (for its SharePoint
// hosts) before the inventories, so every sign-in happens first.
func initialDomain(ctx context.Context, gc *graph.Client, v1 string) string {
	if v1 == "" {
		v1 = "https://graph.microsoft.com/v1.0"
	}
	var org struct {
		Value []struct {
			VerifiedDomains []struct {
				Name      string `json:"name"`
				IsInitial bool   `json:"isInitial"`
			} `json:"verifiedDomains"`
		} `json:"value"`
	}
	if gc.Get(ctx, v1+"/organization?$select=verifiedDomains", &org) != nil {
		return ""
	}
	for _, o := range org.Value {
		for _, d := range o.VerifiedDomains {
			if d.IsInitial {
				return d.Name
			}
		}
	}
	return ""
}

// sharePointHosts are the tenant's SharePoint root and admin hosts, from its
// initial domain (<name>.onmicrosoft.com).
func sharePointHosts(initial string) (root, admin string) {
	name, _, _ := strings.Cut(strings.ToLower(initial), ".")
	if name == "" {
		return "", ""
	}
	return "https://" + name + ".sharepoint.com", "https://" + name + "-admin.sharepoint.com"
}

// explainUnbuilt gives the settings of steps that did not run, for a
// provider read with a workload token, the real reason: the step is not
// built yet, or there is no token and why.
func explainUnbuilt(bySteps map[string][]policy.StepSetting, maps map[string]policy.Mapping, outcomes map[string]audit.StepOutcome, toks *audit.Tokens, graphOnly bool) {
	for st, ss := range bySteps {
		if _, ok := outcomes[st]; ok || len(ss) == 0 {
			continue
		}
		prov := maps[ss[0].ID].Provider
		if !audit.TokenProviders[prov] {
			continue
		}
		res := audit.StepResource(prov, st)
		reason := ""
		switch {
		case graphOnly:
			reason = "needs " + audit.NeedsFor(prov, st) + "; skipped with --graph-only"
		case toks != nil && toks.Has(res):
			reason = "the policy step " + st + " is not built yet: the read-only sign-in for it worked, but no verify reads these settings"
		case toks != nil && toks.Reason(res) != "":
			reason = "needs " + audit.NeedsFor(prov, st) + ": " + toks.Reason(res)
		default:
			continue
		}
		outcomes[st] = audit.StepOutcome{Step: st, Provider: prov, Reason: reason}
	}
}

// notReadNote summarises the settings not read, by reason.
func notReadNote(settings map[string]*audit.SettingRead) []string {
	by, unbuilt, addon := map[string]int{}, map[string]int{}, 0
	for _, s := range settings {
		switch {
		case s.Status != audit.NotRead:
		case strings.HasPrefix(s.Detail, "needs a paid add-on"):
			addon++
		case strings.HasPrefix(s.Detail, "needs "):
			by[s.Provider]++
		case strings.Contains(s.Detail, " is not built yet"):
			unbuilt[s.Step]++
		}
	}
	var out []string
	if addon > 0 {
		out = append(out, fmt.Sprintf("%d add-on setting(s) are outside the subscription and have no read (not read).", addon))
	}
	for _, st := range sortedKeys(unbuilt) {
		out = append(out, fmt.Sprintf("%d setting(s) not read: the policy step %s that verifies them is not built yet.", unbuilt[st], st))
	}
	for _, p := range sortedKeys(by) {
		out = append(out, fmt.Sprintf("%d %s setting(s) not read: needs %s.", by[p], p, audit.NeedsFor(p, "")))
	}
	return out
}

// auditStepReason says why a built policy step cannot run in audit mode
// ("" when it can): its provider reads neither over Graph nor with a
// workload token the audit holds.
func auditStepReason(st *plan.Step, creds auditCreds) string {
	switch {
	case audit.GraphProviders[st.Provider]:
		return ""
	case !audit.TokenProviders[st.Provider]:
		return "needs " + audit.NeedsFor(st.Provider, st.ID)
	}
	res := audit.StepResource(st.Provider, st.ID)
	if creds.tokens != nil && creds.tokens.Has(res) {
		return ""
	}
	reason := "needs " + audit.NeedsFor(st.Provider, st.ID)
	if creds.tokens != nil && creds.tokens.Reason(res) != "" {
		reason += ": " + creds.tokens.Reason(res)
	}
	return reason
}

// auditWorkerCreds is what a provider worker gets in audit mode: the
// read-only Graph token, as "app" too for the Graph providers, and for the
// others their workload tokens under the names they read (app, ipps,
// teams). Tokens near expiry are renewed from the refresh token.
func auditWorkerCreds(ctx context.Context, provider string, creds auditCreds, secret func(string)) (map[string]any, error) {
	g := map[string]any{"access_token": creds.graph}
	out := map[string]any{"graph": g}
	if audit.GraphProviders[provider] {
		out["app"] = g
		return out, nil
	}
	if creds.tokens == nil {
		return out, nil
	}
	for cred, name := range creds.tokens.ForProvider(provider) {
		a, err := creds.tokens.Token(ctx, name)
		if err != nil {
			return nil, err
		}
		secret(a)
		out[cred] = map[string]any{"access_token": a}
	}
	return out, nil
}

// runAuditSteps runs the policy steps in audit mode, in read-only provider
// workers: Entra ID and Intune with the Graph token, SharePoint, Teams and
// (once built) Exchange and Purview with their workload tokens. It returns
// each step's test and verify result.
func runAuditSteps(ctx context.Context, root string, f *auditFlags, out io.Writer, bySteps map[string][]policy.StepSetting, tenant map[string]any, creds auditCreds) map[string]audit.StepOutcome {
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
	running := map[string]bool{}
	for id := range bySteps {
		var st *plan.Step
		if cat != nil {
			st = cat.Steps[id]
		}
		if st == nil {
			// Not built yet (T14): the caller and ReadSettings say so.
			continue
		}
		why := auditStepReason(st, creds)
		switch {
		case why != "":
			outcomes[id] = audit.StepOutcome{Step: id, Provider: st.Provider, Reason: why}
		case reason != "":
			outcomes[id] = audit.StepOutcome{Step: id, Provider: st.Provider, Reason: reason}
		default:
			g.Nodes[id] = &plan.Node{Step: st}
			g.Order = append(g.Order, id)
			running[st.Provider] = true
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
	red.AddSecret(creds.graph)
	if creds.tokens != nil {
		for _, s := range creds.tokens.Secrets() {
			red.AddSecret(s)
		}
	}
	specs := map[string]providers.PoolSpec{}
	for name, man := range cat.Manifests {
		if running[name] {
			// Read-only workers: test, verify and evidence only, and no module
			// cmdlet or REST call that could write (BoxWorker.ps1).
			specs[name] = providers.PoolSpec{Provider: man.ModuleFile(), Modules: man.Modules, ReadOnly: true}
		}
	}
	pool := &providers.Pool{
		Script:     filepath.Join(root, "engine", "providers", "runtime", "BoxWorker.ps1"),
		ModulePath: f.modulePath,
		Specs:      specs,
		CredsFunc: func(ctx context.Context, provider string) (map[string]any, error) {
			return auditWorkerCreds(ctx, provider, creds, red.AddSecret)
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

// policyLocation is where the box keeps the policy documents and the
// register: the sites of defaults/structure.yaml holding the Policies
// library and the register lists, and the templates' recurring records.
func policyLocation(root, initialDomain string, notes *[]string) audit.PolicyLocation {
	loc := audit.PolicyLocation{Library: PoliciesLibrary, PoliciesSite: "compliance", AdminSite: "admin"}
	if name, _, _ := strings.Cut(strings.ToLower(initialDomain), "."); name != "" {
		loc.Host = name + ".sharepoint.com"
	}
	var st config.Structure
	if err := readYAMLFile(filepath.Join(root, "defaults", "structure.yaml"), &st); err == nil {
		for _, s := range st.Sites {
			if s.Name == "Admin" {
				loc.AdminSite = s.URL
			}
			for _, l := range s.Libraries {
				if l.Name == PoliciesLibrary {
					loc.PoliciesSite = s.URL
				}
			}
		}
	}
	if ds, err := loadDocs(root); err != nil {
		*notes = append(*notes, "Policy templates not loaded ("+err.Error()+"); recurring records come from the catalogue only.")
	} else {
		loc.Records = docs.RecordsByRequirement(ds)
	}
	return loc
}
