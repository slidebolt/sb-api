package mcptools

import (
	"encoding/json"
	"strings"

	storage "github.com/slidebolt/sb-storage-sdk"
)

type entityFilter struct {
	Type, Plugin, Search string
}

func (f entityFilter) empty() bool { return f.Type == "" && f.Plugin == "" && f.Search == "" }

type entityRecord struct {
	Plugin   string              `json:"plugin"`
	Type     string              `json:"type"`
	Name     string              `json:"name"`
	Labels   map[string][]string `json:"labels"`
	State    json.RawMessage     `json:"state"`
	Commands []string            `json:"commands"`
	Profile  struct {
		Name string `json:"name"`
	} `json:"profile"`
}

// entitySummary is the compact form returned with summary set.
type entitySummary struct {
	Key      string          `json:"key"`
	Name     string          `json:"name,omitempty"`
	Type     string          `json:"type,omitempty"`
	Area     string          `json:"area,omitempty"`
	Room     string          `json:"room,omitempty"`
	State    json.RawMessage `json:"state,omitempty"`
	Commands []string        `json:"commands,omitempty"`
}

func (r entityRecord) displayName() string {
	if r.Profile.Name != "" {
		return r.Profile.Name
	}
	return r.Name
}

func (r entityRecord) matches(key string, f entityFilter) bool {
	if f.Type != "" && !strings.EqualFold(r.Type, f.Type) {
		return false
	}
	if f.Plugin != "" && r.Plugin != f.Plugin && !strings.HasPrefix(key, f.Plugin+".") {
		return false
	}
	if f.Search == "" {
		return true
	}
	needle := strings.ToLower(f.Search)
	hay := []string{key, r.Name, r.Profile.Name}
	for _, values := range r.Labels {
		hay = append(hay, values...)
	}
	for _, h := range hay {
		if strings.Contains(strings.ToLower(h), needle) {
			return true
		}
	}
	return false
}

func firstLabel(labels map[string][]string, name string) string {
	if v := labels[name]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// filterEntities returns the matching entries, in full or as summaries.
func filterEntities(entries []storage.Entry, f entityFilter, summary bool) any {
	full := []storage.Entry{}
	short := []entitySummary{}
	for _, e := range entries {
		var r entityRecord
		if err := json.Unmarshal(e.Data, &r); err != nil {
			continue
		}
		if !r.matches(e.Key, f) {
			continue
		}
		if !summary {
			full = append(full, e)
			continue
		}
		short = append(short, entitySummary{
			Key:      e.Key,
			Name:     r.displayName(),
			Type:     r.Type,
			Area:     firstLabel(r.Labels, "Area"),
			Room:     firstLabel(r.Labels, "Room"),
			State:    r.State,
			Commands: r.Commands,
		})
	}
	if summary {
		return short
	}
	return full
}
