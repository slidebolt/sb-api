package mcptools

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/slidebolt/sb-api/internal/auth"
	domain "github.com/slidebolt/sb-domain"
	storage "github.com/slidebolt/sb-storage-sdk"
)

// toolScopes is the scope each tool needs, matching the REST API: reading
// needs read, acting on devices and running scripts needs control, and
// changing what is stored needs write. A tool missing here needs write.
var toolScopes = map[string]auth.Scope{
	"list_devices":    auth.ScopeRead,
	"get_device":      auth.ScopeRead,
	"list_entities":   auth.ScopeRead,
	"get_entity":      auth.ScopeRead,
	"list_scripts":    auth.ScopeAdmin,
	"get_script":      auth.ScopeAdmin,
	"list_queries":    auth.ScopeAdmin,
	"get_query":       auth.ScopeAdmin,
	"list_logs":       auth.ScopeAdmin,
	"get_log":         auth.ScopeAdmin,
	"get_inventory":   auth.ScopeRead,
	"phrase_lookup":   auth.ScopeAdmin,
	"browse_tv_guide": auth.ScopeRead,

	"send_command":      auth.ScopeControl,
	"start_script":      auth.ScopeAdmin,
	"stop_script":       auth.ScopeAdmin,
	"phrase_correct":    auth.ScopeAdmin,
	"record_tv_program": auth.ScopeControl,

	"create_device":   auth.ScopeWrite,
	"create_entity":   auth.ScopeWrite,
	"delete_entity":   auth.ScopeWrite,
	"save_script":     auth.ScopeWrite,
	"save_query":      auth.ScopeWrite,
	"delete_query":    auth.ScopeWrite,
	"create_schedule": auth.ScopeWrite,
	"delete_schedule": auth.ScopeWrite,
}

func toolScope(name string) auth.Scope {
	if s, ok := toolScopes[name]; ok {
		return s
	}
	return auth.ScopeWrite
}

// allowed reports whether the caller may use a tool. Calls with no scopes in
// the context did not come through the HTTP auth middleware (in-process use),
// and are allowed.
func allowed(ctx context.Context, name string) bool {
	scopes, ok := auth.ScopesFromContext(ctx)
	return !ok || auth.HasScope(scopes, toolScope(name))
}

func requireToolScope(next server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if !allowed(ctx, req.Params.Name) {
			return mcp.NewToolResultError("token missing required scope: " + string(toolScope(req.Params.Name))), nil
		}
		return next(ctx, req)
	}
}

// visibleTools lists only the tools the caller may use, so an agent is not
// offered tools that will be refused.
func visibleTools(ctx context.Context, tools []mcp.Tool) []mcp.Tool {
	out := make([]mcp.Tool, 0, len(tools))
	for _, t := range tools {
		if allowed(ctx, t.Name) {
			out = append(out, t)
		}
	}
	return out
}

func allowedResource(ctx context.Context, data []byte) bool {
	group, ok := auth.ResourceSecurityGroup(data)
	return (ok && auth.AllowsGroup(ctx, group)) || (!ok && auth.AllowsAllGroups(ctx))
}

func filterAllowedEntries(ctx context.Context, entries []storage.Entry) []storage.Entry {
	out := make([]storage.Entry, 0, len(entries))
	for _, entry := range entries {
		if allowedResource(ctx, entry.Data) {
			var envelope map[string]any
			if json.Unmarshal(entry.Data, &envelope) == nil {
				if _, exists := envelope["securityGroup"]; !exists {
					group, _ := auth.ResourceSecurityGroup(entry.Data)
					envelope["securityGroup"] = group
					entry.Data, _ = json.Marshal(envelope)
				}
			}
			out = append(out, entry)
		}
	}
	return out
}

func deviceGroups(store storage.Storage, data []byte) ([]string, bool) {
	var device domain.Device
	if json.Unmarshal(data, &device) != nil || device.Plugin == "" || device.ID == "" {
		return nil, false
	}
	entries, err := store.Search(device.Plugin + "." + device.ID + ".*")
	if err != nil {
		return nil, false
	}
	entities := make([]domain.Entity, 0, len(entries))
	for _, entry := range entries {
		var entity domain.Entity
		if json.Unmarshal(entry.Data, &entity) == nil && entity.Type != "" {
			entities = append(entities, entity)
		}
	}
	groups, err := domain.DeviceSecurityGroups(device, entities...)
	return groups, err == nil
}

func allowedDevice(ctx context.Context, store storage.Storage, data []byte) bool {
	groups, ok := deviceGroups(store, data)
	if !ok {
		return false
	}
	for _, group := range groups {
		if auth.AllowsGroup(ctx, group) {
			return true
		}
	}
	return false
}

func projectDevice(ctx context.Context, store storage.Storage, data []byte) []byte {
	var device domain.Device
	if json.Unmarshal(data, &device) != nil {
		return data
	}
	groups, ok := deviceGroups(store, data)
	if !ok {
		return data
	}
	device.SecurityGroups = device.SecurityGroups[:0]
	for _, group := range groups {
		if auth.AllowsGroup(ctx, group) {
			device.SecurityGroups = append(device.SecurityGroups, group)
		}
	}
	visible := make([]domain.Entity, 0, len(device.Entities))
	for _, entity := range device.Entities {
		group := entity.SecurityGroup
		if group == "" {
			group = domain.DefaultSecurityGroup(entity.Type)
			entity.SecurityGroup = group
		}
		if auth.AllowsGroup(ctx, group) {
			visible = append(visible, entity)
		}
	}
	device.Entities = visible
	projected, _ := json.Marshal(device)
	return projected
}
