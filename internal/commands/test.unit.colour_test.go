package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	logging "github.com/slidebolt/sb-logging-sdk"
	messenger "github.com/slidebolt/sb-messenger-sdk"
)

func TestColourCheck_MatchesWhateverFormTheLightReports(t *testing.T) {
	green := `{"r":0,"g":255,"b":0}`
	cases := []struct {
		name    string
		action  string
		payload string
		state   map[string]any
		want    bool
	}{
		{"zigbee xy, sRGB green", "light_set_rgb", green, map[string]any{"colorMode": "xy", "xy": []any{0.3, 0.6}}, true},
		{"zigbee xy, the bulb's deeper green", "light_set_rgb", green, map[string]any{"colorMode": "xy", "xy": []any{0.1724, 0.6939}}, true},
		{"zigbee xy, still warm white", "light_set_rgb", green, map[string]any{"colorMode": "xy", "xy": []any{0.5267, 0.4133}}, false},
		{"zigbee xy, red", "light_set_rgb", green, map[string]any{"colorMode": "xy", "xy": []any{0.6915, 0.3083}}, false},
		{"rgb state", "light_set_rgb", green, map[string]any{"rgb": []any{10.0, 240.0, 20.0}}, true},
		{"hs state", "light_set_rgb", green, map[string]any{"colorMode": "hs", "hs": []any{118.0, 95.0}}, true},
		{"white mode", "light_set_rgb", green, map[string]any{"colorMode": "color_temp", "xy": []any{0.3, 0.6}, "temperature": 370.0}, false},
		{"no colour at all", "light_set_rgb", green, map[string]any{"power": true}, false},
		{"hs request, xy light", "light_set_hs", `{"hue":240,"saturation":100}`, map[string]any{"colorMode": "xy", "xy": []any{0.1355, 0.0399}}, true},
		{"xy request, rgb light", "light_set_xy", `{"x":0.6915,"y":0.3083}`, map[string]any{"rgb": []any{255.0, 0.0, 0.0}}, true},
		{"white rgb request, near-white xy", "light_set_rgb", `{"r":255,"g":255,"b":255}`, map[string]any{"colorMode": "xy", "xy": []any{0.3227, 0.329}}, true},
		{"white rgb request, red light", "light_set_rgb", `{"r":255,"g":255,"b":255}`, map[string]any{"rgb": []any{255.0, 0.0, 0.0}}, false},
	}
	for _, c := range cases {
		check, ok := Expectation(c.action, json.RawMessage(c.payload))
		if !ok {
			t.Fatalf("%s: %s should be checkable", c.name, c.action)
		}
		if got := check(nil, c.state); got != c.want {
			t.Errorf("%s: reached=%v, want %v", c.name, got, c.want)
		}
	}
}

// A Zigbee bulb reports colour as xy only: green on it is verified.
func TestSend_GreenOnAnXYLightIsVerified(t *testing.T) {
	h := newHouse(t)
	key := "plugin-zigbee.0xtest.default-light"
	h.store.put(key, map[string]any{
		"type": "light", "name": "Light Bar", "commands": append(append([]string{}, lightCommands...), "light_set_rgb"),
		"state": map[string]any{"power": true, "brightness": 100, "colorMode": "xy", "xy": []any{0.5267, 0.4133}},
	})
	sub, err := h.msg.Subscribe("plugin-zigbee.0xtest.default-light.command.light_set_rgb", func(*messenger.Message) {
		h.store.setState(key, map[string]any{"power": true, "brightness": 100, "colorMode": "xy", "xy": []any{0.1724, 0.6939}})
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe()
	_ = h.msg.Flush()
	res, err := h.sender.Send(context.Background(), Request{
		Plugin: "plugin-zigbee", Device: "0xtest", Entity: "default-light",
		Action: "light_set_rgb", Payload: json.RawMessage(`{"r":0,"g":255,"b":0}`),
		Words: "turn one light green", Strict: true, Wait: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Verification.Status != StatusVerified {
		t.Fatalf("status %s: %s", res.Verification.Status, res.Text())
	}
}

func TestCorrect_WorkedMarksVerified(t *testing.T) {
	b := &Book{Store: newMemStore(), Now: func() time.Time { return at(30) }}
	last := record(t, b, attempt("turn one light green", "light_set_rgb", `{"r":0,"g":255,"b":0}`, StatusNoChange, 20))
	e, err := b.Correct("", "", "it turned green", true)
	if err != nil {
		t.Fatal(err)
	}
	if e.ID != last.Entry.ID || e.Status != EntryVerified || !strings.Contains(e.Reason, "it turned green") {
		t.Fatalf("want the last entry verified with the note, got %+v", e)
	}
}

// "no delivery record" is only said for a plugin that records deliveries.
func TestDiagnostics_NoDeliveryRecordOnlyWhereThePluginRecordsThem(t *testing.T) {
	h := newHouse(t)
	h.broken["tv-01"] = true
	r := h.send(t, "turn off the basement lights", "light_turn_off", `{}`)
	if d := strings.Join(r.Diagnostics, "\n"); strings.Contains(d, "no delivery record") {
		t.Fatalf("the plugin records no deliveries, so a missing one means nothing:\n%s", d)
	}

	// Now the plugin records deliveries, for the devices that got the command.
	sub, err := h.msg.Subscribe("plugin-automation.group.basement.command.>", func(m *messenger.Message) {
		for _, n := range []string{"bar-01", "bar-02"} {
			_ = h.logs.Append(context.Background(), logging.Event{
				ID: fmt.Sprintf("rx-%s-%d", n, time.Now().UnixNano()), TS: time.Now().UTC(), Source: "plugin-esphome",
				Kind: "device.command.received", Level: "info", Plugin: "plugin-esphome", Device: n,
				TraceID: messenger.TraceID(m.Headers),
			})
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe()
	_ = h.msg.Flush()
	r = h.send(t, "set the basement lights to 20%", "light_set_brightness", `{"brightness":50}`)
	if d := strings.Join(r.Diagnostics, "\n"); !strings.Contains(d, "no delivery record") {
		t.Fatalf("tv-01's plugin recorded deliveries for others but not tv-01:\n%s", d)
	}
}
