package routes

import (
	"context"
	"encoding/json"
	"strings"

	domain "github.com/slidebolt/sb-domain"
	"github.com/danielgtaylor/huma/v2"
	storage "github.com/slidebolt/sb-storage-sdk"
)

type DeviceEntry struct {
	Key  string          `json:"key" doc:"Dot-delimited storage key"`
	Data json.RawMessage `json:"data" doc:"Device JSON object"`
}

type DevicesOutput struct {
	Body []DeviceEntry
}

type DeviceOutput struct {
	Body json.RawMessage
}

type DeviceInput struct {
	Plugin string `path:"plugin"`
	ID     string `path:"id"`
	Body   json.RawMessage
}

// extractDeviceProfileFields returns a JSON object with labels/meta/profile from
// the device if any are present, or nil if none are set.
func extractDeviceProfileFields(dev domain.Device) json.RawMessage {
	profile := make(map[string]any)
	if len(dev.Labels) > 0 {
		profile["labels"] = dev.Labels
	}
	if len(dev.Meta) > 0 {
		profile["meta"] = dev.Meta
	}
	if dev.Profile != nil {
		profile["profile"] = dev.Profile
	}
	if len(profile) == 0 {
		return nil
	}
	data, _ := json.Marshal(profile)
	return data
}

type DeviceKey struct {
	Plugin string `path:"plugin"`
	ID     string `path:"id"`
}

func (k DeviceKey) Key() string { return k.Plugin + "." + k.ID }

// rawKeyed wraps a key + raw JSON so it satisfies storage.Keyed.
type rawKeyed struct {
	key  string
	data json.RawMessage
}

func (r rawKeyed) Key() string                  { return r.key }
func (r rawKeyed) MarshalJSON() ([]byte, error) { return r.data, nil }

func RegisterDevices(api huma.API, store storage.Storage) {
	huma.Register(api, huma.Operation{
		Method:      "GET",
		Path:        "/devices",
		Summary:     "List all devices",
		Description: "Returns all devices across all plugins.",
		Tags:        []string{"devices"},
	}, func(ctx context.Context, _ *struct{}) (*DevicesOutput, error) {
		entries, err := store.Search("*.*")
		if err != nil {
			return nil, huma.Error500InternalServerError("storage search failed", err)
		}
		out := make([]DeviceEntry, 0, len(entries))
		for _, e := range entries {
			// Storage wildcard search is prefix-oriented, so filter to exact
			// two-part keys here to keep /devices from leaking entity rows.
			if strings.Count(e.Key, ".") != 1 {
				continue
			}
			out = append(out, DeviceEntry{Key: e.Key, Data: json.RawMessage(e.Data)})
		}
		return &DevicesOutput{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		Method:  "GET",
		Path:    "/devices/{plugin}/{id}",
		Summary: "Get a device",
		Tags:    []string{"devices"},
	}, func(ctx context.Context, input *struct {
		DeviceKey
	}) (*DeviceOutput, error) {
		data, err := store.Get(input.DeviceKey)
		if err != nil {
			return nil, huma.Error404NotFound("device not found")
		}
		return &DeviceOutput{Body: json.RawMessage(data)}, nil
	})

	huma.Register(api, huma.Operation{
		Method:  "PUT",
		Path:    "/devices/{plugin}/{id}",
		Summary: "Upsert a device",
		Tags:    []string{"devices"},
	}, func(ctx context.Context, input *DeviceInput) (*struct{}, error) {
		dev, err := validateDeviceBody(input.Body, DeviceKey{Plugin: input.Plugin, ID: input.ID})
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("invalid device", err)
		}
		data, err := json.Marshal(dev)
		if err != nil {
			return nil, huma.Error500InternalServerError("marshal failed", err)
		}
		if err := store.Save(rawKeyed{key: input.Plugin + "." + input.ID, data: data}); err != nil {
			return nil, huma.Error500InternalServerError("save failed", err)
		}
		profile := extractDeviceProfileFields(dev)
		if profile != nil {
			deviceKey := DeviceKey{Plugin: input.Plugin, ID: input.ID}
			if err := store.SetProfile(deviceKey, profile); err != nil {
				return nil, huma.Error500InternalServerError("set profile failed", err)
			}
		}
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		Method:      "PATCH",
		Path:        "/devices/{plugin}/{id}/profile",
		Summary:     "Set device profile",
		Description: "Writes user-owned fields (labels, meta, profile) to a sidecar file that is never overwritten by plugin Save() calls.",
		Tags:        []string{"devices"},
	}, func(ctx context.Context, input *DeviceInput) (*struct{}, error) {
		key := DeviceKey{Plugin: input.Plugin, ID: input.ID}
		if err := store.SetProfile(key, input.Body); err != nil {
			return nil, huma.Error500InternalServerError("set profile failed", err)
		}
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		Method:  "DELETE",
		Path:    "/devices/{plugin}/{id}",
		Summary: "Delete a device",
		Tags:    []string{"devices"},
	}, func(ctx context.Context, input *struct {
		DeviceKey
	}) (*struct{}, error) {
		if err := store.Delete(input.DeviceKey); err != nil {
			return nil, huma.Error500InternalServerError("delete failed", err)
		}
		return nil, nil
	})
}
