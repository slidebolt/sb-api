package routes

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/slidebolt/sb-api/internal/commands"
	apitrc "github.com/slidebolt/sb-api/internal/trace"
	domain "github.com/slidebolt/sb-domain"
	logging "github.com/slidebolt/sb-logging-sdk"
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

// storedEntity returns data only when both the storage key and payload satisfy
// the public entity contract. State storage also contains scripts, queries,
// runtime records, and credentials; those are not entities and must never leak
// through this API merely because their keys have three segments.
func storedEntity(key string, data []byte) (json.RawMessage, bool) {
	parts := strings.Split(key, ".")
	if len(parts) != 3 {
		return nil, false
	}
	if _, err := validateEntityBody(data, EntityKey{Plugin: parts[0], DeviceID: parts[1], EntityID: parts[2]}); err != nil {
		return nil, false
	}
	return json.RawMessage(data), true
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

func RegisterEntities(api huma.API, store storage.Storage, msg messenger.Messenger, logger logging.Store) {
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
		out := make([]EntityEntry, 0, len(entries))
		for _, e := range entries {
			if data, ok := storedEntity(e.Key, e.Data); ok {
				out = append(out, EntityEntry{Key: e.Key, Data: data})
			}
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
		entity, ok := storedEntity(input.EntityKey.Key(), data)
		if !ok {
			return nil, huma.Error404NotFound("entity not found")
		}
		return &EntityOutput{Body: entity}, nil
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
		merged, empty, err := mergeProfilePatch(store, key, input.Body)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid profile patch", err)
		}
		if empty {
			if err := store.DeleteFile(storage.Profile, key); err != nil {
				return nil, huma.Error500InternalServerError("delete profile failed", err)
			}
			return nil, nil
		}
		if err := store.SetProfile(key, merged); err != nil {
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
		ctx, traceID := apitrc.Ensure(ctx)
		subject := input.Plugin + "." + input.DeviceID + "." + input.EntityID + ".command." + input.Action
		target := input.Plugin + "." + input.DeviceID + "." + input.EntityID
		apitrc.AppendLog(ctx, logger, "sb-api", "api.command.request.received", "info", "API received entity command", traceID, map[string]any{
			"subject": subject,
			"plugin":  input.Plugin,
			"device":  input.DeviceID,
			"entity":  input.EntityID,
			"target":  target,
			"action":  input.Action,
		})

		// The entity must exist and list the action, and a payload for an
		// action sb-domain knows must decode and pass its Validate. Plugin
		// specific actions are accepted when the entity lists them. Extra
		// payload fields are tolerated here (strict checking is for agents).
		if _, err := commands.Validate(store, target, input.Action, input.Body, false); err != nil {
			apitrc.AppendLog(ctx, logger, "sb-api", "api.command.rejected", "warn", "API rejected entity command", traceID, map[string]any{
				"target": target, "action": input.Action, "reason": err.Error(),
			})
			return nil, huma.Error400BadRequest(err.Error())
		}

		headers := apitrc.MessageHeaders(traceID, "sb-api", target, input.Action)
		if err := msg.PublishWithHeaders(subject, input.Body, headers); err != nil {
			return nil, huma.Error500InternalServerError("publish failed", err)
		}
		apitrc.AppendLog(ctx, logger, "sb-api", "api.command.published", "info", "API published entity command", traceID, map[string]any{
			"subject": subject,
			"plugin":  input.Plugin,
			"device":  input.DeviceID,
			"entity":  input.EntityID,
			"action":  input.Action,
		})
		return nil, nil
	})
}
