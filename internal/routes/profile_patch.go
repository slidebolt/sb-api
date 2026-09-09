package routes

import (
	"encoding/json"
	"strings"

	storage "github.com/slidebolt/sb-storage-sdk"
)

func mergeProfilePatch(store storage.Storage, key storage.Keyed, patch json.RawMessage) (json.RawMessage, bool, error) {
	var patchDoc map[string]any
	if len(patch) == 0 {
		patch = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(patch, &patchDoc); err != nil {
		return nil, false, err
	}

	baseDoc := map[string]any{}
	if existing, err := store.ReadFile(storage.Profile, key); err == nil && len(existing) > 0 {
		if err := json.Unmarshal(existing, &baseDoc); err != nil {
			return nil, false, err
		}
	} else if err != nil && !strings.Contains(err.Error(), "not found") {
		return nil, false, err
	}

	merged := mergeJSONObjects(baseDoc, patchDoc)
	if len(merged) == 0 {
		return json.RawMessage(`{}`), true, nil
	}
	data, err := json.Marshal(merged)
	return data, false, err
}

func mergeJSONObjects(base, patch map[string]any) map[string]any {
	if base == nil {
		base = map[string]any{}
	}
	for key, patchValue := range patch {
		if patchValue == nil {
			delete(base, key)
			continue
		}
		patchMap, patchIsMap := patchValue.(map[string]any)
		baseMap, baseIsMap := base[key].(map[string]any)
		if patchIsMap && baseIsMap {
			nested := mergeJSONObjects(baseMap, patchMap)
			if len(nested) == 0 {
				delete(base, key)
			} else {
				base[key] = nested
			}
			continue
		}
		base[key] = patchValue
	}
	return base
}
