package routes

import (
	"context"
	"encoding/json"

	"github.com/danielgtaylor/huma/v2"
	domain "github.com/slidebolt/sb-domain"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"
)

// extractProfileFields returns a JSON object with labels/meta/profile from the
// entity if any are present, or nil if none are set.
func extractProfileFields(ent domain.Entity) json.RawMessage {
	profile := make(map[string]any)
	if len(ent.Labels) > 0 {
		profile["labels"] = ent.Labels
	}
	if len(ent.Meta) > 0 {
		profile["meta"] = ent.Meta
	}
	if ent.Profile != nil {
		profile["profile"] = ent.Profile
	}
	if len(profile) == 0 {
		return nil
	}
	data, _ := json.Marshal(profile)
	return data
}

type EntityEntry struct {
	Key  string          `json:"key"`
	Data json.RawMessage `json:"data"`
}

type EntitiesOutput struct {
	Body []EntityEntry
}

type EntityOutput struct {
	Body json.RawMessage
}

type EntityKey struct {
	Plugin   string `path:"plugin"`
	DeviceID string `path:"device"`
	EntityID string `path:"entity"`
}

func (k EntityKey) Key() string { return k.Plugin + "." + k.DeviceID + "." + k.EntityID }

type EntityInput struct {
	Plugin   string `path:"plugin"`
	DeviceID string `path:"device"`
	EntityID string `path:"entity"`
	Body     json.RawMessage
}

type CommandInput struct {
	Plugin   string          `path:"plugin"`
	DeviceID string          `path:"device"`
	EntityID string          `path:"entity"`
	Action   string          `path:"action"`
	Body     json.RawMessage `doc:"Command payload"`
}

func RegisterEntities(api huma.API, store storage.Storage, msg messenger.Messenger) {
	huma.Register(api, huma.Operation{
		Method:      "GET",
		Path:        "/entities",
		Summary:     "List all entities",
		Description: "Returns all entities across all plugins and devices.",
		Tags:        []string{"entities"},
	}, func(ctx context.Context, _ *struct{}) (*EntitiesOutput, error) {
		entries, err := store.Search("*.*.*")
		if err != nil {
			return nil, huma.Error500InternalServerError("storage search failed", err)
		}
		out := make([]EntityEntry, len(entries))
		for i, e := range entries {
			out[i] = EntityEntry{Key: e.Key, Data: json.RawMessage(e.Data)}
		}
		return &EntitiesOutput{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		Method:  "GET",
		Path:    "/entities/{plugin}/{device}/{entity}",
		Summary: "Get an entity",
		Tags:    []string{"entities"},
	}, func(ctx context.Context, input *struct {
		EntityKey
	}) (*EntityOutput, error) {
		data, err := store.Get(input.EntityKey)
		if err != nil {
			return nil, huma.Error404NotFound("entity not found")
		}
		return &EntityOutput{Body: json.RawMessage(data)}, nil
	})

	huma.Register(api, huma.Operation{
		Method:  "PUT",
		Path:    "/entities/{plugin}/{device}/{entity}",
		Summary: "Upsert an entity",
		Tags:    []string{"entities"},
	}, func(ctx context.Context, input *EntityInput) (*struct{}, error) {
		key := input.Plugin + "." + input.DeviceID + "." + input.EntityID
		ent, err := validateEntityBody(input.Body, EntityKey{
			Plugin: input.Plugin, DeviceID: input.DeviceID, EntityID: input.EntityID,
		})
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("invalid entity", err)
		}
		data, err := json.Marshal(ent)
		if err != nil {
			return nil, huma.Error500InternalServerError("marshal failed", err)
		}
		if err := store.Save(rawKeyed{key: key, data: data}); err != nil {
			return nil, huma.Error500InternalServerError("save failed", err)
		}
		// If the body contains user-owned fields, persist them in the sidecar.
		profile := extractProfileFields(ent)
		if profile != nil {
			entityKey := EntityKey{Plugin: input.Plugin, DeviceID: input.DeviceID, EntityID: input.EntityID}
			if err := store.SetProfile(entityKey, profile); err != nil {
				return nil, huma.Error500InternalServerError("set profile failed", err)
			}
		}
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		Method:  "DELETE",
		Path:    "/entities/{plugin}/{device}/{entity}",
		Summary: "Delete an entity",
		Tags:    []string{"entities"},
	}, func(ctx context.Context, input *struct {
		EntityKey
	}) (*struct{}, error) {
		if err := store.Delete(input.EntityKey); err != nil {
			return nil, huma.Error500InternalServerError("delete failed", err)
		}
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		Method:      "PATCH",
		Path:        "/entities/{plugin}/{device}/{entity}/profile",
		Summary:     "Set entity profile",
		Description: "Writes user-owned fields (labels, meta, profile) to a sidecar file that is never overwritten by plugin Save() calls.",
		Tags:        []string{"entities"},
	}, func(ctx context.Context, input *EntityInput) (*struct{}, error) {
		key := EntityKey{Plugin: input.Plugin, DeviceID: input.DeviceID, EntityID: input.EntityID}
		if err := store.SetProfile(key, input.Body); err != nil {
			return nil, huma.Error500InternalServerError("set profile failed", err)
		}
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		Method:      "POST",
		Path:        "/entities/{plugin}/{device}/{entity}/command/{action}",
		Summary:     "Send a command to an entity",
		Description: "Publishes a command to the entity via NATS.",
		Tags:        []string{"commands"},
	}, func(ctx context.Context, input *CommandInput) (*struct{}, error) {
		subject := input.Plugin + "." + input.DeviceID + "." + input.EntityID + ".command." + input.Action
		if err := msg.Publish(subject, input.Body); err != nil {
			return nil, huma.Error500InternalServerError("publish failed", err)
		}
		return nil, nil
	})
}
