package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	logging "github.com/slidebolt/sb-logging-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"
)

// Diagnoser looks deeper into why a request keeps being repeated. The level is
// the ask number: 2 lists the targets that did not change, 3 adds whether the
// command reached each device and whether the device is online, 4+ adds recent
// device errors and scripts that may be overriding the targets.
type Diagnoser struct {
	Store storage.Storage
	Logs  logging.Store
	Now   func() time.Time
}

func (d Diagnoser) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// Diagnose returns report lines. level is how deep to look (2: which targets
// lagged, 3: + delivery and connectivity, 4+: + errors, scripts and a
// conclusion); ask is how many times the person has asked, for the wording.
// trace is the attempt whose delivery is inspected.
func (d Diagnoser) Diagnose(ctx context.Context, level, ask int, current Verification, trace string) []string {
	if level < 2 {
		return nil
	}
	prevTrace := trace
	lagging := current.Lagging()
	var lines []string
	if len(current.Checked) > 0 && len(lagging) == 0 && current.Status != StatusUnchecked {
		lines = append(lines, fmt.Sprintf("Ask #%d: every target is already in the requested state (%d checked). The command works; the person may mean different lights or devices, so ask which ones.", ask, len(current.Checked)))
		return lines
	}
	if len(lagging) > 0 {
		lines = append(lines, fmt.Sprintf("Ask #%d: %d of %d targets did not reach the requested state: %s.", ask, len(lagging), len(current.Checked), names(lagging, 5)))
	} else {
		lines = append(lines, fmt.Sprintf("Ask #%d: the result could not be checked from state.", ask))
	}
	if level < 3 {
		return lines
	}

	// Level 3: delivery and connectivity per lagging device.
	devices := map[string][]TargetState{} // plugin.device -> targets
	order := []string{}
	for _, t := range lagging {
		pd := pluginDevice(t.Key)
		if _, ok := devices[pd]; !ok {
			order = append(order, pd)
		}
		devices[pd] = append(devices[pd], t)
	}
	var delivered map[string][]logging.Event
	if d.Logs != nil && prevTrace != "" {
		events, err := d.Logs.List(ctx, logging.ListRequest{TraceID: prevTrace, Limit: 500})
		if err == nil {
			delivered = map[string][]logging.Event{}
			for _, e := range events {
				if e.Plugin != "" && e.Device != "" {
					pd := e.Plugin + "." + e.Device
					delivered[pd] = append(delivered[pd], e)
				}
			}
		}
	}
	// Collect each lagging device's condition, then report devices that share
	// a condition together, so 30 offline bulbs are one line, not 30.
	offline := []string{}
	dropped := []string{}
	type group struct {
		cond  string
		names []string
	}
	var groups []*group
	byCond := map[string]*group{}
	for _, pd := range order {
		label := devices[pd][0].Name
		var notes []string
		if conn, detail, ok := d.connectivity(pd); ok {
			if conn {
				notes = append(notes, "online")
			} else {
				note := "OFFLINE"
				if detail != "" {
					note += " (" + shortError(detail) + ")"
				}
				notes = append(notes, note)
				offline = append(offline, label)
			}
		}
		if delivered != nil {
			received, drop := false, ""
			for _, e := range delivered[pd] {
				switch {
				case strings.Contains(e.Kind, "dropped"):
					drop = "command dropped by " + e.Source
					if r, ok := e.Data["reason"].(string); ok {
						drop += " (" + r + ")"
					}
				case strings.Contains(e.Kind, "received"):
					received = true
				}
			}
			switch {
			case drop != "":
				notes = append(notes, drop)
				dropped = append(dropped, label)
			case received:
				notes = append(notes, "plugin received the command")
			default:
				notes = append(notes, "no delivery record")
			}
		}
		if len(notes) == 0 {
			continue
		}
		cond := strings.Join(notes, "; ")
		g, ok := byCond[cond]
		if !ok {
			g = &group{cond: cond}
			byCond[cond] = g
			groups = append(groups, g)
		}
		g.names = append(g.names, label)
	}
	for _, g := range groups {
		lines = append(lines, fmt.Sprintf("  %s: %s", listNames(g.names, 4), g.cond))
	}
	if level < 4 {
		if len(offline) > 0 {
			lines = append(lines, fmt.Sprintf("Likely cause: %d of %d devices offline. The command is right; the devices are not answering (powered off at the wall, or a network problem).", len(offline), len(order)))
		}
		return lines
	}

	// Level 4+: recent device errors, overriding scripts, and a conclusion.
	if d.Logs != nil {
		since := d.now().Add(-15 * time.Minute)
		for _, pd := range order {
			plugin, device, _ := strings.Cut(pd, ".")
			var bad []logging.Event
			for _, level := range []string{"error", "warn"} {
				events, err := d.Logs.List(ctx, logging.ListRequest{Plugin: plugin, Device: device, Level: level, Since: since, Limit: 20})
				if err == nil {
					bad = append(bad, events...)
				}
			}
			if len(bad) > 0 {
				last := bad[len(bad)-1]
				lines = append(lines, fmt.Sprintf("  %s: %d warnings/errors in 15 min, latest %q", devices[pd][0].Name, len(bad), last.Message))
			}
		}
	}
	scripts := d.overridingScripts(lagging)
	if len(scripts) > 0 {
		lines = append(lines, "  Running scripts that also drive these targets: "+strings.Join(scripts, ", ")+". They may be undoing the command; stop them first.")
	}
	switch {
	case len(offline) > 0:
		lines = append(lines, fmt.Sprintf("Conclusion: %s offline. This is not a command problem; they are probably powered off at the wall or out of range. Tell the person.", listNames(offline, 4)))
	case len(dropped) > 0:
		lines = append(lines, "Conclusion: the plugin dropped the command for "+listNames(dropped, 4)+". This is a device connection problem, not a wrong command. Tell the person.")
	case len(scripts) > 0:
		lines = append(lines, "Conclusion: a running script is overriding the targets.")
	default:
		lines = append(lines, "Conclusion: no cause found. Flagged for review; tell the person it has been flagged.")
	}
	return lines
}

func pluginDevice(key string) string {
	parts := strings.SplitN(key, ".", 3)
	if len(parts) < 2 {
		return key
	}
	return parts[0] + "." + parts[1]
}

// connectivity reports whether the device is online, from a connectivity
// binary_sensor (zigbee availability) or a connection status entity with
// state.connected (esphome). detail carries the last connection error.
func (d Diagnoser) connectivity(pd string) (online bool, detail string, known bool) {
	if d.Store == nil {
		return false, "", false
	}
	entries, err := d.Store.Search(pd + ".*")
	if err != nil {
		return false, "", false
	}
	for _, e := range entries {
		var rec struct {
			Type  string         `json:"type"`
			State map[string]any `json:"state"`
		}
		if json.Unmarshal(e.Data, &rec) != nil {
			continue
		}
		if dc, _ := rec.State["deviceClass"].(string); rec.Type == "binary_sensor" && dc == "connectivity" {
			on, _ := rec.State["on"].(bool)
			return on, "", true
		}
		if connected, ok := rec.State["connected"].(bool); ok {
			last, _ := rec.State["lastError"].(string)
			return connected, last, true
		}
	}
	return false, "", false
}

// overridingScripts lists running script instances that target any of the keys.
func (d Diagnoser) overridingScripts(targets []TargetState) []string {
	if d.Store == nil || len(targets) == 0 {
		return nil
	}
	entries, err := d.Store.Search("sb-script.instances.*")
	if err != nil {
		return nil
	}
	want := map[string]bool{}
	for _, t := range targets {
		want[t.Key] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		var inst struct {
			Name            string   `json:"name"`
			Status          string   `json:"status"`
			QueryRef        string   `json:"queryRef"`
			ResolvedTargets []string `json:"resolvedTargets"`
		}
		if json.Unmarshal(e.Data, &inst) != nil || inst.Status != "running" {
			continue
		}
		for _, k := range inst.ResolvedTargets {
			if want[k] && !seen[inst.Name] {
				seen[inst.Name] = true
				out = append(out, inst.Name)
				break
			}
		}
	}
	return out
}

// listNames shows up to max names, then a count.
func listNames(all []string, max int) string {
	if len(all) <= max {
		return strings.Join(all, ", ")
	}
	return fmt.Sprintf("%d devices (%s and %d more)", len(all), strings.Join(all[:max], ", "), len(all)-max)
}

// shortError drops addresses from a connection error so identical failures
// group together: "dial tcp 10.0.0.5:6053: connect: network is unreachable"
// becomes "network is unreachable".
func shortError(e string) string {
	if i := strings.LastIndex(e, ": "); i >= 0 {
		return e[i+2:]
	}
	return e
}
