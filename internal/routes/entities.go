package routes

import (
	"context"
	"encoding/json"

	"github.com/danielgtaylor/huma/v2"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"
)

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
