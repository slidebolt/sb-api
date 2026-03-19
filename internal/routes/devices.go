package routes

import (
	"context"
	"encoding/json"
	"strings"

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
	Plugin string          `path:"plugin"`
	ID     string          `path:"id"`
	Body   json.RawMessage
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

func (r rawKeyed) Key() string                         { return r.key }
func (r rawKeyed) MarshalJSON() ([]byte, error)        { return r.data, nil }

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
		if err := store.Save(rawKeyed{key: input.Plugin + "." + input.ID, data: input.Body}); err != nil {
			return nil, huma.Error500InternalServerError("save failed", err)
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
