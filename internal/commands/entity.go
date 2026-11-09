package commands

import (
	"encoding/json"
	"fmt"
	"strings"

	storage "github.com/slidebolt/sb-storage-sdk"
)

// rawKey satisfies storage.Keyed for a plain key string.
type rawKey string

func (k rawKey) Key() string { return string(k) }

// EntityRecord is the part of a stored entity that command checks need.
type EntityRecord struct {
	Key      string          `json:"-"`
	Type     string          `json:"type"`
	Name     string          `json:"name"`
	Commands []string        `json:"commands"`
	State    map[string]any  `json:"state"`
	Target   json.RawMessage `json:"target"`
	Profile  *struct {
		Name string `json:"name"`
	} `json:"profile"`
}

// DisplayName prefers the user's profile name.
func (e EntityRecord) DisplayName() string {
	if e.Profile != nil && strings.TrimSpace(e.Profile.Name) != "" {
		return e.Profile.Name
	}
	if strings.TrimSpace(e.Name) != "" {
		return e.Name
	}
	return e.Key
}

// IsGroup reports whether the entity fans its commands out to members.
func (e EntityRecord) IsGroup() bool {
	t := strings.TrimSpace(string(e.Target))
	return t != "" && t != "null" && t != "{}"
}

// HasCommand reports whether the entity lists the action.
func (e EntityRecord) HasCommand(action string) bool {
	for _, c := range e.Commands {
		if c == action {
			return true
		}
	}
	return false
}

func parseEntity(key string, data json.RawMessage) (EntityRecord, error) {
	var rec EntityRecord
	if len(data) == 0 || string(data) == "null" {
		return rec, fmt.Errorf("no entity %s", key)
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		return rec, fmt.Errorf("entity %s: %w", key, err)
	}
	rec.Key = key
	return rec, nil
}

// LoadEntity reads one entity from storage.
func LoadEntity(store storage.Storage, key string) (EntityRecord, error) {
	data, err := store.Get(rawKey(key))
	if err != nil {
		return EntityRecord{}, fmt.Errorf("no entity %s: %v", key, err)
	}
	return parseEntity(key, data)
}

// Members resolves a group's member entities from its target query.
func Members(store storage.Storage, group EntityRecord) ([]EntityRecord, error) {
	if !group.IsGroup() {
		return nil, nil
	}
	var q storage.Query
	if err := json.Unmarshal(group.Target, &q); err != nil {
		return nil, fmt.Errorf("group %s target: %w", group.Key, err)
	}
	entries, err := store.Query(q)
	if err != nil {
		return nil, err
	}
	out := make([]EntityRecord, 0, len(entries))
	for _, e := range entries {
		if e.Key == group.Key {
			continue
		}
		rec, err := parseEntity(e.Key, e.Data)
		if err != nil {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}
