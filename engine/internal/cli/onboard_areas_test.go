package cli

import (
	"testing"

	"github.com/Smohan-vcode/O365-Box/engine/internal/choices"
	"github.com/Smohan-vcode/O365-Box/engine/internal/tui"
)

func TestAreaInfoFromCatalogue(t *testing.T) {
	root := "../../.."
	cat, err := choices.Load(root + "/defaults/choices.yaml")
	if err != nil {
		t.Fatal(err)
	}
	areas, err := tui.LoadAreas(root + "/defaults/areas.yaml")
	if err != nil || len(areas) != 9 {
		t.Fatalf("areas: %d %v", len(areas), err)
	}
	info := areaInfo(root, areas, cat)
	if info == nil {
		t.Fatal("no policy catalogue")
	}
	mods := map[string]bool{"devices": true, "records": true, "people": true, "policies": true}
	tier, ans, _ := cat.DialAnswers("recommended", choices.Context{Modules: mods})
	core := info(tui.Selection{Tier: tier, Modules: mods, Choices: ans})
	tier, ans, _ = cat.DialAnswers("strict", choices.Context{Modules: mods})
	strict := info(tui.Selection{Tier: tier, Modules: mods, Choices: ans})
	for _, id := range []string{"sign-in", "email", "files", "meetings", "devices", "sensitive-info", "apps"} {
		if core[id].Settings == 0 || strict[id].Settings < core[id].Settings {
			t.Errorf("%s: core %d strict %d", id, core[id].Settings, strict[id].Settings)
		}
	}
	if core["cloud"].Off == "" || core["devices"].Off != "" || len(core["sign-in"].Lines) == 0 {
		t.Errorf("cloud %+v devices off %q sign-in lines %v", core["cloud"], core["devices"].Off, core["sign-in"].Lines)
	}
	noDev := map[string]bool{"records": true, "policies": true}
	off := info(tui.Selection{Tier: "core", Modules: noDev, Choices: map[string]string{"azure.subscription": "create"}})
	if off["devices"].Off == "" || off["cloud"].Off != "" || off["cloud"].Cost == "" {
		t.Errorf("devices %+v cloud %+v", off["devices"], off["cloud"])
	}
}
