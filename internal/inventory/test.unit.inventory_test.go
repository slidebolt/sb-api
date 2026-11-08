package inventory

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	storage "github.com/slidebolt/sb-storage-sdk"
)

func entry(key, data string) storage.Entry {
	return storage.Entry{Key: key, Data: json.RawMessage(data)}
}

var home = []storage.Entry{
	entry("plugin-wiz.lamp1.light", `{"plugin":"plugin-wiz","type":"light","name":"lamp1","profile":{"name":"Kitchen Lamp"},
		"labels":{"Area":["Ground Floor"],"Room":["Kitchen"],"PluginAutomation":["KitchenLights"]},
		"state":{"power":true,"brightness":200},"commands":["light_turn_on","light_turn_off","light_set_brightness"]}`),
	entry("plugin-wiz.lamp2.light", `{"plugin":"plugin-wiz","type":"light","name":"Bedroom Lamp",
		"labels":{"Area":["Upstairs"],"Room":["Bedroom"]},"state":{"power":false},"commands":["light_turn_on","light_turn_off"]}`),
	entry("plugin-wiz.lamp3.light", `{"plugin":"plugin-wiz","type":"light","name":"Bath Lamp",
		"labels":{"Area":["Upstairs"],"Room":["Bedroom Bath"]},"commands":["light_turn_on","light_turn_off"]}`),
	entry("plugin-kasa.plug1.switch", `{"plugin":"plugin-kasa","type":"switch","name":"Porch Plug","commands":["switch_turn_on","switch_turn_off"]}`),
	entry("plugin-temp.sensor1.temperature", `{"plugin":"plugin-temp","type":"sensor","name":"Hall Temperature","labels":{"Area":["Ground Floor"]},"state":{"value":21.5}}`),
	entry("plugin-automation.group.kitchenlights", `{"plugin":"plugin-automation","type":"light","name":"KitchenLights",
		"profile":{"name":"Kitchen Lights"},"commands":["light_turn_on","light_turn_off","script_run"]}`),
	entry("sb-query.queries.bedroom_lights", `{"type":"query","name":"Bedroom Lights",
		"query":{"pattern":">","where":[{"field":"type","op":"eq","value":"light"},{"field":"labels.Room","op":"eq","value":"Bedroom"}]}}`),
}

var fixed = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func section(t *testing.T, text, name string) string {
	t.Helper()
	start := strings.Index(text, "## "+name)
	if start < 0 {
		t.Fatalf("no %s section in:\n%s", name, text)
	}
	rest := text[start+3:]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

func TestRender_HeaderCountsAndScope(t *testing.T) {
	out := Render(home, Options{Now: fixed})
	if !strings.Contains(out, "generated 2026-01-02T03:04:05Z; 7 entities, 5 listed (scope=controllable)") {
		t.Fatalf("header wrong:\n%s", out)
	}
	if strings.Contains(out, "plugin-temp.sensor1") {
		t.Fatal("a sensor with no commands is listed under the controllable scope")
	}
	all := Render(home, Options{Now: fixed, Scope: ScopeAll})
	if !strings.Contains(all, "plugin-temp.sensor1.temperature\tsensor\tHall Temperature\tGround Floor") {
		t.Fatalf("scope=all should list the sensor:\n%s", all)
	}
}

func TestRender_EntityRows(t *testing.T) {
	ents := section(t, Render(home, Options{Now: fixed}), "ENTITIES")
	lines := strings.Split(strings.TrimSpace(ents), "\n")
	if lines[1] != "key\ttype\tname\tarea\troom\tlabels\tcommands" {
		t.Fatalf("header row = %q", lines[1])
	}
	want := "plugin-wiz.lamp1.light\tlight\tKitchen Lamp\tGround Floor\tKitchen\tPluginAutomation=KitchenLights\tlight: turn_on turn_off set_brightness"
	if !strings.Contains(ents, want+"\n") {
		t.Fatalf("missing row %q in:\n%s", want, ents)
	}
	if strings.Contains(ents, "plugin-automation.group.") {
		t.Fatal("groups belong in GROUPS, not in the entity rows")
	}
	// Rows are ordered by area, then room, with unplaced entities last.
	if strings.Index(ents, "lamp1") > strings.Index(ents, "lamp2") || strings.Index(ents, "lamp3") > strings.Index(ents, "plug1") {
		t.Fatalf("rows not ordered by place:\n%s", ents)
	}
}

func TestRender_StateColumn(t *testing.T) {
	ents := section(t, Render(home, Options{Now: fixed, State: true}), "ENTITIES")
	if !strings.Contains(ents, "\tstate\n") || !strings.Contains(ents, "set_brightness\ton brightness=200\n") {
		t.Fatalf("state column wrong:\n%s", ents)
	}
}

func TestRender_Places(t *testing.T) {
	places := section(t, Render(home, Options{Now: fixed}), "PLACES")
	for _, want := range []string{"Ground Floor (1): Kitchen 1", "Upstairs (2): Bedroom 1, Bedroom Bath 1", "(no area) (2): (no room) 2"} {
		if !strings.Contains(places, want) {
			t.Fatalf("missing %q in:\n%s", want, places)
		}
	}
}

func TestRender_GroupMembersByExactLabel(t *testing.T) {
	groups := section(t, Render(home, Options{Now: fixed}), "GROUPS")
	want := "plugin-automation.group.kitchenlights\tKitchen Lights\tlight\t1\tlight: turn_on turn_off; script: run"
	if !strings.Contains(groups, want) {
		t.Fatalf("missing %q in:\n%s", want, groups)
	}
}

func TestRender_QueryMatchesWholeValuesOnly(t *testing.T) {
	queries := section(t, Render(home, Options{Now: fixed}), "SAVED QUERIES")
	// Room "Bedroom" must not also match Room "Bedroom Bath".
	want := "sb-query.queries.bedroom_lights\tBedroom Lights\ttype eq \"light\" AND labels.Room eq \"Bedroom\"\t1"
	if !strings.Contains(queries, want) {
		t.Fatalf("missing %q in:\n%s", want, queries)
	}
}

func TestRender_Gaps(t *testing.T) {
	gaps := section(t, Render(home, Options{Now: fixed}), "GAPS")
	if !strings.Contains(gaps, "controllable entities with no Area label: 1") {
		t.Fatalf("no-area count wrong:\n%s", gaps)
	}
	if !strings.Contains(gaps, `Room "Bedroom" is part of Room "Bedroom Bath"`) {
		t.Fatalf("similar names not reported:\n%s", gaps)
	}
}

func TestRender_SummaryLeavesOutRows(t *testing.T) {
	out := Render(home, Options{Now: fixed, Section: SectionSummary})
	if strings.Contains(out, "## ENTITIES") || !strings.Contains(out, "## GROUPS") {
		t.Fatalf("summary sections wrong:\n%s", out)
	}
}

func TestRender_IntentsComeFirst(t *testing.T) {
	withIntent := append(append([]storage.Entry(nil), home...),
		entry("plugin-intent.intents.reading", `{"plugin":"plugin-intent","type":"button","name":"Reading time",
			"labels":{"Phrase":["it's reading time","reading time"],"Description":["dims the lounge"]},"commands":["button_press"],"state":{"presses":2}}`),
		entry("plugin-intent.intents.nophrase", `{"plugin":"plugin-intent","type":"button","name":"No phrases","commands":["button_press"]}`))
	out := Render(withIntent, Options{Now: fixed})
	if strings.Index(out, "## INTENTS") > strings.Index(out, "## PLACES") {
		t.Fatalf("intents must come before everything else:\n%s", out)
	}
	intents := section(t, out, "INTENTS")
	want := "plugin-intent.intents.reading\tReading time\tit's reading time | reading time\tdims the lounge\n"
	if !strings.Contains(intents, want) {
		t.Fatalf("missing %q in:\n%s", want, intents)
	}
	if strings.Contains(intents, "nophrase") {
		t.Fatal("a button without phrases is not an intent")
	}
	if ents := section(t, out, "ENTITIES"); strings.Contains(ents, "intents.reading") || !strings.Contains(ents, "intents.nophrase") {
		t.Fatalf("intents belong in INTENTS only:\n%s", ents)
	}
	if !strings.Contains(out, "9 entities, 6 listed") {
		t.Fatalf("intents must not be counted as listed entities:\n%s", out)
	}
	if sum := Render(withIntent, Options{Now: fixed, Section: SectionSummary}); !strings.Contains(sum, "intents.reading") {
		t.Fatal("the summary keeps intents")
	}
}

func TestRender_EmptyHome(t *testing.T) {
	out := Render(nil, Options{Now: fixed})
	if !strings.Contains(out, "0 entities, 0 listed") {
		t.Fatalf("empty render wrong:\n%s", out)
	}
}

func TestMatches(t *testing.T) {
	doc := map[string]any{"type": "light", "labels": map[string]any{"Room": []any{"Bedroom Bath"}}, "state": map[string]any{"brightness": 120.0}}
	cases := []struct {
		name string
		q    storage.Query
		want bool
	}{
		{"eq whole value", storage.Query{Where: []storage.Filter{{Field: "labels.Room", Op: "eq", Value: "bedroom bath"}}}, true},
		{"eq is not a word match", storage.Query{Where: []storage.Filter{{Field: "labels.Room", Op: "eq", Value: "Bedroom"}}}, false},
		{"neq", storage.Query{Where: []storage.Filter{{Field: "labels.Room", Op: "neq", Value: "Bedroom"}}}, true},
		{"in", storage.Query{Where: []storage.Filter{{Field: "type", Op: "in", Value: []any{"switch", "light"}}}}, true},
		{"contains", storage.Query{Where: []storage.Filter{{Field: "labels.Room", Op: "contains", Value: "bath"}}}, true},
		{"exists", storage.Query{Where: []storage.Filter{{Field: "labels.Area", Op: "exists"}}}, false},
		{"gte", storage.Query{Where: []storage.Filter{{Field: "state.brightness", Op: "gte", Value: 100.0}}}, true},
		{"key pattern", storage.Query{Pattern: "plugin-wiz.*.*"}, true},
		{"key pattern miss", storage.Query{Pattern: "plugin-kasa.>"}, false},
	}
	for _, c := range cases {
		if got := Matches("plugin-wiz.lamp3.light", doc, c.q); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
