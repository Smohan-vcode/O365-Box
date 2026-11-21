package cli

// box extensions report (T14-20): the Edge extensions in use on company
// devices, with the number of devices and what the tenant's Edge policy
// does with each. It only reads: the live Edge policies (Intune settings
// catalogue, DeviceManagementConfiguration in the devices bundle) and the
// installed extensions (Defender Vulnerability Management's inventory over
// Advanced hunting). When a read is refused the report says what it needs
// and proposes it; it never grants anything.

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/Smohan-vcode/O365-Box/engine/internal/extensions"
	"github.com/Smohan-vcode/O365-Box/engine/internal/graph"
	"github.com/Smohan-vcode/O365-Box/engine/internal/policy"
)

// extReport is the report as written to JSON.
type extReport struct {
	ReadAt    time.Time             `json:"read_at"`
	Windows   extensions.Live       `json:"windows"`
	MacOS     extensions.Live       `json:"macos"`
	Inventory extensions.Inventory  `json:"inventory"`
	Rows      []extensions.Row      `json:"rows"`
	Notes     []string              `json:"notes,omitempty"`
	Proposed  []extensions.Proposal `json:"proposed,omitempty"`
}

// extensionNotes says where the configuration's Edge extension settings
// apply, from the resolved settings by step.
func extensionNotes(byStep map[string][]policy.StepSetting, choice string) []string {
	has := func(step, id string) bool {
		for _, s := range byStep[step] {
			if s.ID == id {
				return true
			}
		}
		return false
	}
	var notes []string
	switch {
	case choice == "store":
		notes = append(notes, "The choice devices.browser_extensions is \"store\": any extension from the Microsoft Edge store is allowed.")
	case !has("policy.intune-windows", "WIN-069"):
		notes = append(notes, "Windows: the extension block and allow lists (WIN-069) are not in this company's settings (they are in the enhanced tier), so Edge on Windows does not block extensions.")
	}
	if choice != "store" && !has("policy.intune-macos", "MAC-030") {
		notes = append(notes, "macOS: the Edge settings (MAC-030) are not in this company's settings.")
	}
	return notes
}

func newExtReportCmd(g *globals) *cobra.Command {
	f := &runFlags{}
	var outDir string
	c := &cobra.Command{Use: "report", Short: "Report the Edge extensions in use on company devices and what the Edge policy does with each (read-only)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			s, d, ctx, done, err := extSession(g, f, out)
			if err != nil {
				return err
			}
			defer done()
			if outDir != "" {
				if rel, inside := relInside(s.root, outDir); inside {
					return usageError("--out must be outside the repository (the report describes one company): " + rel)
				}
			}
			tok, err := s.graphToken(ctx)
			if err != nil {
				return err
			}
			gc := &graph.Client{Token: func() string { return tok }}
			beta := strings.Replace(graph.Base, "/v1.0", "/beta", 1)
			rep := extReport{ReadAt: time.Now().UTC()}
			rep.Windows = extensions.ReadLive(ctx, gc, beta, "Windows", extensions.WindowsPolicy)
			rep.MacOS = extensions.ReadLive(ctx, gc, beta, "macOS", extensions.MacPolicy)
			rep.Inventory = extensions.ReadInventory(ctx, gc, graph.Base)
			lists := extensions.Lists{Allow: s.doc.Config.Devices.Browser.ExtensionAllowlist, Forced: s.doc.Config.Devices.Browser.ForcedExtensions}
			names := map[string]string{}
			if rs, err := d.reg.Requests(ctx); err == nil {
				for _, r := range rs {
					if r.State == extensions.Approved {
						names[r.ExtensionID] = r.Label()
					}
				}
			}
			rep.Rows = extensions.Rows(rep.Inventory, lists, names, rep.Windows, rep.MacOS)
			if sel, err := selection(s.root, &s.doc.Config); err == nil {
				if p, err := s.policyInput(); err == nil {
					if bs, ok := p["by_step"].(map[string][]policy.StepSetting); ok {
						rep.Notes = extensionNotes(bs, sel.Choices["devices.browser_extensions"])
					}
				}
			}
			if !rep.Inventory.Read {
				rep.Proposed = extensions.InventoryProposal()
			}
			if outDir != "" {
				if err := writeExtReport(outDir, &rep); err != nil {
					return err
				}
			}
			if g.asJSON {
				return writeJSON(out, rep)
			}
			printExtReport(out, &rep)
			if outDir != "" {
				fmt.Fprintf(out, "written: %s\n", filepath.Join(outDir, "extensions-in-use.csv"))
			}
			return nil
		}}
	f.bind(c, false)
	c.Flags().StringVar(&outDir, "out", "", "also write extensions-in-use.csv and .json into this directory (outside the repository)")
	return c
}

func printExtReport(out io.Writer, rep *extReport) {
	fmt.Fprintln(out, "Edge extension policy in the tenant")
	for _, l := range []extensions.Live{rep.Windows, rep.MacOS} {
		fmt.Fprintln(out, "  "+l.Summary())
	}
	for _, n := range rep.Notes {
		fmt.Fprintln(out, "  ! "+n)
	}
	fmt.Fprintln(out)
	if rep.Inventory.Read {
		fmt.Fprintf(out, "Extensions in use: %d found on devices\n", len(rep.Inventory.Items))
	} else {
		fmt.Fprintln(out, "Extensions in use: not read")
		fmt.Fprintln(out, "  "+rep.Inventory.Reason)
	}
	if len(rep.Rows) == 0 {
		fmt.Fprintln(out, "  no extensions found or approved")
	} else {
		tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "EXTENSION ID\tNAME\tDEVICES\tWINDOWS\tMACOS\tCONFIGURATION")
		for _, r := range rep.Rows {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", r.ID, oneLine(r.Name, 50), r.Devices, r.Windows, r.MacOS, r.Configuration)
		}
		tw.Flush()
	}
	for _, p := range rep.Proposed {
		fmt.Fprintf(out, "\nproposed, not granted: %s\n  why: %s\n  also needs: %s\n", p.Permission, p.Why, p.Needs)
	}
}

func writeExtReport(dir string, rep *extReport) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "extensions-in-use.json"), b, 0o600); err != nil {
		return err
	}
	fh, err := os.OpenFile(filepath.Join(dir, "extensions-in-use.csv"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	w := csv.NewWriter(fh)
	_ = w.Write([]string{"extension_id", "name", "devices", "windows", "macos", "configuration"})
	for _, r := range rep.Rows {
		_ = w.Write([]string{r.ID, r.Name, r.Devices, r.Windows, r.MacOS, r.Configuration})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		fh.Close()
		return err
	}
	return fh.Close()
}
