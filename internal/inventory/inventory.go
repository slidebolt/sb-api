// Package inventory renders everything SlideBolt knows about as one compact
// text document: places, groups, saved queries and one line per entity. It is
// built for an LLM (or a person) to read in a single call instead of searching.
//
// It works only from the full entity listing and does its own exact matching,
// so it never depends on the storage query engine.
package inventory

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	storage "github.com/slidebolt/sb-storage-sdk"
)

// Scope selects which entities get a row.
const (
	ScopeControllable = "controllable" // entities that accept commands
	ScopeAll          = "all"
)

// Section selects how much is rendered.
const (
	SectionFull    = "full"
	SectionSummary = "summary" // everything except the entity rows
)

// Options control the rendering. The zero value means controllable entities,
// no state, every section.
type Options struct {
	Scope   string
	State   bool
	Section string
	Now     time.Time
}

const groupPrefix = "plugin-automation.group."

type record struct {
	Key      string
	Plugin   string              `json:"plugin"`
	Type     string              `json:"type"`
	Name     string              `json:"name"`
	Labels   map[string][]string `json:"labels"`
	State    json.RawMessage     `json:"state"`
	Commands []string            `json:"commands"`
	Profile  struct {
		Name string `json:"name"`
	} `json:"profile"`
	Query *storage.Query `json:"query"`
	doc   map[string]any
}

func (r record) name() string {
	if r.Profile.Name != "" {
		return r.Profile.Name
	}
	return r.Name
}

func (r record) label(k string) string {
	if v := r.Labels[k]; len(v) > 0 {
		return v[0]
	}
	return ""
}

func (r record) controllable() bool { return len(r.Commands) > 0 }

func parse(entries []storage.Entry) []record {
	out := make([]record, 0, len(entries))
	for _, e := range entries {
		var r record
		if json.Unmarshal(e.Data, &r) != nil {
			continue
		}
		_ = json.Unmarshal(e.Data, &r.doc)
		r.Key = e.Key
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Render returns the inventory of the given entities (the result of a
// "*.*.*" storage search) as text.
func Render(entries []storage.Entry, opt Options) string {
	if opt.Scope != ScopeAll {
		opt.Scope = ScopeControllable
	}
	if opt.Section != SectionSummary {
		opt.Section = SectionFull
	}
	if opt.Now.IsZero() {
		opt.Now = time.Now()
	}
	all := parse(entries)
	var shown []record
	for _, r := range all {
		if opt.Scope == ScopeAll || r.controllable() {
			shown = append(shown, r)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# SlideBolt inventory\n")
	fmt.Fprintf(&b, "generated %s; %d entities, %d listed (scope=%s)\n", opt.Now.UTC().Format(time.RFC3339), len(all), len(shown), opt.Scope)
	b.WriteString("Entity keys are plugin.device.entity; send commands to a key. Area and Room are the entity's place labels.\n")

	writePlaces(&b, shown)
	writeGroups(&b, all)
	writeQueries(&b, all)
	if opt.Section == SectionFull {
		writeEntities(&b, shown, opt.State)
	}
	writeGaps(&b, shown)
	return b.String()
}

// --- places -----------------------------------------------------------------

func writePlaces(b *strings.Builder, rs []record) {
	type count struct {
		name string
		n    int
	}
	areas := map[string]map[string]int{}
	totals := map[string]int{}
	for _, r := range rs {
		a, rm := r.label("Area"), r.label("Room")
		if areas[a] == nil {
			areas[a] = map[string]int{}
		}
		areas[a][rm]++
		totals[a]++
	}
	names := make([]string, 0, len(areas))
	for a := range areas {
		names = append(names, a)
	}
	sort.Slice(names, func(i, j int) bool { return sortLast(names[i], names[j]) })

	b.WriteString("\n## PLACES (Area: Room count, ...) for the listed entities\n")
	for _, a := range names {
		var rooms []count
		for rm, n := range areas[a] {
			rooms = append(rooms, count{rm, n})
		}
		sort.Slice(rooms, func(i, j int) bool {
			if rooms[i].n != rooms[j].n {
				return rooms[i].n > rooms[j].n
			}
			return sortLast(rooms[i].name, rooms[j].name)
		})
		parts := make([]string, len(rooms))
		for i, c := range rooms {
			parts[i] = fmt.Sprintf("%s %d", orNone(c.name, "(no room)"), c.n)
		}
		fmt.Fprintf(b, "%s (%d): %s\n", orNone(a, "(no area)"), totals[a], strings.Join(parts, ", "))
	}
}

// sortLast orders names alphabetically with the empty name last.
func sortLast(a, b string) bool {
	if (a == "") != (b == "") {
		return b == ""
	}
	return a < b
}

func orNone(s, none string) string {
	if s == "" {
		return none
	}
	return s
}

// --- groups -----------------------------------------------------------------

func writeGroups(b *strings.Builder, all []record) {
	var groups []record
	for _, r := range all {
		if strings.HasPrefix(r.Key, groupPrefix) {
			groups = append(groups, r)
		}
	}
	if len(groups) == 0 {
		return
	}
	b.WriteString("\n## GROUPS (one command to the group reaches every member; members carry label PluginAutomation=<group>)\n")
	b.WriteString("key\tname\ttype\tmembers\tcommands\n")
	for _, g := range groups {
		label := g.Name
		if label == "" {
			label = strings.TrimPrefix(g.Key, groupPrefix)
		}
		n := 0
		for _, r := range all {
			if strings.HasPrefix(r.Key, groupPrefix) {
				continue
			}
			for _, v := range r.Labels["PluginAutomation"] {
				if strings.EqualFold(v, label) {
					n++
					break
				}
			}
		}
		name := g.name()
		if !sameWords(name, label) {
			name += " (label " + label + ")"
		}
		fmt.Fprintf(b, "%s\t%s\t%s\t%d\t%s\n", g.Key, name, g.Type, n, commands(g.Commands))
	}
}

// sameWords reports whether two names are equal ignoring case and spaces.
func sameWords(a, b string) bool {
	squash := func(s string) string { return strings.ToLower(strings.ReplaceAll(s, " ", "")) }
	return squash(a) == squash(b)
}

// --- saved queries ----------------------------------------------------------

func writeQueries(b *strings.Builder, all []record) {
	var qs []record
	for _, r := range all {
		if r.Query != nil {
			qs = append(qs, r)
		}
	}
	if len(qs) == 0 {
		return
	}
	b.WriteString("\n## SAVED QUERIES (matches = entities the filter selects by exact comparison)\n")
	b.WriteString("key\tname\tfilter\tmatches\n")
	for _, q := range qs {
		n := 0
		for _, r := range all {
			if r.Query == nil && Matches(r.Key, r.doc, *q.Query) {
				n++
			}
		}
		fmt.Fprintf(b, "%s\t%s\t%s\t%d\n", q.Key, q.name(), describe(*q.Query), n)
	}
}

func describe(q storage.Query) string {
	var parts []string
	if q.Pattern != "" && q.Pattern != ">" {
		parts = append(parts, "key "+q.Pattern)
	}
	for _, f := range q.Where {
		if f.Op == storage.Exists {
			parts = append(parts, f.Field+" exists")
			continue
		}
		v, _ := json.Marshal(f.Value)
		parts = append(parts, fmt.Sprintf("%s %s %s", f.Field, f.Op, v))
	}
	if len(parts) == 0 {
		return "everything"
	}
	return strings.Join(parts, " AND ")
}

// --- entities ---------------------------------------------------------------

func writeEntities(b *strings.Builder, rs []record, withState bool) {
	sorted := append([]record(nil), rs...)
	sort.SliceStable(sorted, func(i, j int) bool {
		ai, aj := sorted[i].label("Area"), sorted[j].label("Area")
		if ai != aj {
			return sortLast(ai, aj)
		}
		ri, rj := sorted[i].label("Room"), sorted[j].label("Room")
		if ri != rj {
			return sortLast(ri, rj)
		}
		return sorted[i].Key < sorted[j].Key
	})
	b.WriteString("\n## ENTITIES (groups are listed above, not here)\n")
	header := "key\ttype\tname\tarea\troom\tlabels\tcommands"
	if withState {
		header += "\tstate"
	}
	b.WriteString(header + "\n")
	for _, r := range sorted {
		if strings.HasPrefix(r.Key, groupPrefix) {
			continue
		}
		row := []string{r.Key, r.Type, r.name(), r.label("Area"), r.label("Room"), otherLabels(r.Labels), commands(r.Commands)}
		if withState {
			row = append(row, state(r.State))
		}
		b.WriteString(clean(strings.Join(row, "\t")) + "\n")
	}
}

func otherLabels(labels map[string][]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		if k == "Area" || k == "Room" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+strings.Join(labels[k], ","))
	}
	// Area and Room beyond the first value are kept too.
	for _, k := range []string{"Area", "Room"} {
		if v := labels[k]; len(v) > 1 {
			parts = append(parts, k+"="+strings.Join(v, ","))
		}
	}
	return strings.Join(parts, ";")
}

// commands groups actions by their first word: "light: turn_on turn_off".
func commands(cmds []string) string {
	var order []string
	groups := map[string][]string{}
	for _, c := range cmds {
		head, rest, ok := strings.Cut(c, "_")
		if !ok {
			head, rest = c, ""
		}
		if _, seen := groups[head]; !seen {
			order = append(order, head)
		}
		if rest != "" {
			groups[head] = append(groups[head], rest)
		} else if groups[head] == nil {
			groups[head] = []string{}
		}
	}
	parts := make([]string, 0, len(order))
	for _, h := range order {
		if len(groups[h]) == 0 {
			parts = append(parts, h)
			continue
		}
		parts = append(parts, h+": "+strings.Join(groups[h], " "))
	}
	return strings.Join(parts, "; ")
}

// state renders up to four scalar state fields as k=v.
func state(raw json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k, v := range m {
		switch v.(type) {
		case bool, float64, string:
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		pi, pj := statePriority(keys[i]), statePriority(keys[j])
		if pi != pj {
			return pi < pj
		}
		return keys[i] < keys[j]
	})
	if len(keys) > 4 {
		keys = keys[:4]
	}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := m[k]
		if k == "power" {
			if on, ok := v.(bool); ok {
				parts = append(parts, map[bool]string{true: "on", false: "off"}[on])
				continue
			}
		}
		s := fmt.Sprint(v)
		if len(s) > 24 {
			s = s[:24] + "…"
		}
		parts = append(parts, k+"="+s)
	}
	return strings.Join(parts, " ")
}

func statePriority(k string) int {
	switch k {
	case "power":
		return 0
	case "brightness":
		return 1
	case "position", "state", "locked":
		return 2
	}
	return 3
}

func clean(s string) string { return strings.ReplaceAll(s, "\n", " ") }

// --- gaps -------------------------------------------------------------------

func writeGaps(b *strings.Builder, rs []record) {
	var noArea []string
	for _, r := range rs {
		if r.controllable() && r.label("Area") == "" && !strings.HasPrefix(r.Key, groupPrefix) && r.Query == nil {
			noArea = append(noArea, r.Key)
		}
	}
	similar := similarPlaces(rs)
	if len(noArea) == 0 && len(similar) == 0 {
		return
	}
	b.WriteString("\n## GAPS (facts about the labels, not instructions)\n")
	if len(noArea) > 0 {
		fmt.Fprintf(b, "controllable entities with no Area label: %d\n", len(noArea))
	}
	for _, s := range similar {
		b.WriteString(s + "\n")
	}
}

// similarPlaces lists place names whose words are contained in another
// place name, e.g. Room "Bathroom" and Room "Upstairs Bathroom".
func similarPlaces(rs []record) []string {
	type place struct{ kind, name string }
	seen := map[place]bool{}
	for _, r := range rs {
		for _, k := range []string{"Area", "Room"} {
			for _, v := range r.Labels[k] {
				seen[place{k, v}] = true
			}
		}
	}
	places := make([]place, 0, len(seen))
	for p := range seen {
		places = append(places, p)
	}
	sort.Slice(places, func(i, j int) bool {
		if places[i].kind != places[j].kind {
			return places[i].kind < places[j].kind
		}
		return places[i].name < places[j].name
	})
	var out []string
	for _, a := range places {
		for _, c := range places {
			if a == c {
				continue
			}
			if strings.EqualFold(a.name, c.name) {
				if a.kind < c.kind {
					out = append(out, fmt.Sprintf("%s %q is also used as a %s", a.kind, a.name, c.kind))
				}
				continue
			}
			if a.kind == c.kind && wordsWithin(a.name, c.name) {
				out = append(out, fmt.Sprintf("%s %q is part of %s %q", a.kind, a.name, c.kind, c.name))
			}
		}
	}
	return out
}

func wordsWithin(small, big string) bool {
	bw := map[string]bool{}
	for _, w := range strings.Fields(strings.ToLower(big)) {
		bw[w] = true
	}
	sw := strings.Fields(strings.ToLower(small))
	if len(sw) == 0 || len(sw) >= len(bw) {
		return false
	}
	for _, w := range sw {
		if !bw[w] {
			return false
		}
	}
	return true
}
