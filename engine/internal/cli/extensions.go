package cli

// box extensions: Microsoft Edge extensions on company devices (T14-18,
// T14-19, T14-20). Staff ask in the Admin site's ExtensionRequests list
// (its form, or box extensions request); the owner decides with box
// extensions approve or reject, or from the home screen. An approval adds
// the extension id to the configuration's devices.browser.extension_allowlist
// as a new configuration version; the Edge policies read it, and the change
// reaches devices through the normal plan, approve and apply (SPEC §25).
// The box never adds an extension without the owner's decision.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/Smohan-vcode/O365-Box/engine/internal/approval"
	"github.com/Smohan-vcode/O365-Box/engine/internal/config"
	"github.com/Smohan-vcode/O365-Box/engine/internal/extensions"
	"github.com/Smohan-vcode/O365-Box/engine/internal/graph"
	"github.com/Smohan-vcode/O365-Box/engine/internal/tui"
)

// ExtensionRequestsStep keeps the ExtensionRequests list open to staff.
const ExtensionRequestsStep = "sharepoint.extension-requests"

// ForcedChoice is the choice that installs approved extensions for everyone.
const ForcedChoice = "devices.browser_forced_extensions"

// extensionPolicySteps are the steps an approval's configuration change
// reaches: the configuration store, then the Edge policies.
var extensionPolicySteps = []string{"sharepoint.config-store", "policy.intune-windows", "policy.intune-macos"}

// extDeps is what deciding a request needs; the CLI and the home screen
// share it.
type extDeps struct {
	reg      *extensions.Register
	docPath  string
	root     string
	who      string
	dir      string // the tenant's local records (events.jsonl)
	tenant   string
	now      func() time.Time
	lookup   func(ctx context.Context, id string) (string, error)
	everyone bool // the configuration answers devices.browser_forced_extensions=everyone
}

func newExtensionsCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "extensions", Short: "Microsoft Edge extensions: staff requests, the owner's decisions and the extensions in use (T14-18..20)"}
	cmd.AddCommand(newExtRequestsCmd(g), newExtRequestCmd(g), newExtApproveCmd(g), newExtRejectCmd(g), newExtReportCmd(g))
	return cmd
}

// extSession opens the tenant session and the register over Graph.
func extSession(g *globals, f *runFlags, out io.Writer) (*session, *extDeps, context.Context, func(), error) {
	s, err := g.open(f, out, false)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	fail := func(e error) (*session, *extDeps, context.Context, func(), error) {
		s.pool.Close()
		return nil, nil, nil, nil, e
	}
	if !s.doc.Config.Modules.Devices {
		return fail(usageError("module devices is off: there are no company devices to manage extensions on"))
	}
	if s.pipeline == nil || s.graphToken == nil {
		return fail(usageError("this tenant has no pipeline identity yet: run box bootstrap first"))
	}
	ctx, cancel := signalContext()
	closeAll := func() { cancel(); s.pool.Close() }
	tok, err := s.graphToken(ctx)
	if err != nil {
		closeAll()
		return nil, nil, nil, nil, err
	}
	name, _, _ := strings.Cut(strings.ToLower(s.pipeline.InitialDomain), ".")
	admin := ""
	for _, st := range s.doc.Config.Structure.Sites {
		if st.Name == "Admin" {
			admin = st.URL
		}
	}
	if name == "" || admin == "" {
		closeAll()
		return nil, nil, nil, nil, errors.New("no initial domain or Admin site in the configuration")
	}
	gc := &graph.Client{Token: func() string { return tok }}
	reg, err := extensions.OpenRegister(ctx, gc, graph.Base, name+".sharepoint.com", admin)
	if err != nil {
		closeAll()
		return nil, nil, nil, nil, err
	}
	d := &extDeps{reg: reg, docPath: s.doc.File, root: s.root, who: approval.Who(), dir: s.dir, tenant: s.doc.Config.Tenant.TenantID,
		now: func() time.Time { return time.Now().UTC() },
		lookup: func(ctx context.Context, id string) (string, error) {
			return extensions.LookupName(ctx, nil, id)
		}}
	if sel, err := selection(s.root, &s.doc.Config); err == nil {
		d.everyone = sel.Choices[ForcedChoice] == "everyone"
	}
	return s, d, ctx, closeAll, nil
}

func requestNumber(arg string) (int, error) {
	n, err := strconv.Atoi(strings.TrimPrefix(arg, "#"))
	if err != nil || n <= 0 {
		return 0, usageError(fmt.Sprintf("%q is not a request number (box extensions requests lists them)", arg))
	}
	return n, nil
}

func newExtRequestsCmd(g *globals) *cobra.Command {
	f := &runFlags{}
	var all bool
	c := &cobra.Command{Use: "requests", Short: "List the extension requests waiting for the owner (--all: every request)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			s, d, ctx, done, err := extSession(g, f, out)
			if err != nil {
				return err
			}
			defer done()
			rs, err := d.reg.Requests(ctx)
			if err != nil {
				return err
			}
			if !all {
				rs = extensions.Pending(rs)
			}
			lists := extensions.Lists{Allow: s.doc.Config.Devices.Browser.ExtensionAllowlist, Forced: s.doc.Config.Devices.Browser.ForcedExtensions}
			if g.asJSON {
				return writeJSON(out, map[string]any{"requests": rs, "allowlist": lists.Allow, "forced": lists.Forced, "form": requestForm(s)})
			}
			printRequests(out, rs)
			fmt.Fprintf(out, "approved extensions in the configuration: %d (installed for everyone: %d)\n", len(lists.Allow), len(lists.Forced))
			if p := extensions.Pending(rs); len(p) > 0 {
				fmt.Fprintln(out, "decide with box extensions approve <number> [--install-for-everyone] or box extensions reject <number> --reason \"...\"")
			}
			if u := requestForm(s); u != "" {
				fmt.Fprintln(out, "staff ask at "+u)
			}
			return nil
		}}
	f.bind(c, false)
	c.Flags().BoolVar(&all, "all", false, "list every request, decided ones too")
	return c
}

// requestForm is the list's new-item form staff use.
func requestForm(s *session) string {
	if s.pipeline == nil {
		return ""
	}
	name, _, _ := strings.Cut(strings.ToLower(s.pipeline.InitialDomain), ".")
	for _, st := range s.doc.Config.Structure.Sites {
		if st.Name == "Admin" && name != "" {
			return "https://" + name + ".sharepoint.com/sites/" + st.URL + "/Lists/" + extensions.List + "/NewForm.aspx"
		}
	}
	return ""
}

func printRequests(out io.Writer, rs []extensions.Request) {
	if len(rs) == 0 {
		fmt.Fprintln(out, "no extension requests waiting")
		return
	}
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "NUMBER\tSTATE\tEXTENSION ID\tEXTENSION\tREQUESTER\tFOR EVERYONE\tREASON")
	for _, r := range rs {
		ev := ""
		if r.InstallForEveryone {
			ev = "asked"
		}
		id := r.ExtensionID
		if !extensions.ValidID(id) {
			id += " (not a valid id)"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n", r.Item, r.State, id, r.Label(), r.Requester, ev, oneLine(r.Reason, 60))
	}
	tw.Flush()
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func newExtRequestCmd(g *globals) *cobra.Command {
	f := &runFlags{}
	var name, reason, by string
	var everyone bool
	c := &cobra.Command{Use: "request <extension-id>", Short: "Ask for an extension on behalf of a person (staff normally use the list's form)", Args: cobra.ExactArgs(1),
		Long: "Adds an ExtensionRequests item in state requested, for the owner to decide. It approves nothing.\n" +
			"--by names the person asking (a user id in the configuration).",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			id := strings.ToLower(strings.TrimSpace(args[0]))
			if err := extensions.CheckID(id); err != nil {
				return usageError(err.Error())
			}
			if strings.TrimSpace(reason) == "" {
				return usageError("say why the extension is needed: --reason \"...\"")
			}
			s, d, ctx, done, err := extSession(g, f, out)
			if err != nil {
				return err
			}
			defer done()
			if s.doc.Config.Tenant.Environment == "live" && !f.live {
				return usageError("this configuration targets a live tenant: pass --live to add a request there")
			}
			if !userExists(s.doc.Config.Users, by) {
				return usageError("no user " + by + " in the configuration")
			}
			n, err := d.reg.Submit(ctx, extensions.Request{ExtensionID: id, Name: strings.TrimSpace(name), Reason: strings.TrimSpace(reason), Requester: by, InstallForEveryone: everyone})
			if err != nil {
				return err
			}
			if g.asJSON {
				return writeJSON(out, map[string]any{"item": n, "extension_id": id, "state": extensions.Requested})
			}
			fmt.Fprintf(out, "✓ request %d: %s for %s, waiting for the owner (box extensions approve %d, or reject)\n", n, id, by, n)
			return nil
		}}
	f.bind(c, false)
	c.Flags().StringVar(&name, "name", "", "the extension's name as the person knows it")
	c.Flags().StringVar(&reason, "reason", "", "why it is needed (required)")
	c.Flags().StringVar(&by, "by", "", "the person asking (a user id in the configuration)")
	c.Flags().BoolVar(&everyone, "ask-for-everyone", false, "ask for it to be installed for everyone (the owner decides)")
	_ = c.MarkFlagRequired("by")
	return c
}

func userExists(users []config.User, id string) bool {
	for _, u := range users {
		if u.ID == id && !u.BreakGlass {
			return true
		}
	}
	return false
}

func confirm(cmd *cobra.Command, out io.Writer, word, what string) error {
	fmt.Fprintf(out, "Type %q to %s: ", word, what)
	line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if strings.TrimSpace(line) != word {
		fmt.Fprintln(out)
		return usageError("not confirmed: nothing recorded")
	}
	return nil
}

// approved is what an approval did.
type approved struct {
	Request   extensions.Request
	Label     string
	Changed   bool // the configuration changed
	Everyone  bool
	EventID   string
	EventNote string // set when the Events item waits locally
}

// pendingRequest reads a request and checks it can be decided.
func (d *extDeps) pendingRequest(ctx context.Context, n int) (*extensions.Request, error) {
	r, err := d.reg.Request(ctx, n)
	if err != nil {
		return nil, err
	}
	if r.State != extensions.Requested {
		return nil, usageError(fmt.Sprintf("request %d is already %s (by %s): a decision is not changed; a new request starts again", n, r.State, r.DecidedBy))
	}
	return r, nil
}

// approve records the owner's approval: the configuration gains the id
// (a new version, validated before it is written), the request item is
// marked approved, and one Events item is written.
func (d *extDeps) approve(ctx context.Context, r *extensions.Request, everyone bool) (*approved, error) {
	if err := extensions.CheckID(r.ExtensionID); err != nil {
		return nil, usageError(err.Error() + "; reject the request and ask for the right id")
	}
	res := &approved{Request: *r, Everyone: everyone}
	store, lerr := d.lookup(ctx, r.ExtensionID)
	res.Label = r.Label()
	if lerr == nil && store != "" {
		res.Label = store
	}
	before, err := os.ReadFile(d.docPath)
	if err != nil {
		return nil, err
	}
	cur, err := extensions.ReadLists(before)
	if err != nil {
		return nil, err
	}
	next, changed := cur.Approve(r.ExtensionID, everyone)
	res.Changed = changed
	var after []byte
	if changed {
		if after, err = extensions.SetLists(before, next); err != nil {
			return nil, err
		}
		doc, err := config.Parse(d.docPath, after)
		if err != nil {
			return nil, err
		}
		opt, err := engineOptions(d.root)
		if err != nil {
			return nil, err
		}
		issues, err := config.Validate(doc, opt)
		if err != nil {
			return nil, err
		}
		if len(issues) > 0 {
			return nil, validationError("the configuration with this extension would not be valid: " + issues[0].String())
		}
	}
	// The configuration first: if the request cannot be marked approved,
	// the configuration goes back to what it was.
	if changed {
		if err := extensions.WriteFile(d.docPath, after); err != nil {
			return nil, err
		}
	}
	at := d.now()
	if err := d.reg.Decide(ctx, r.Item, extensions.Decision{State: extensions.Approved, By: d.who, At: at, StoreName: store, Everyone: everyone}); err != nil {
		if changed {
			if rerr := extensions.WriteFile(d.docPath, before); rerr != nil {
				return nil, fmt.Errorf("%w; and the configuration could not be restored: %v", err, rerr)
			}
		}
		return nil, err
	}
	ev := &approval.EventItem{At: at, Actor: d.who, Entity: extensions.List, EntityID: strconv.Itoa(r.Item), Action: "approve",
		FromState: extensions.Requested, ToState: extensions.Approved,
		Detail: map[string]any{"extension_id": r.ExtensionID, "install_for_everyone": everyone, "configuration_changed": changed}}
	res.EventID, res.EventNote = d.event(ctx, ev)
	return res, nil
}

// reject records the owner's rejection with the reason.
func (d *extDeps) reject(ctx context.Context, r *extensions.Request, reason string) (string, string, error) {
	if strings.TrimSpace(reason) == "" {
		return "", "", usageError("a rejection needs a reason: --reason \"...\" (the requester sees it)")
	}
	at := d.now()
	if err := d.reg.Decide(ctx, r.Item, extensions.Decision{State: extensions.Rejected, By: d.who, At: at, Reason: strings.TrimSpace(reason)}); err != nil {
		return "", "", err
	}
	ev := &approval.EventItem{At: at, Actor: d.who, Entity: extensions.List, EntityID: strconv.Itoa(r.Item), Action: "reject",
		FromState: extensions.Requested, ToState: extensions.Rejected, Detail: map[string]any{"extension_id": r.ExtensionID, "reason": strings.TrimSpace(reason)}}
	id, note := d.event(ctx, ev)
	return id, note, nil
}

// event writes the Events item to the tenant; when that fails it waits in
// the local events file (approval.EventsFile) with the same id.
func (d *extDeps) event(ctx context.Context, ev *approval.EventItem) (string, string) {
	if ev.EventID == "" {
		id, err := approval.NewEventID()
		if err != nil {
			return "", "Events item not written: " + err.Error()
		}
		ev.EventID = id
	}
	if err := d.reg.AddEvent(ctx, ev); err != nil {
		if lerr := approval.AppendEvent(filepath.Join(d.dir, approval.EventsFile), ev); lerr != nil {
			return ev.EventID, "Events item not written: " + err.Error() + "; " + lerr.Error()
		}
		return ev.EventID, "Events item kept locally until the Events list can be written: " + err.Error()
	}
	return ev.EventID, ""
}

func nextSteps(out io.Writer) {
	fmt.Fprintln(out, "  next: the allow list is part of the configuration; send it to devices with")
	fmt.Fprintf(out, "    box plan --proposal --only %s, then box approve and box apply\n", strings.Join(extensionPolicySteps, ","))
}

func newExtApproveCmd(g *globals) *cobra.Command {
	f := &runFlags{}
	var everyone bool
	c := &cobra.Command{Use: "approve <number>", Short: "Approve an extension request: the extension is added to the approved list", Args: cobra.ExactArgs(1),
		Long: "Records the owner's approval of an ExtensionRequests item: the extension id is added to the configuration's\n" +
			"devices.browser.extension_allowlist (a new configuration version), the item is marked approved and one Events\n" +
			"item is written. --install-for-everyone also adds it to devices.browser.forced_extensions; Edge installs those\n" +
			"for everyone only while the choice devices.browser_forced_extensions is \"everyone\". Nothing reaches devices\n" +
			"until the resulting proposal is approved and applied. It asks you to type \"approve\"; --yes skips that, for\n" +
			"scripts the owner runs themselves.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			n, err := requestNumber(args[0])
			if err != nil {
				return err
			}
			s, d, ctx, done, err := extSession(g, f, out)
			if err != nil {
				return err
			}
			defer done()
			if s.doc.Config.Tenant.Environment == "live" && !f.live {
				return usageError("this configuration targets a live tenant: pass --live to decide a request there")
			}
			r, err := d.pendingRequest(ctx, n)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "request %d: %s\n  extension id %s\n  asked by %s: %s\n", r.Item, r.Label(), r.ExtensionID, r.Requester, oneLine(r.Reason, 200))
			if everyone {
				fmt.Fprintln(out, "  install for everyone: yes")
			}
			if !f.yes {
				if err := confirm(cmd, out, "approve", "allow this extension on company devices"); err != nil {
					return err
				}
			}
			res, err := d.approve(ctx, r, everyone)
			if err != nil {
				return err
			}
			if g.asJSON {
				return writeJSON(out, res)
			}
			fmt.Fprintf(out, "✓ request %d approved by %s: %s (%s)\n", n, d.who, res.Label, r.ExtensionID)
			if res.Changed {
				fmt.Fprintf(out, "  configuration %s: devices.browser.extension_allowlist now holds it\n", d.docPath)
			} else {
				fmt.Fprintln(out, "  the extension was already approved: the configuration is unchanged")
			}
			if everyone && !d.everyone {
				fmt.Fprintf(out, "  ! it is installed for everyone only once the choice %s is \"everyone\"\n", ForcedChoice)
			}
			if res.EventNote != "" {
				fmt.Fprintln(out, "  ! "+res.EventNote)
			}
			if res.Changed {
				nextSteps(out)
			}
			return nil
		}}
	f.bind(c, false)
	c.Flags().BoolVar(&everyone, "install-for-everyone", false, "also install it for everyone (needs the choice devices.browser_forced_extensions=everyone)")
	return c
}

func newExtRejectCmd(g *globals) *cobra.Command {
	f := &runFlags{}
	var reason string
	c := &cobra.Command{Use: "reject <number>", Short: "Reject an extension request, with the reason", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			n, err := requestNumber(args[0])
			if err != nil {
				return err
			}
			if strings.TrimSpace(reason) == "" {
				return usageError("a rejection needs a reason: --reason \"...\" (the requester sees it)")
			}
			s, d, ctx, done, err := extSession(g, f, out)
			if err != nil {
				return err
			}
			defer done()
			if s.doc.Config.Tenant.Environment == "live" && !f.live {
				return usageError("this configuration targets a live tenant: pass --live to decide a request there")
			}
			r, err := d.pendingRequest(ctx, n)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "request %d: %s (%s), asked by %s\n", r.Item, r.Label(), r.ExtensionID, r.Requester)
			if !f.yes {
				if err := confirm(cmd, out, "reject", "reject this request"); err != nil {
					return err
				}
			}
			id, note, err := d.reject(ctx, r, reason)
			if err != nil {
				return err
			}
			if g.asJSON {
				return writeJSON(out, map[string]any{"item": n, "state": extensions.Rejected, "event_id": id, "note": note})
			}
			fmt.Fprintf(out, "✓ request %d rejected by %s (%s)\n", n, d.who, id)
			if note != "" {
				fmt.Fprintln(out, "  ! "+note)
			}
			return nil
		}}
	f.bind(c, false)
	c.Flags().StringVar(&reason, "reason", "", "why the request is rejected (required)")
	return c
}

// homeHooks are the home screen's Requests and DecideRequest options
// (T14-18): the same decisions as box extensions approve and reject.
func (d *extDeps) homeHooks(ctx context.Context) (func() ([]tui.ExtensionRequest, error), func(tui.RequestDecision) (string, error)) {
	list := func() ([]tui.ExtensionRequest, error) {
		rs, err := d.reg.Requests(ctx)
		if err != nil {
			return nil, err
		}
		var out []tui.ExtensionRequest
		for _, r := range extensions.Pending(rs) {
			out = append(out, tui.ExtensionRequest{Number: r.Item, ExtensionID: r.ExtensionID, Label: r.Label(), Requester: r.Requester,
				Reason: oneLine(r.Reason, 200), AskEveryone: r.InstallForEveryone})
		}
		return out, nil
	}
	decide := func(q tui.RequestDecision) (string, error) {
		r, err := d.pendingRequest(ctx, q.Number)
		if err != nil {
			return "", err
		}
		if !q.Approve {
			if _, note, err := d.reject(ctx, r, q.Reason); err != nil {
				return "", err
			} else if note != "" {
				return fmt.Sprintf("Request %d turned down. %s", q.Number, note), nil
			}
			return fmt.Sprintf("Request %d turned down; the person who asked sees your reason.", q.Number), nil
		}
		res, err := d.approve(ctx, r, q.Everyone)
		if err != nil {
			return "", err
		}
		msg := fmt.Sprintf("Request %d approved: %s. It reaches devices once you approve the new proposal.", q.Number, res.Label)
		if q.Everyone && !d.everyone {
			msg += " It is installed for everyone once you choose that for browser extensions."
		}
		return msg, nil
	}
	return list, decide
}
