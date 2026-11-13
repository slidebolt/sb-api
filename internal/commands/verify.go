package commands

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	storage "github.com/slidebolt/sb-storage-sdk"
)

// Verification outcomes.
const (
	StatusVerified  = "verified"  // every target reached the requested state
	StatusPartial   = "partial"   // some targets did, some did not
	StatusNoChange  = "no_change" // no target reached the requested state
	StatusUnchecked = "unchecked" // the action has no state we know how to check
	StatusNotSent   = "not_sent"  // rejected before publishing
)

// check decides whether one entity's state satisfies the command.
type check func(before, after map[string]any) bool

// Expectation maps an action and payload to a state check. ok=false means the
// action has no state we know how to check (buttons, scenes, media keys...).
func Expectation(action string, payload json.RawMessage) (check, bool) {
	var p map[string]any
	_ = json.Unmarshal(payload, &p)
	num := func(m map[string]any, k string) (float64, bool) {
		v, ok := m[k].(float64)
		return v, ok
	}
	power := func(m map[string]any) (bool, bool) {
		v, ok := m["power"].(bool)
		return v, ok
	}
	switch {
	case strings.HasSuffix(action, "_turn_on"):
		return func(_, after map[string]any) bool { v, ok := power(after); return ok && v }, true
	case strings.HasSuffix(action, "_turn_off"):
		return func(_, after map[string]any) bool { v, ok := power(after); return ok && !v }, true
	case strings.HasSuffix(action, "_toggle"):
		return func(before, after map[string]any) bool {
			b, ok1 := power(before)
			a, ok2 := power(after)
			return ok1 && ok2 && a != b
		}, true
	case action == "light_set_brightness":
		want, ok := num(p, "brightness")
		if !ok {
			return nil, false
		}
		return func(_, after map[string]any) bool {
			on, _ := power(after)
			if want == 0 {
				got, _ := num(after, "brightness")
				return !on || got == 0
			}
			got, ok := num(after, "brightness")
			return on && ok && math.Abs(got-want) <= 4
		}, true
	case action == "light_set_color_temp":
		want, ok := num(p, "mireds")
		if !ok {
			return nil, false
		}
		return func(_, after map[string]any) bool {
			got, ok := num(after, "temperature")
			return ok && math.Abs(got-want) <= 10
		}, true
	case strings.HasPrefix(action, "light_set_") && requestedColourAction(action):
		want, ok := requestedColour(action, p)
		if !ok {
			return nil, false
		}
		return func(_, after map[string]any) bool {
			got, ok := reportedColour(after)
			return ok && sameColour(want, got)
		}, true
	}
	return nil, false
}

func requestedColourAction(action string) bool {
	switch action {
	case "light_set_rgb", "light_set_rgbw", "light_set_rgbww", "light_set_hs", "light_set_xy":
		return true
	}
	return false
}

// TargetState is one checked entity.
type TargetState struct {
	Key     string
	Name    string
	Reached bool
}

// Verification is the result of watching targets after a command.
type Verification struct {
	Status  string
	Checked []TargetState
	Changed bool // any target's state changed (useful when unchecked)
	Waited  time.Duration
}

// Lagging returns the targets that did not reach the requested state.
func (v Verification) Lagging() []TargetState {
	var out []TargetState
	for _, t := range v.Checked {
		if !t.Reached {
			out = append(out, t)
		}
	}
	return out
}

// Summary is a one-line, agent-readable description.
func (v Verification) Summary() string {
	total := len(v.Checked)
	reached := total - len(v.Lagging())
	switch v.Status {
	case StatusVerified:
		if total > 1 {
			return fmt.Sprintf("verified: all %d targets reached the requested state", total)
		}
		return "verified: the state changed as requested"
	case StatusPartial:
		return fmt.Sprintf("partly worked: %d of %d targets reached the requested state; not yet: %s", reached, total, names(v.Lagging(), 8))
	case StatusNoChange:
		return fmt.Sprintf("NOT confirmed: the command is valid but no target reached the requested state within %s. Do not tell the person it is done, and do not try other entities on your own: tell them it did not take effect and why (see Diagnostics).", v.Waited.Round(100*time.Millisecond))
	case StatusUnchecked:
		if v.Changed {
			return "sent; this action has no state check, but the entity's state changed"
		}
		return "sent; this action has no state check and no state change was seen"
	}
	return v.Status
}

func names(ts []TargetState, max int) string {
	out := make([]string, 0, len(ts))
	for i, t := range ts {
		if i == max {
			out = append(out, fmt.Sprintf("and %d more", len(ts)-max))
			break
		}
		out = append(out, t.Name)
	}
	return strings.Join(out, ", ")
}

// Verifier watches the command's targets by polling storage.
type Verifier struct {
	Store   storage.Storage
	Timeout time.Duration
	Poll    time.Duration
	Sleep   func(time.Duration) // injectable for tests
}

// Snapshot captures the targets' states before the command is published:
// the members of a group, or the entity itself.
func (v Verifier) Snapshot(rec EntityRecord) map[string]EntityRecord {
	out := map[string]EntityRecord{}
	if rec.IsGroup() {
		if members, err := Members(v.Store, rec); err == nil && len(members) > 0 {
			// Nested groups only aggregate their own members, which are
			// checked directly.
			for _, m := range members {
				if !m.IsGroup() {
					out[m.Key] = m
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	out[rec.Key] = rec
	return out
}

// Wait polls the targets until they all satisfy the command or the timeout passes.
func (v Verifier) Wait(action string, payload json.RawMessage, before map[string]EntityRecord) Verification {
	timeout, poll := v.Timeout, v.Poll
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	if poll <= 0 {
		poll = 150 * time.Millisecond
	}
	sleep := v.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	keys := make([]string, 0, len(before))
	for k := range before {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	satisfied, checkable := Expectation(action, payload)
	var waited time.Duration
	for {
		res := Verification{Waited: waited}
		allReached := true
		for _, k := range keys {
			prev := before[k]
			cur, err := LoadEntity(v.Store, k)
			if err != nil {
				cur = prev
			}
			if !statesEqual(prev.State, cur.State) {
				res.Changed = true
			}
			reached := checkable && satisfied(prev.State, cur.State)
			if !reached {
				allReached = false
			}
			res.Checked = append(res.Checked, TargetState{Key: k, Name: prev.DisplayName(), Reached: reached})
		}
		if !checkable {
			// Give an unchecked action a short moment to show any change.
			if res.Changed || waited >= timeout/3 {
				res.Status = StatusUnchecked
				return res
			}
		} else if allReached {
			res.Status = StatusVerified
			return res
		} else if waited >= timeout {
			if len(res.Lagging()) < len(res.Checked) {
				res.Status = StatusPartial
			} else {
				res.Status = StatusNoChange
			}
			return res
		}
		sleep(poll)
		waited += poll
	}
}

func statesEqual(a, b map[string]any) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}
