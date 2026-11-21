package cli

import (
	"context"
	"errors"
	"fmt"
	"github.com/Smohan-vcode/O365-Box/engine/internal/policy"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/Smohan-vcode/O365-Box/engine/internal/approval"
	"github.com/Smohan-vcode/O365-Box/engine/internal/bootstrap"
	"github.com/Smohan-vcode/O365-Box/engine/internal/config"
	"github.com/Smohan-vcode/O365-Box/engine/internal/events"
	"github.com/Smohan-vcode/O365-Box/engine/internal/plan"
	"github.com/Smohan-vcode/O365-Box/engine/internal/providers"
	"github.com/Smohan-vcode/O365-Box/engine/internal/run"
	"github.com/Smohan-vcode/O365-Box/engine/internal/state"
)

// runFlags are shared by plan, up, drift, apply, check, status and gate.
type runFlags struct {
	config       string
	statePath    string
	only         []string
	parallel     int
	yes          bool
	live         bool
	capabilities []string
	providersDir string
	gatesFile    string
	modulePath   string
	timeout      time.Duration
	dictionary   string
	proposal     bool
}

func (f *runFlags) bind(cmd *cobra.Command, withOnly bool) {
	cmd.Flags().StringVar(&f.config, "config", "", "configuration document (until T0-06 adds the tenant store)")
	cmd.Flags().StringVar(&f.statePath, "state", "", "state cache file (default: user cache dir, per tenant)")
	if withOnly {
		cmd.Flags().StringSliceVar(&f.only, "only", nil, "run only these steps and their dependencies")
	}
	cmd.Flags().IntVar(&f.parallel, "parallel", 4, "steps to run at once")
	cmd.Flags().BoolVar(&f.yes, "yes", false, "non-interactive: do not wait at gates (exit 3 instead)")
	cmd.Flags().BoolVar(&f.live, "live", false, "allow changes to a tenant whose environment is live")
	cmd.Flags().StringSliceVar(&f.capabilities, "capabilities", nil, "tenant capabilities (default: from tenant.default_plan)")
	cmd.Flags().StringVar(&f.providersDir, "providers", "", "provider directory (default: engine/providers)")
	cmd.Flags().StringVar(&f.gatesFile, "gates", "", "gate definitions (default: engine/gates.yaml)")
	cmd.Flags().StringVar(&f.modulePath, "module-path", os.Getenv("BOX_MODULE_PATH"), "PSModulePath for provider workers")
	cmd.Flags().DurationVar(&f.timeout, "step-timeout", 10*time.Minute, "timeout per step operation")
	cmd.Flags().StringVar(&f.dictionary, "dictionary", "", "company data values to seed the Dictionary (until the onboarding collects them)")
	_ = cmd.Flags().MarkHidden("providers")
	_ = cmd.Flags().MarkHidden("gates")
}

// session is everything one orchestration command needs.
type session struct {
	root       string
	doc        *config.Document
	catalog    *plan.Catalog
	graph      *plan.Graph
	state      *state.File
	pool       *providers.Pool
	sink       events.Sink
	red        *events.Redactor
	caps       map[string]bool
	pipeline   *bootstrap.Pipeline
	defaults   map[string]any
	dictionary map[string]string
	policyIn   map[string]any
	dir        string // the tenant's proposal and approvals
	areas      *approval.Areas
	approvals  *approval.File
}

func (g *globals) open(f *runFlags, out io.Writer, exact bool) (*session, error) {
	root, err := g.root()
	if err != nil {
		return nil, err
	}
	if f.config == "" {
		return nil, usageError("--config is required until the tenant configuration store exists (T0-06)")
	}
	doc, err := config.Load(f.config)
	if err != nil {
		return nil, err
	}
	opt, err := engineOptions(root)
	if err != nil {
		return nil, err
	}
	issues, err := config.Validate(doc, opt)
	if err != nil {
		return nil, err
	}
	if len(issues) > 0 {
		for _, i := range issues {
			fmt.Fprintln(out, "  ✗ "+i.String())
		}
		return nil, validationError(fmt.Sprintf("%d problem(s) in %s; run box config validate", len(issues), doc.File))
	}
	s := &session{root: root, doc: doc}
	pdir := f.providersDir
	if pdir == "" {
		pdir = filepath.Join(root, "engine", "providers")
	}
	gfile := f.gatesFile
	if gfile == "" {
		gfile = filepath.Join(root, "engine", "gates.yaml")
	}
	if s.catalog, err = plan.Load(pdir, gfile); err != nil {
		return nil, err
	}
	if s.caps, err = capabilities(root, doc, f.capabilities); err != nil {
		return nil, err
	}
	mods := doc.Config.Modules.Map()
	if s.graph, err = plan.Build(s.catalog, plan.Options{Modules: mods, Capabilities: s.caps, Only: f.only, Exact: exact}); err != nil {
		return nil, err
	}
	sp := f.statePath
	if sp == "" {
		if sp, err = state.DefaultPath(doc.Config.Tenant.TenantID); err != nil {
			return nil, err
		}
	}
	if s.state, err = state.Open(sp); err != nil {
		return nil, err
	}
	if s.dir, err = approval.Dir(doc.Config.Tenant.TenantID, f.statePath); err != nil {
		return nil, err
	}
	if s.areas, err = approval.LoadAreas(filepath.Join(root, "defaults", "areas.yaml")); err != nil {
		return nil, err
	}
	if s.approvals, err = approval.Open(filepath.Join(s.dir, approval.ApprovalsFile)); err != nil {
		return nil, err
	}
	var sink events.Sink = &textSink{w: out}
	if g.asJSON {
		sink = events.NewJSONLines(out)
	}
	s.red = events.NewRedactor(sink, nil)
	s.sink = s.red
	specs := map[string]providers.PoolSpec{}
	for name, man := range s.catalog.Manifests {
		specs[name] = providers.PoolSpec{Provider: man.ModuleFile(), Modules: man.Modules}
	}
	s.pool = &providers.Pool{
		Script:     filepath.Join(root, "engine", "providers", "runtime", "BoxWorker.ps1"),
		ModulePath: f.modulePath,
		Specs:      specs,
		OnRaw: func(p, line string) {
			s.sink.Emit(events.Event{T: time.Now().UTC(), Ev: events.Log, Level: "raw", Msg: p + ": " + line})
		},
		OnLog: func(p, step, level, msg string) {
			s.sink.Emit(events.Event{T: time.Now().UTC(), Ev: events.Log, Step: step, Level: level, Msg: msg})
		},
	}
	if f.dictionary != "" {
		if err := readYAMLFile(f.dictionary, &s.dictionary); err != nil {
			return nil, err
		}
	}
	if err := s.appCredentials(); err != nil {
		return nil, err
	}
	if s.defaults, err = engineDefaults(root); err != nil {
		return nil, err
	}
	return s, nil
}

// teamsAdminResource is Microsoft's fixed application id of the Teams
// administration API (Skype and Teams Tenant Admin API).
const teamsAdminResource = "48ac35b8-9aa8-4d74-927d-1f4a14a0b239"

// appCredentials gives providers app-only tokens from the pipeline app's
// certificate, when bootstrap has run for this tenant.
func (s *session) appCredentials() error {
	tenant := s.doc.Config.Tenant.TenantID
	pl, err := bootstrap.LoadPipeline(tenant)
	if err != nil {
		return nil // not bootstrapped yet: providers get no credentials
	}
	s.pipeline = pl
	ks, err := openKeystore()
	if err != nil {
		return err
	}
	cred, _, err := bootstrap.Credential(ks, tenant)
	if err != nil {
		return err
	}
	name, _, _ := strings.Cut(pl.InitialDomain, ".")
	name = strings.ToLower(name)
	resources := map[string]string{
		"identity": "https://graph.microsoft.com", "intune": "https://graph.microsoft.com", "teams": "https://graph.microsoft.com",
		"exchange": "https://outlook.office365.com", "sharepoint": "https://" + name + ".sharepoint.com",
	}
	s.pool.CredsFunc = func(ctx context.Context, provider string) (map[string]any, error) {
		res, ok := resources[provider]
		if !ok {
			return nil, nil
		}
		tok, err := cred.Token(ctx, res)
		if err != nil {
			return nil, err
		}
		s.red.AddSecret(tok.Access)
		// Only tokens: the worker redacts every credential string it receives.
		out := map[string]any{"app": map[string]any{"access_token": tok.Access}}
		if res == "https://graph.microsoft.com" {
			out["graph"] = out["app"]
		} else {
			// Providers also read Graph (for example group members) over REST.
			g, err := cred.Token(ctx, "https://graph.microsoft.com")
			if err != nil {
				return nil, err
			}
			s.red.AddSecret(g.Access)
			out["graph"] = map[string]any{"access_token": g.Access}
		}
		if provider == "teams" {
			// Connect-MicrosoftTeams -AccessTokens takes a Graph token and a
			// token for the Teams administration resource.
			t, err := cred.Token(ctx, teamsAdminResource)
			if err != nil {
				return nil, err
			}
			s.red.AddSecret(t.Access)
			out["teams"] = map[string]any{"access_token": t.Access}
		}
		return out, nil
	}
	return nil
}

// policyInput resolves the configuration's policy selection for the
// tenant's plan and lists, per capability, the applied settings that need
// it, with the service plans that provide each capability (T14-02).
func (s *session) policyInput() (map[string]any, error) {
	if s.policyIn != nil {
		return s.policyIn, nil
	}
	cat, issues, err := loadCatalogue(s.root)
	if err != nil {
		return nil, err
	}
	if len(issues) > 0 {
		return nil, fmt.Errorf("policy catalogue has %d problem(s); run box policy validate", len(issues))
	}
	plans, err := enginePlans(s.root)
	if err != nil {
		return nil, err
	}
	planName := s.doc.Config.Tenant.DefaultPlan
	caps := map[string]bool{}
	for _, c := range plans[planName] {
		caps[c] = true
	}
	sel, err := selection(s.root, &s.doc.Config)
	if err != nil {
		return nil, err
	}
	res, err := cat.Resolve(sel, planName, caps)
	if err != nil {
		return nil, err
	}
	needed := map[string][]string{}
	for _, c := range res.Applied {
		for _, a := range c.Settings {
			if st := cat.Setting(a.ID); st != nil {
				needed[st.Capability] = append(needed[st.Capability], a.ID)
			}
		}
	}
	sp, err := servicePlans(s.root)
	if err != nil {
		return nil, err
	}
	_, maps, err := grantData(s.root)
	if err != nil {
		return nil, err
	}
	s.policyIn = map[string]any{"plan": planName, "applied": res.AppliedCount(), "capabilities_needed": needed, "service_plans": sp,
		"rollout": map[string]any{"stage": sel.Stage, "pilot_group": s.doc.Config.Policy.Rollout.PilotGroup},
		"by_step": policy.SettingsByStep(res, maps)}
	return s.policyIn, nil
}

func servicePlans(root string) (map[string][]string, error) {
	var file struct {
		ServicePlans map[string][]string `yaml:"service_plans"`
	}
	err := readYAMLFile(filepath.Join(root, "engine", "capabilities.yaml"), &file)
	return file.ServicePlans, err
}

// engineDefaults loads the engine data steps may need (roles, baseline).
func engineDefaults(root string) (map[string]any, error) {
	out := map[string]any{}
	for key, rel := range map[string]string{"roles": "defaults/roles.yaml", "baseline_identity": "baseline/identity.yaml",
		"baseline_exchange": "baseline/exchange.yaml", "baseline_sharepoint": "baseline/sharepoint.yaml",
		"capabilities": "engine/capabilities.yaml", "lists": "engine/register/lists.yaml"} {
		var v any
		if err := readYAMLFile(filepath.Join(root, rel), &v); err != nil {
			return nil, err
		}
		out[key] = v
	}
	return out, nil
}

// capabilities returns the flag's list, or the plan's from engine/capabilities.yaml.
func capabilities(root string, doc *config.Document, flag []string) (map[string]bool, error) {
	caps := map[string]bool{}
	if len(flag) > 0 {
		for _, c := range flag {
			caps[strings.TrimSpace(c)] = true
		}
		return caps, nil
	}
	var file struct {
		Plans map[string][]string `yaml:"plans"`
	}
	if err := readYAMLFile(filepath.Join(root, "engine", "capabilities.yaml"), &file); err != nil {
		return nil, err
	}
	planName := doc.Config.Tenant.DefaultPlan
	list, ok := file.Plans[planName]
	if !ok {
		return nil, fmt.Errorf("engine/capabilities.yaml has no plan %q", planName)
	}
	for _, c := range list {
		caps[c] = true
	}
	return caps, nil
}

// withDefault returns m with path set to v unless a non-empty value is
// there; maps on the path are copied, never changed in place.
func withDefault(m map[string]any, path []string, v string) map[string]any {
	out := make(map[string]any, len(m)+1)
	for k, x := range m {
		out[k] = x
	}
	if len(path) == 1 {
		if cur, ok := out[path[0]]; !ok || cur == nil || cur == "" {
			out[path[0]] = v
		}
		return out
	}
	child, _ := out[path[0]].(map[string]any)
	if child == nil {
		if _, isOther := out[path[0]]; isOther && out[path[0]] != nil {
			return out // a non-map value is there; leave it
		}
		child = map[string]any{}
	}
	out[path[0]] = withDefault(child, path[1:], v)
	return out
}

// input gives a step only the configuration sections it declares in reads.
func (s *session) input(step *plan.Step) any {
	sub := map[string]any{}
	for _, r := range step.Reads {
		top := r
		if i := strings.IndexAny(top, ".["); i >= 0 {
			top = top[:i]
		}
		if strings.HasPrefix(top, "@") {
			continue
		}
		if v, ok := s.doc.Raw[top]; ok {
			sub[top] = v
		}
	}
	// Values the company's choices supply (SPEC §24) fill configuration keys
	// the configuration leaves empty, for the sections the step reads; a
	// policy step also gets values.* for its {text:} placeholders.
	if sel, err := selection(s.root, &s.doc.Config); err == nil {
		for k, v := range sel.Values {
			top, _, _ := strings.Cut(k, ".")
			if _, reads := sub[top]; reads || slices.Contains(step.Reads, top) || (top == "values" && slices.Contains(step.Reads, "@policy")) {
				sub = withDefault(sub, strings.Split(k, "."), v)
			}
		}
	}
	in := map[string]any{"config": sub, "environment": s.doc.Config.Tenant.Environment, "defaults": s.defaults}
	for _, r := range step.Reads {
		switch r {
		case "@document":
			// The configuration document as saved, for the configuration store.
			if b, err := os.ReadFile(s.doc.File); err == nil {
				in["document"] = string(b)
			}
		case "@dictionary":
			in["dictionary"] = s.dictionary
		case "@policy":
			if p, err := s.policyInput(); err == nil {
				// A policy.<area> step sees only its own settings (T14-04).
				q := map[string]any{}
				for k, v := range p {
					if k != "by_step" {
						q[k] = v
					}
				}
				q["settings"] = p["by_step"].(map[string][]policy.StepSetting)[step.ID]
				in["policy"] = q
			} else {
				in["policy_error"] = err.Error()
			}
		}
	}
	if s.pipeline != nil {
		name, _, _ := strings.Cut(strings.ToLower(s.pipeline.InitialDomain), ".")
		in["tenant"] = map[string]any{"id": s.pipeline.TenantID, "initial_domain": s.pipeline.InitialDomain,
			"sharepoint": "https://" + name + ".sharepoint.com", "sharepoint_admin": "https://" + name + "-admin.sharepoint.com"}
	}
	return in
}

func (s *session) executor(f *runFlags) *run.Executor {
	return &run.Executor{Graph: s.graph, Catalog: s.catalog, Invoker: s.pool, Events: s.sink, State: s.state,
		Input: s.input, Parallel: f.parallel, Retries: 2, Timeout: f.timeout, Interactive: !f.yes,
		Approvals: s.approvals}
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt)
}

func runMode(g *globals, f *runFlags, mode run.Mode, exact bool, cmd *cobra.Command) error {
	out := cmd.OutOrStdout()
	s, err := g.open(f, out, exact)
	if err != nil {
		return err
	}
	defer s.pool.Close()
	if mode == run.ModeApply && s.doc.Config.Tenant.Environment == "live" && !f.live {
		return usageError("this configuration targets a live tenant: build and verify on test first, then pass --live")
	}
	ctx, cancel := signalContext()
	defer cancel()
	x := s.executor(f)
	var proposed []approval.Change
	var pmu sync.Mutex
	if mode == run.ModePlan && f.proposal {
		x.Proposal = func(step *plan.Step, changes []run.Change) {
			pmu.Lock()
			defer pmu.Unlock()
			for _, c := range changes {
				proposed = append(proposed, approval.NewChange(s.areas, step, c))
			}
		}
	}
	sum, err := x.Run(ctx, mode)
	if !g.asJSON {
		printSummary(out, sum, len(s.graph.Order))
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	if x.Proposal != nil && err == nil {
		if perr := s.writeProposal(out, g.asJSON, x.RunID, proposed, len(sum.Failed) > 0); perr != nil {
			return perr
		}
	}
	switch {
	case len(sum.Failed) > 0 || errors.Is(err, context.Canceled):
		return exitError{ExitFailure}
	case len(sum.Waiting) > 0 || len(sum.NeedsApproval) > 0:
		return exitError{ExitGate}
	case len(sum.Drifted) > 0:
		return exitError{ExitFailure}
	}
	return nil
}

func printSummary(w io.Writer, s run.Summary, steps int) {
	var parts []string
	keys := make([]string, 0, len(s.Status))
	for k := range s.Status {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%d %s", s.Status[k], strings.ToLower(k)))
	}
	mark := "✓"
	if !s.OK() {
		mark = "✗"
	}
	fmt.Fprintf(w, "%s %s: %d step(s), %d change(s); %s (%s)\n", mark, s.Mode, steps, s.Changes,
		strings.Join(parts, ", "), s.Duration.Round(time.Millisecond))
	if len(s.Failed) > 0 {
		fmt.Fprintln(w, "  failed: "+strings.Join(s.Failed, ", "))
	}
	if len(s.Waiting) > 0 {
		fmt.Fprintln(w, "  waiting on: "+strings.Join(s.Waiting, ", ")+" (see box gate <id>)")
	}
	if len(s.Drifted) > 0 {
		fmt.Fprintln(w, "  drifted: "+strings.Join(s.Drifted, ", "))
	}
	if len(s.NeedsApproval) > 0 {
		fmt.Fprintln(w, "  needs approval: "+strings.Join(s.NeedsApproval, ", ")+" (run box plan --proposal, then box approve)")
	}
	if len(s.Excepted) > 0 {
		fmt.Fprintln(w, "  not applied, owner exception: "+strings.Join(s.Excepted, ", "))
	}
}

// textSink prints a readable line per significant event.
type textSink struct {
	mu sync.Mutex
	w  io.Writer
}

func (t *textSink) Emit(e events.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	var line string
	switch e.Ev {
	case events.StepStarted:
		line = "▸ " + e.Step
	case events.StepPlanned:
		line = "  " + e.Step + ": " + e.Msg
		if ch, ok := e.Data["changes"].([]any); ok {
			for _, c := range ch {
				if m, ok := c.(map[string]any); ok {
					line += fmt.Sprintf("\n      %v %v", m["action"], m["target"])
				}
			}
		}
	case events.StepChanged:
		line = "  ~ " + e.Step + ": " + e.Msg
	case events.StepProg:
		if e.Of > 0 {
			line = fmt.Sprintf("  … %s: %s (%d/%d)", e.Step, e.Msg, e.Done, e.Of)
		} else {
			line = "  … " + e.Step + ": " + e.Msg
		}
	case events.StepDone:
		line = "✓ " + e.Step + ": " + e.Msg
	case events.StepFailed:
		line = "✗ " + e.Step + ": " + e.Msg
	case events.StepSkipped:
		line = "- " + e.Step + ": " + e.Msg
	case events.GateWaiting:
		if e.Gate == run.ApprovalGate {
			line = "⏸ " + e.Step + ": " + e.Msg
			if ch, ok := e.Data["changes"].([]any); ok {
				for _, c := range ch {
					if m, ok := c.(map[string]any); ok {
						line += fmt.Sprintf("\n      %v %v  [%.12v]", m["action"], m["target"], m["digest"])
					}
				}
			}
			break
		}
		line = fmt.Sprintf("⏸ %s waiting on %s %s: %v", e.Step, e.Gate, e.Msg, e.Data["guide"])
	case events.GatePassed:
		line = "✓ gate " + e.Gate + " passed"
	case events.Log:
		if e.Level == "evidence" {
			line = "  ≡ " + e.Step + ": " + e.Msg
		} else if e.Level == "warning" || e.Level == "error" || e.Level == "raw" {
			line = fmt.Sprintf("  ! %s %s", e.Level, e.Msg)
		}
	}
	if line != "" {
		fmt.Fprintln(t.w, line)
	}
}

func newRunCommands(g *globals) []*cobra.Command {
	mkf := func(use, short string, mode run.Mode, withOnly bool) (*cobra.Command, *runFlags) {
		f := &runFlags{}
		c := &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error { return runMode(g, f, mode, false, cmd) }}
		f.bind(c, withOnly)
		return c, f
	}
	mk := func(use, short string, mode run.Mode, withOnly bool) *cobra.Command {
		c, _ := mkf(use, short, mode, withOnly)
		return c
	}
	planCmd, planFlags := mkf("plan", "Show what would change, changing nothing; --proposal writes it for the owner to approve", run.ModePlan, true)
	planCmd.Flags().BoolVar(&planFlags.proposal, "proposal", false, "write the tenant's proposal (every change, by area) for box approve")
	cmds := []*cobra.Command{
		planCmd,
		mk("up", "Build and harden the tenant: run every step in order, resuming where it stopped", run.ModeApply, true),
		mk("drift", "Report where the tenant differs from the configuration, changing nothing", run.ModeDrift, true),
	}
	single := func(use, short string, mode run.Mode) *cobra.Command {
		f := &runFlags{}
		c := &cobra.Command{Use: use + " <step>", Short: short, Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				f.only = []string{args[0]}
				return runMode(g, f, mode, true, cmd)
			}}
		f.bind(c, false)
		return c
	}
	cmds = append(cmds,
		single("apply", "Run one step (its dependencies are assumed done)", run.ModeApply),
		newCheckCmd(g),
		newStatusCmd(g), newGateCmd(g), newApproveCmd(g))
	return cmds
}

// newCheckCmd verifies one step, or with "identity" the pipeline app.
func newCheckCmd(g *globals) *cobra.Command {
	f := &runFlags{}
	c := &cobra.Command{Use: "check <step|identity>", Short: "Verify one step against the tenant, or the pipeline app (identity)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] != "identity" {
				f.only = []string{args[0]}
				return runMode(g, f, run.ModeCheck, true, cmd)
			}
			root, err := g.root()
			if err != nil {
				return err
			}
			doc, err := loadValidConfig(root, f.config, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			gr, err := neededGrants(root, &doc.Config)
			if err != nil {
				return err
			}
			ks, err := openKeystore()
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  expected bundles: %s\n", strings.Join(gr.IDs(), ", "))
			return checkIdentity(cmd.Context(), cmd.OutOrStdout(), doc.Config.Tenant.TenantID, ks, gr.Permissions, 0)
		}}
	f.bind(c, false)
	return c
}

func newStatusCmd(g *globals) *cobra.Command {
	f := &runFlags{}
	c := &cobra.Command{Use: "status", Short: "Show each step's last result", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			s, err := g.open(f, out, false)
			if err != nil {
				return err
			}
			type row struct {
				Step    string `json:"step"`
				Track   string `json:"track"`
				Status  string `json:"status"`
				Changes int    `json:"changes"`
				At      string `json:"at,omitempty"`
				Reason  string `json:"reason,omitempty"`
			}
			var rows []row
			for _, id := range s.graph.Order {
				n := s.graph.Nodes[id]
				r := row{Step: id, Track: n.Step.Track, Status: "PENDING"}
				if n.Skip != "" {
					r.Status, r.Reason = state.Skipped, n.Skip
				}
				if rec := s.state.Get(id); rec != nil {
					r.Status, r.Changes, r.Reason, r.At = rec.Status, rec.Changes, rec.Reason, rec.At.Format(time.RFC3339)
				}
				rows = append(rows, r)
			}
			if g.asJSON {
				return writeJSON(out, map[string]any{"command": "status", "steps": rows})
			}
			for _, r := range rows {
				extra := ""
				if r.Reason != "" {
					extra = " — " + r.Reason
				}
				fmt.Fprintf(out, "%-9s %-40s %-4s %s%s\n", r.Status, r.Step, r.Track, r.At, extra)
			}
			fmt.Fprintf(out, "%d step(s)\n", len(rows))
			return nil
		}}
	f.bind(c, true)
	return c
}

func newGateCmd(g *globals) *cobra.Command {
	f := &runFlags{}
	var check, confirm bool
	c := &cobra.Command{Use: "gate <id>", Short: "Show a gate's guide, check it, or confirm it", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			s, err := g.open(f, out, false)
			if err != nil {
				return err
			}
			defer s.pool.Close()
			gate := s.catalog.Gates[args[0]]
			if gate == nil {
				return usageError("no gate " + args[0])
			}
			passed := false
			how := ""
			switch {
			case confirm:
				if gate.Check != "" {
					return usageError(gate.ID + " is checked automatically; it cannot be confirmed by hand")
				}
				if err := s.state.ConfirmGate(gate.ID, "manual", time.Now().UTC()); err != nil {
					return err
				}
				passed, how = true, "confirmed"
			case check:
				if gate.Check == "" {
					r := s.state.GateRecord(gate.ID)
					passed, how = r != nil && r.Status == state.Done, "confirmation"
					break
				}
				ctx, cancel := signalContext()
				defer cancel()
				raw, err := s.pool.Invoke(ctx, s.catalog.Steps[gate.Check].Provider, gate.Check, "verify",
					s.input(s.catalog.Steps[gate.Check]), nil)
				if err != nil {
					return err
				}
				passed, how = strings.Contains(strings.ReplaceAll(string(raw), " ", ""), `"ok":true`), "check "+gate.Check
			}
			if g.asJSON {
				return writeJSON(out, map[string]any{"gate": gate.ID, "title": gate.Title, "guide": gate.Guide,
					"expected": gate.Expected, "checked": check || confirm, "passed": passed})
			}
			fmt.Fprintf(out, "%s  %s\n  %s\n  Expected: %s\n", gate.ID, gate.Title, gate.Guide, gate.Expected)
			if check || confirm {
				if passed {
					fmt.Fprintf(out, "✓ passed (%s)\n", how)
				} else {
					fmt.Fprintf(out, "⏸ not passed yet (%s)\n", how)
					return exitError{ExitGate}
				}
			}
			return nil
		}}
	f.bind(c, false)
	c.Flags().BoolVar(&check, "check", false, "run the gate's check now")
	c.Flags().BoolVar(&confirm, "confirm", false, "record that the gate is done (gates without a check only)")
	return c
}
