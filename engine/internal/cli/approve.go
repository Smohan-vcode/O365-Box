package cli

import (
	"bufio"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Smohan-vcode/O365-Box/engine/internal/approval"
	"github.com/Smohan-vcode/O365-Box/engine/internal/events"
)

// writeProposal saves the tenant's proposal (SPEC §25.1) and prints it by area.
func (s *session) writeProposal(out io.Writer, asJSON bool, runID string, changes []approval.Change, partial bool) error {
	approval.Sort(changes)
	p := &approval.Proposal{Tenant: s.doc.Config.Tenant.TenantID, Run: runID, Created: time.Now().UTC(), Changes: changes}
	path := filepath.Join(s.dir, approval.ProposalFile)
	if err := approval.WriteProposal(path, p); err != nil {
		return err
	}
	groups := approval.ByArea(s.areas, changes)
	if asJSON {
		var data []any
		for _, gr := range groups {
			data = append(data, map[string]any{"area": gr.Area, "title": gr.Title, "changes": len(gr.Changes)})
		}
		s.sink.Emit(events.Event{T: time.Now().UTC(), Run: runID, Ev: events.Log, Level: "proposal",
			Msg: fmt.Sprintf("proposal: %d change(s)", len(changes)), Data: map[string]any{"path": path, "areas": data, "partial": partial}})
		return nil
	}
	printChanges(out, s.areas, s.approvals, groups)
	fmt.Fprintf(out, "proposal: %d change(s) written to %s\n", len(changes), path)
	if partial {
		fmt.Fprintln(out, "  ! some steps failed to plan: their changes are missing from the proposal")
	}
	if len(changes) > 0 {
		fmt.Fprintln(out, "  approve with box approve --all, --area <id>, --step <id> or --change <digest>; reject with --reject <digest> --reason \"...\"")
	}
	return nil
}

func printChanges(out io.Writer, areas *approval.Areas, decided *approval.File, groups []approval.Group) {
	for _, gr := range groups {
		head := gr.Title
		if a := areas.Get(gr.Area); a != nil {
			head += " — " + a.Plain
		}
		fmt.Fprintf(out, "\n%s [%s]\n", head, gr.Area)
		for _, c := range gr.Changes {
			mark := "  "
			if decided != nil {
				switch decided.Decision(c.Digest) {
				case "approved":
					mark = "✓ "
				case "rejected":
					mark = "✗ "
				}
			}
			fmt.Fprintf(out, "  %s%s  (%s)  %s\n", mark, c.Digest[:12], c.Step, c.Text)
		}
	}
	fmt.Fprintln(out)
}

// newApproveCmd records the owner's approvals and rejections of the
// proposal's changes (SPEC §25.1).
func newApproveCmd(g *globals) *cobra.Command {
	var (
		configPath, statePath, reason      string
		all, yes                           bool
		areas, steps, changeIDs, rejectIDs []string
	)
	c := &cobra.Command{
		Use:   "approve",
		Short: "Approve the proposal's changes (all, an area, a step or one change), or reject one with a reason",
		Long: "box approve records the owner's decision on the changes in the tenant's proposal (box plan --proposal).\n" +
			"box apply and box up apply only approved changes. A rejected change becomes an exception with the\n" +
			"reason; the step holding it applies nothing. It asks you to type \"approve\" (or \"reject\").\n" +
			"--yes skips that question: it is for scripts the owner runs themselves, never for anyone else.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			root, err := g.root()
			if err != nil {
				return err
			}
			doc, err := loadValidConfig(root, configPath, out)
			if err != nil {
				return err
			}
			rejecting := len(rejectIDs) > 0
			selecting := all || len(areas) > 0 || len(steps) > 0 || len(changeIDs) > 0
			switch {
			case rejecting && selecting:
				return usageError("approve and reject separately: --reject cannot be combined with --all, --area, --step or --change")
			case !rejecting && !selecting:
				return usageError("say what to approve: --all, --area <id>, --step <id> or --change <digest>; or --reject <digest> --reason \"...\"")
			case rejecting && strings.TrimSpace(reason) == "":
				return usageError("--reject needs --reason: the reason is kept with the exception")
			case !rejecting && reason != "":
				return usageError("--reason goes with --reject")
			}
			tenant := doc.Config.Tenant.TenantID
			dir, err := approval.Dir(tenant, statePath)
			if err != nil {
				return err
			}
			p, err := approval.ReadProposal(filepath.Join(dir, approval.ProposalFile))
			if err != nil {
				if err == approval.ErrNoProposal {
					return usageError(err.Error())
				}
				return err
			}
			if p.Tenant != tenant {
				return usageError("the proposal is for another tenant: run box plan --proposal for this configuration")
			}
			ar, err := approval.LoadAreas(filepath.Join(root, "defaults", "areas.yaml"))
			if err != nil {
				return err
			}
			for _, a := range areas {
				if ar.Get(a) == nil && a != approval.Other {
					return usageError(fmt.Sprintf("no area %q (see defaults/areas.yaml)", a))
				}
			}
			for _, st := range steps {
				if !slices.ContainsFunc(p.Changes, func(c approval.Change) bool { return c.Step == st }) {
					return usageError(fmt.Sprintf("the proposal has no change for step %q", st))
				}
			}
			byPrefix := func(ids []string) ([]approval.Change, error) {
				var sel []approval.Change
				for _, id := range ids {
					id = strings.ToLower(strings.TrimSpace(id))
					if len(id) < 6 {
						return nil, usageError(fmt.Sprintf("digest %q: give at least 6 characters", id))
					}
					var hit []approval.Change
					for _, c := range p.Changes {
						if strings.HasPrefix(c.Digest, id) {
							hit = append(hit, c)
						}
					}
					switch len(hit) {
					case 0:
						return nil, usageError(fmt.Sprintf("no change %s in the proposal", id))
					case 1:
						sel = append(sel, hit[0])
					default:
						return nil, usageError(fmt.Sprintf("digest %s matches %d changes: give more characters", id, len(hit)))
					}
				}
				return sel, nil
			}
			file, err := approval.Open(filepath.Join(dir, approval.ApprovalsFile))
			if err != nil {
				return err
			}
			var sel []approval.Change
			if rejecting {
				if sel, err = byPrefix(rejectIDs); err != nil {
					return err
				}
			} else {
				picked, err := byPrefix(changeIDs)
				if err != nil {
					return err
				}
				kept := 0
				for _, c := range p.Changes {
					explicit := slices.ContainsFunc(picked, func(x approval.Change) bool { return x.Digest == c.Digest })
					if !explicit && !all && !slices.Contains(areas, c.Area) && !slices.Contains(steps, c.Step) {
						continue
					}
					switch file.Decision(c.Digest) {
					case "approved": // already approved and not applied yet
						continue
					case "rejected":
						// Only naming the change itself lifts an exception.
						if !explicit {
							kept++
							continue
						}
					}
					sel = append(sel, c)
				}
				if kept > 0 {
					fmt.Fprintf(out, "  %d rejected change(s) stay exceptions; approve one with --change <digest> to lift it\n", kept)
				}
			}
			if len(sel) == 0 {
				fmt.Fprintln(out, "nothing to approve: the selected changes are already decided")
				return nil
			}
			word := "approve"
			if rejecting {
				word = "reject"
			}
			if !g.asJSON || !yes {
				printChanges(out, ar, file, approval.ByArea(ar, sel))
			}
			if !yes {
				fmt.Fprintf(out, "Type %q to %s these %d change(s): ", word, word, len(sel))
				line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if strings.TrimSpace(line) != word {
					fmt.Fprintln(out)
					return usageError("not confirmed: nothing recorded")
				}
			}
			who, at := approval.Who(), time.Now().UTC()
			digests := make([]any, len(sel))
			for i, c := range sel {
				digests[i] = c.Digest
			}
			item := &approval.EventItem{At: at, Actor: who, Entity: "proposal", EntityID: p.Run,
				Detail: map[string]any{"tenant": tenant, "digests": digests, "count": len(sel), "scope": scope(all, areas, steps, changeIDs, rejectIDs)}}
			if rejecting {
				if err := file.Reject(sel, reason, who, p.Run, at); err != nil {
					return err
				}
				item.Action, item.FromState, item.ToState = "rejection", "proposed", "exception"
				item.Detail["reason"] = reason
			} else {
				if _, err := file.Approve(sel, who, p.Run, at); err != nil {
					return err
				}
				item.Action, item.FromState, item.ToState = "approval", "proposed", "approved"
			}
			if err := approval.AppendEvent(filepath.Join(dir, approval.EventsFile), item); err != nil {
				return err
			}
			ev := events.Event{T: at, Run: p.Run, Ev: events.Log, Level: item.Action,
				Msg:  fmt.Sprintf("%s: %d change(s) by %s", item.Action, len(sel), who),
				Data: map[string]any{"event_id": item.EventID, "by": who, "digests": digests}}
			if rejecting {
				ev.Data["reason"] = reason
			}
			if g.asJSON {
				events.NewJSONLines(out).Emit(ev)
				return nil
			}
			if rejecting {
				fmt.Fprintf(out, "✓ rejected %d change(s): recorded as exceptions (%s)\n", len(sel), item.EventID)
			} else {
				fmt.Fprintf(out, "✓ approved %d change(s) by %s at %s (%s)\n", len(sel), who, at.Format(time.RFC3339), item.EventID)
			}
			return nil
		}}
	c.Flags().StringVar(&configPath, "config", "", "configuration document")
	c.Flags().StringVar(&statePath, "state", "", "state cache file; the proposal and approvals sit next to it (default: user config dir, per tenant)")
	c.Flags().BoolVar(&all, "all", false, "approve every change in the proposal")
	c.Flags().StringSliceVar(&areas, "area", nil, "approve every change in these areas (defaults/areas.yaml)")
	c.Flags().StringSliceVar(&steps, "step", nil, "approve every change of these steps")
	c.Flags().StringSliceVar(&changeIDs, "change", nil, "approve these changes (digest, or a unique prefix of at least 6 characters)")
	c.Flags().StringSliceVar(&rejectIDs, "reject", nil, "reject these changes (digest prefix): each becomes an exception")
	c.Flags().StringVar(&reason, "reason", "", "why the changes are rejected (required with --reject)")
	c.Flags().BoolVar(&yes, "yes", false, "do not ask to type approve; for scripts the owner runs themselves")
	return c
}

func scope(all bool, areas, steps, changes, rejects []string) string {
	var parts []string
	if all {
		parts = append(parts, "all")
	}
	for _, x := range areas {
		parts = append(parts, "area:"+x)
	}
	for _, x := range steps {
		parts = append(parts, "step:"+x)
	}
	for _, x := range changes {
		parts = append(parts, "change:"+x)
	}
	for _, x := range rejects {
		parts = append(parts, "reject:"+x)
	}
	return strings.Join(parts, ",")
}
