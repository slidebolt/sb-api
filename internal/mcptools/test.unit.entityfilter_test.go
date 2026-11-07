package mcptools

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	storage "github.com/slidebolt/sb-storage-sdk"
)

func entry(key, data string) storage.Entry { return storage.Entry{Key: key, Data: json.RawMessage(data)} }

var sampleEntities = []storage.Entry{
	entry("plugin-zigbee.0xa.default-light", `{"plugin":"plugin-zigbee","type":"light","name":"Light",
		"profile":{"name":"Master Bathroom 04 Light"},"labels":{"Area":["Upstairs"],"Room":["Master Bedroom Bath"]},
		"state":{"power":true,"brightness":110},"commands":["light_turn_on","light_turn_off"]}`),
	entry("plugin-wiz.bulb1.light", `{"plugin":"plugin-wiz","type":"light","name":"Basement Bar","labels":{"Area":["Basement"]},
		"state":{"power":false},"commands":["light_turn_on"]}`),
	entry("plugin-kasa.plug1.switch", `{"plugin":"plugin-kasa","type":"switch","name":"Deck Lights","state":{"power":false}}`),
	entry("plugin-frigate.cam.snap", `{"plugin":"plugin-frigate","type":"button","name":"Snapshot"}`),
}

func keys(v any) []string {
	var out []string
	switch x := v.(type) {
	case []storage.Entry:
		for _, e := range x {
			out = append(out, e.Key)
		}
	case []entitySummary:
		for _, e := range x {
			out = append(out, e.Key)
		}
	}
	return out
}

func TestFilterEntities(t *testing.T) {
	cases := []struct {
		name string
		f    entityFilter
		want string
	}{
		{"by type", entityFilter{Type: "light"}, "plugin-zigbee.0xa.default-light,plugin-wiz.bulb1.light"},
		{"type is case-insensitive", entityFilter{Type: "LIGHT"}, "plugin-zigbee.0xa.default-light,plugin-wiz.bulb1.light"},
		{"by plugin", entityFilter{Plugin: "plugin-kasa"}, "plugin-kasa.plug1.switch"},
		{"search profile name", entityFilter{Search: "master bathroom"}, "plugin-zigbee.0xa.default-light"},
		{"search label", entityFilter{Search: "basement"}, "plugin-wiz.bulb1.light"},
		{"search plus type", entityFilter{Search: "deck", Type: "light"}, ""},
		{"no match", entityFilter{Search: "garage"}, ""},
	}
	for _, c := range cases {
		got := strings.Join(keys(filterEntities(sampleEntities, c.f, false)), ",")
		if got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestFilterEntitiesSummary(t *testing.T) {
	got := filterEntities(sampleEntities, entityFilter{Type: "light"}, true).([]entitySummary)
	if len(got) != 2 {
		t.Fatalf("got %d summaries", len(got))
	}
	s := got[0]
	if s.Name != "Master Bathroom 04 Light" || s.Area != "Upstairs" || s.Room != "Master Bedroom Bath" ||
		!strings.Contains(string(s.State), `"brightness":110`) || len(s.Commands) != 2 {
		t.Fatalf("summary: %+v", s)
	}
}

// With SB_ENTITIES_JSON pointing at a /entities dump, report how big the
// agent's usual query is on real data. Skipped otherwise.
func TestFilterEntitiesRealDataSize(t *testing.T) {
	path := os.Getenv("SB_ENTITIES_JSON")
	if path == "" {
		t.Skip("set SB_ENTITIES_JSON to a GET /entities dump")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var all []struct {
		Key  string          `json:"key"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &all); err != nil {
		t.Fatal(err)
	}
	entries := make([]storage.Entry, len(all))
	for i, e := range all {
		entries[i] = entry(e.Key, string(e.Data))
	}
	fullAll, _ := json.Marshal(entries)
	lights := filterEntities(entries, entityFilter{Type: "light"}, true).([]entitySummary)
	lightsJSON, _ := json.Marshal(lights)
	t.Logf("all entities in full: %d bytes; %d lights as summaries: %d bytes", len(fullAll), len(lights), len(lightsJSON))
}
