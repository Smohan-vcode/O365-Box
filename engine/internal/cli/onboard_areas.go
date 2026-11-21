package cli

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/Smohan-vcode/O365-Box/engine/internal/choices"
	"github.com/Smohan-vcode/O365-Box/engine/internal/policy"
	"github.com/Smohan-vcode/O365-Box/engine/internal/tui"
)

// choiceAreas places the choices that change no catalogue setting.
var choiceAreas = map[string]string{
	"directory.mode":     "sign-in",
	"azure.subscription": "cloud",
	"domain.registrar":   "web-address",
}

// areaInfo counts, per area, the catalogue settings the owner's answers
// select (tier, modules and the choices that leave settings out), and names
// the choices behind each area with their cost notes: the Review screen's
// numbers (SPEC §25.3). Licence capabilities are not applied yet, so the
// count is an upper bound until the plan runs.
func areaInfo(root string, areas []tui.Area, cat *choices.Catalogue) func(tui.Selection) map[string]tui.AreaInfo {
	pc, err := policy.Load(filepath.Join(root, "policy"), "policy")
	if err != nil {
		return nil
	}
	return func(s tui.Selection) map[string]tui.AreaInfo {
		r, err := cat.Resolve(s.Choices, choices.Context{Modules: s.Modules})
		if err != nil {
			return nil
		}
		out := map[string]tui.AreaInfo{}
		centreArea := map[string]string{}
		for _, a := range areas {
			for _, st := range a.Steps {
				if c, ok := strings.CutPrefix(st, "policy."); ok {
					centreArea[strings.SplitN(c, ".", 2)[0]] = a.ID
				}
			}
		}
		for _, c := range pc.Centres {
			id, ok := centreArea[c.ID]
			if !ok {
				continue
			}
			info := out[id]
			for _, st := range c.Settings {
				if st.Tier == policy.TierAddon || (st.Tier == policy.TierEnhanced && s.Tier != policy.TierEnhanced) ||
					r.LeftOut[st.ID] != "" || (strings.HasPrefix(c.ID, "intune-") && !s.Modules["devices"]) {
					continue
				}
				info.Settings++
				info.Details = append(info.Details, st.ID+" "+st.Name)
			}
			out[id] = info
		}
		var costs = map[string][]string{}
		ids := make([]string, 0, len(r.Answers))
		for id := range r.Answers {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			ch := cat.Choice(id)
			o := ch.Option(r.Answers[id])
			if o == nil {
				continue
			}
			area := choiceAreas[id]
			touched := append([]string{}, o.Effects.Exceptions...)
			for st := range o.Effects.Variants {
				touched = append(touched, st)
			}
			for _, st := range touched {
				if set := pc.Setting(st); set != nil && centreArea[set.Centre] != "" {
					area = centreArea[set.Centre]
					break
				}
			}
			if area == "" {
				continue
			}
			info := out[area]
			info.Lines = append(info.Lines, strings.TrimSuffix(ch.Question, "?")+": "+o.Label)
			out[area] = info
			if o.Cost != "" && o.Cost != "none" {
				costs[area] = append(costs[area], o.Cost)
			}
		}
		for area, cs := range costs {
			info := out[area]
			info.Cost = strings.Join(cs, "; ") + "."
			out[area] = info
		}
		if !s.Modules["devices"] {
			out["devices"] = tui.AreaInfo{Off: "you do not run company computers and phones. Switch it on under What you run."}
		}
		if v := r.Answers["azure.subscription"]; v == "" || v == "none" {
			out["cloud"] = tui.AreaInfo{Off: "you do not use Microsoft Azure. Switch it on under What you run."}
		}
		return out
	}
}
