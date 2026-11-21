package cli

// box tasks: the recurring control tasks (F17, T10-02). schedule shows
// what the documents and the control set ask for; list reads the tenant's
// ControlTasks; done records that the assignee completed one, with its
// output. The box never marks a task done on its own (SPEC §25).

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/Smohan-vcode/O365-Box/engine/internal/config"
	"github.com/Smohan-vcode/O365-Box/engine/internal/docs"
)

// ControlTasksStep is the step that keeps the ControlTasks list.
const ControlTasksStep = "sharepoint.control-tasks"

// maxOutputFile bounds a task output uploaded from a file.
const maxOutputFile = 50 << 20

// controlTaskInput is the step's "@controltasks" input: today's date and
// every scheduled kind with its current and next period. Problems in the
// documents or the control set are an error: the step does not guess.
func controlTaskInput(root string, now time.Time) (map[string]any, error) {
	kinds, problems, err := docs.LoadSchedule(root)
	if err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("%d problem(s) in the control task schedule, first: %s", len(problems), problems[0])
	}
	due, err := docs.DueOn(kinds, now)
	if err != nil {
		return nil, err
	}
	return map[string]any{"today": now.Format("2006-01-02"), "kinds": due}, nil
}

func newTasksCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "tasks", Short: "Recurring control tasks: schedule, status and completion (T10-02)"}
	cmd.AddCommand(newTasksScheduleCmd(g), newTasksListCmd(g), newTasksDoneCmd(g))
	return cmd
}

func newTasksScheduleCmd(g *globals) *cobra.Command {
	return &cobra.Command{Use: "schedule", Short: "Show each control task kind, its frequency, owner role and next due date (no tenant)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := g.root()
			if err != nil {
				return err
			}
			kinds, problems, err := docs.LoadSchedule(root)
			if err != nil {
				return err
			}
			due, err := docs.DueOn(kinds, time.Now().UTC())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if g.asJSON {
				return writeJSON(out, map[string]any{"kinds": kinds, "due": due, "problems": problems})
			}
			byKind := map[string]docs.Due{}
			for _, d := range due {
				byKind[d.Kind] = d
			}
			tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "KIND\tCONTROL\tFREQUENCY\tOWNER ROLE\tDUE\tREQUIREMENTS\tPREPARES")
			for _, k := range kinds {
				d := "on event"
				if x, ok := byKind[k.Kind]; ok {
					d = x.Current.DueOn + " (" + x.Current.ID + ")"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", k.Kind, k.ControlID, k.Frequency, k.OwnerRole, d, strings.Join(k.Requirements, ","), k.Prepares)
			}
			tw.Flush()
			for _, p := range problems {
				fmt.Fprintln(out, "  ✗ "+p)
			}
			if len(problems) > 0 {
				return validationError(fmt.Sprintf("%d problem(s) in the control task schedule", len(problems)))
			}
			return nil
		}}
}

// taskItem is one ControlTasks item as the step's read operation returns it.
type taskItem struct {
	TaskID       string `json:"task_id"`
	Title        string `json:"title"`
	ControlID    string `json:"control_id"`
	DueOn        string `json:"due_on"`
	State        string `json:"state"`
	AssigneeRole string `json:"assignee_role"`
	OutputURL    string `json:"output_url"`
	PreparedURL  string `json:"prepared_url"`
	ClosedAt     string `json:"closed_at"`
}

// kindOf returns the kind of a task id <kind>-<period>, given the kinds.
func kindOf(taskID string, kinds []docs.TaskKind) string {
	best := ""
	for _, k := range kinds {
		if strings.HasPrefix(taskID, k.Kind+"-") && len(k.Kind) > len(best) {
			best = k.Kind
		}
	}
	return best
}

// taskRow is one kind's status for box tasks list.
type taskRow struct {
	Kind      string    `json:"kind"`
	Frequency string    `json:"frequency"`
	OwnerRole string    `json:"owner_role"`
	Status    string    `json:"status"` // due | overdue | done | not created | on event
	Open      *taskItem `json:"open,omitempty"`
	LastDone  *taskItem `json:"last_done,omitempty"`
	Tasks     int       `json:"tasks"`
}

// summarise gives each kind its status from the tenant's items: the oldest
// task not done (overdue when its state says so or its date has passed),
// else the latest done one.
func summarise(kinds []docs.TaskKind, items []taskItem, today string) []taskRow {
	by := map[string][]taskItem{}
	for _, it := range items {
		if k := kindOf(it.TaskID, kinds); k != "" {
			by[k] = append(by[k], it)
		}
	}
	var out []taskRow
	for _, k := range kinds {
		r := taskRow{Kind: k.Kind, Frequency: k.Frequency, OwnerRole: k.OwnerRole, Status: "not created"}
		list := by[k.Kind]
		sort.Slice(list, func(i, j int) bool { return list[i].DueOn < list[j].DueOn })
		r.Tasks = len(list)
		for i := range list {
			it := list[i]
			if it.State == "DONE" {
				r.LastDone = &it
			} else if r.Open == nil {
				r.Open = &it
			}
		}
		switch {
		case r.Open != nil && (r.Open.State == "OVERDUE" || (r.Open.DueOn != "" && r.Open.DueOn < today)):
			r.Status = "overdue"
		case r.Open != nil:
			r.Status = "due"
		case r.LastDone != nil:
			r.Status = "done"
		case !k.Scheduled():
			r.Status = "on event"
		}
		out = append(out, r)
	}
	return out
}

// tasksSession opens the tenant session for box tasks and returns the
// step's input.
func tasksSession(g *globals, f *runFlags, out io.Writer) (*session, map[string]any, error) {
	s, err := g.open(f, out, false)
	if err != nil {
		return nil, nil, err
	}
	step := s.catalog.Steps[ControlTasksStep]
	if step == nil {
		s.pool.Close()
		return nil, nil, fmt.Errorf("no step %s in the catalogue", ControlTasksStep)
	}
	if !s.doc.Config.Modules.Map()[step.Module] {
		s.pool.Close()
		return nil, nil, usageError(fmt.Sprintf("module %s is off: there are no control tasks", step.Module))
	}
	if s.pipeline == nil {
		s.pool.Close()
		return nil, nil, usageError("this tenant has no pipeline identity yet: run box bootstrap first")
	}
	in, _ := s.input(step).(map[string]any)
	if e, ok := in["control_tasks_error"].(string); ok {
		s.pool.Close()
		return nil, nil, validationError(e)
	}
	return s, in, nil
}

func readTasks(ctx context.Context, s *session, in map[string]any) ([]taskItem, error) {
	raw, err := s.pool.Invoke(ctx, s.catalog.Steps[ControlTasksStep].Provider, ControlTasksStep, "read", in, nil)
	if err != nil {
		return nil, err
	}
	var res struct {
		Items []taskItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("read ControlTasks: %w", err)
	}
	return res.Items, nil
}

func newTasksListCmd(g *globals) *cobra.Command {
	f := &runFlags{}
	c := &cobra.Command{Use: "list", Short: "Show each control task kind as due, overdue or done on the tenant", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			s, in, err := tasksSession(g, f, out)
			if err != nil {
				return err
			}
			defer s.pool.Close()
			ctx, cancel := signalContext()
			defer cancel()
			items, err := readTasks(ctx, s, in)
			if err != nil {
				return err
			}
			kinds, _, err := docs.LoadSchedule(s.root)
			if err != nil {
				return err
			}
			rows := summarise(kinds, items, in["control_tasks"].(map[string]any)["today"].(string))
			if g.asJSON {
				return writeJSON(out, map[string]any{"kinds": rows, "items": items})
			}
			tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "KIND\tFREQUENCY\tOWNER ROLE\tSTATUS\tTASK\tDUE\tLAST DONE\tPREPARED")
			overdue := 0
			for _, r := range rows {
				task, due, last, prep := "", "", "", ""
				if r.Open != nil {
					task, due = r.Open.TaskID, r.Open.DueOn
					if r.Open.PreparedURL != "" {
						prep = "yes"
					}
				}
				if r.LastDone != nil {
					last = r.LastDone.ClosedAt
					if last == "" {
						last = r.LastDone.TaskID
					}
				}
				if r.Status == "overdue" {
					overdue++
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.Kind, r.Frequency, r.OwnerRole, r.Status, task, due, last, prep)
			}
			tw.Flush()
			if overdue > 0 {
				fmt.Fprintf(out, "%d kind(s) overdue: do the task, then box tasks done <kind> --output <url or file> --by <user id>\n", overdue)
			}
			return nil
		}}
	f.bind(c, false)
	return c
}

func newTasksDoneCmd(g *globals) *cobra.Command {
	f := &runFlags{}
	var output, taskID, by string
	var usePrepared bool
	c := &cobra.Command{Use: "done <kind>", Short: "Record that you completed a control task, with its output", Args: cobra.ExactArgs(1),
		Long: "Records completion of the oldest open task of <kind> (or --task): state DONE, the completed date and the output.\n" +
			"The output is a URL or a local file (uploaded to the Compliance site's Evidence library), or --use-prepared to\n" +
			"confirm the review sheet the box prepared. --by names you (a user id in the configuration) and you must hold the\n" +
			"task's owner role. One Evidence item and one Events item are written. A completed task is never changed.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			kind := args[0]
			if (output == "") == !usePrepared {
				return usageError("give the task's output: --output <url or file>, or --use-prepared")
			}
			s, in, err := tasksSession(g, f, out)
			if err != nil {
				return err
			}
			defer s.pool.Close()
			if s.doc.Config.Tenant.Environment == "live" && !f.live {
				return usageError("this configuration targets a live tenant: pass --live to record a completion there")
			}
			kinds, _, err := docs.LoadSchedule(s.root)
			if err != nil {
				return err
			}
			var k *docs.TaskKind
			for i := range kinds {
				if kinds[i].Kind == kind {
					k = &kinds[i]
				}
			}
			if k == nil {
				names := make([]string, len(kinds))
				for i, x := range kinds {
					names[i] = x.Kind
				}
				return usageError(fmt.Sprintf("no control task kind %q (kinds: %s)", kind, strings.Join(names, ", ")))
			}
			if err := mayClose(s.doc.Config.Users, by, k.OwnerRole); err != nil {
				return err
			}
			ctx, cancel := signalContext()
			defer cancel()
			items, err := readTasks(ctx, s, in)
			if err != nil {
				return err
			}
			task, err := pickTask(items, kinds, kind, taskID)
			if err != nil {
				return err
			}
			closeReq := map[string]any{"task_id": task.TaskID, "kind": kind, "by": by}
			what := ""
			switch {
			case usePrepared:
				if task.PreparedURL == "" {
					return usageError(task.TaskID + " has no prepared review sheet; give --output")
				}
				closeReq["output_url"], what = task.PreparedURL, "the prepared review sheet "+task.PreparedURL
			case strings.HasPrefix(output, "https://"):
				closeReq["output_url"], what = output, output
			case strings.Contains(output, "://"):
				return usageError("the output URL must start with https://")
			default:
				st, err := os.Stat(output)
				if err != nil {
					return usageError("the output is neither an https URL nor a readable file: " + err.Error())
				}
				if st.IsDir() || st.Size() == 0 || st.Size() > maxOutputFile {
					return usageError("the output file must be a non-empty file of at most 50 MB")
				}
				b, err := os.ReadFile(output)
				if err != nil {
					return err
				}
				closeReq["file_name"], closeReq["file_b64"] = filepath.Base(output), base64.StdEncoding.EncodeToString(b)
				what = "file " + filepath.Base(output) + " (uploaded to Compliance/Evidence/Control tasks/" + task.TaskID + ")"
			}
			fmt.Fprintf(out, "%s  %s\n  due %s, state %s, owner role %s\n  output: %s\n  done by: %s\n", task.TaskID, task.Title, task.DueOn, task.State, k.OwnerRole, what, by)
			if !f.yes {
				fmt.Fprint(out, `Type "done" to record this task as DONE: `)
				line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if strings.TrimSpace(line) != "done" {
					fmt.Fprintln(out)
					return usageError("not confirmed: nothing recorded")
				}
			}
			in["close"] = closeReq
			raw, err := s.pool.Invoke(ctx, s.catalog.Steps[ControlTasksStep].Provider, ControlTasksStep, "record", in, nil)
			if err != nil {
				return err
			}
			var res map[string]any
			if err := json.Unmarshal(raw, &res); err != nil {
				return err
			}
			if g.asJSON {
				return writeJSON(out, res)
			}
			fmt.Fprintf(out, "✓ %s DONE; evidence %v; output %v\n", task.TaskID, res["evidence_id"], res["output_url"])
			if b, _ := res["closed_by_person"].(bool); !b {
				fmt.Fprintln(out, "  (no People item for "+by+": closed_by is empty; the Evidence and Events items name the person)")
			}
			return nil
		}}
	f.bind(c, false)
	c.Flags().StringVar(&output, "output", "", "the task's output: an https URL, or a local file to upload")
	c.Flags().BoolVar(&usePrepared, "use-prepared", false, "confirm the review sheet the box prepared as the output")
	c.Flags().StringVar(&taskID, "task", "", "the task id (default: the oldest open task of the kind)")
	c.Flags().StringVar(&by, "by", "", "your user id in the configuration (you must hold the task's owner role)")
	_ = c.MarkFlagRequired("by")
	return c
}

// mayClose checks that the user exists and holds the task's owner role,
// or the owner role.
func mayClose(users []config.User, id, role string) error {
	for _, u := range users {
		if u.ID != id {
			continue
		}
		if u.BreakGlass {
			return usageError(id + " is an emergency access account; it does not close tasks")
		}
		if slices.Contains(u.Roles, role) || slices.Contains(u.Roles, "owner") {
			return nil
		}
		return usageError(fmt.Sprintf("%s does not hold the role %s that this task is assigned to", id, role))
	}
	return usageError("no user " + id + " in the configuration")
}

// pickTask returns the task to close: the one named, or the oldest of the
// kind that is not done.
func pickTask(items []taskItem, kinds []docs.TaskKind, kind, id string) (*taskItem, error) {
	var open []taskItem
	for _, it := range items {
		if kindOf(it.TaskID, kinds) != kind {
			continue
		}
		if id != "" && it.TaskID == id {
			if it.State == "DONE" {
				return nil, usageError(id + " is already DONE; a completed task is never changed")
			}
			return &it, nil
		}
		if it.State != "DONE" {
			open = append(open, it)
		}
	}
	if id != "" {
		return nil, usageError("no task " + id + " of kind " + kind)
	}
	if len(open) == 0 {
		return nil, usageError("no open " + kind + " task: run box apply --only " + ControlTasksStep + " to create the next one")
	}
	sort.Slice(open, func(i, j int) bool { return open[i].DueOn < open[j].DueOn })
	return &open[0], nil
}
