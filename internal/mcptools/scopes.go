package mcptools

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/slidebolt/sb-api/internal/auth"
)

// toolScopes is the scope each tool needs, matching the REST API: reading
// needs read, acting on devices and running scripts needs control, and
// changing what is stored needs write. A tool missing here needs write.
var toolScopes = map[string]auth.Scope{
	"list_devices":  auth.ScopeRead,
	"get_device":    auth.ScopeRead,
	"list_entities": auth.ScopeRead,
	"get_entity":    auth.ScopeRead,
	"list_scripts":  auth.ScopeRead,
	"get_script":    auth.ScopeRead,
	"list_queries":  auth.ScopeRead,
	"get_query":     auth.ScopeRead,
	"list_logs":     auth.ScopeRead,
	"get_log":       auth.ScopeRead,
	"get_inventory": auth.ScopeRead,
	"phrase_lookup": auth.ScopeRead,

	"send_command":   auth.ScopeControl,
	"start_script":   auth.ScopeControl,
	"stop_script":    auth.ScopeControl,
	"phrase_correct": auth.ScopeControl,

	"create_device": auth.ScopeWrite,
	"create_entity": auth.ScopeWrite,
	"delete_entity": auth.ScopeWrite,
	"save_script":   auth.ScopeWrite,
	"save_query":    auth.ScopeWrite,
	"delete_query":  auth.ScopeWrite,
	"create_timer":  auth.ScopeWrite,
	"delete_timer":  auth.ScopeWrite,
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
