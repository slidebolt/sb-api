package mcptools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"
)

// rawKeyed satisfies storage.Keyed for arbitrary JSON blobs.
type rawKeyed struct {
	key  string
	data json.RawMessage
}

func (r rawKeyed) Key() string              { return r.key }
func (r rawKeyed) MarshalJSON() ([]byte, error) { return r.data, nil }

// New builds an MCP server wired to the SlideBolt storage and messenger.
func New(store storage.Storage, msg messenger.Messenger) *server.MCPServer {
	s := server.NewMCPServer(
		"SlideBolt",
		"0.1.0",
		server.WithToolCapabilities(true),
	)

	s.AddTool(mcp.NewTool("list_devices",
		mcp.WithDescription("List all devices in storage"),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		entries, err := store.Search("*.*")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("search failed: %v", err)), nil
		}
		out, _ := json.Marshal(entries)
		return mcp.NewToolResultText(string(out)), nil
	})

	s.AddTool(mcp.NewTool("create_device",
		mcp.WithDescription("Create or update a device in storage"),
		mcp.WithString("plugin", mcp.Required(), mcp.Description("Plugin ID, e.g. esphome")),
		mcp.WithString("id", mcp.Required(), mcp.Description("Device ID, e.g. living_room")),
		mcp.WithString("data", mcp.Required(), mcp.Description("Device JSON object")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		plugin := req.GetString("plugin", "")
		id := req.GetString("id", "")
		data := req.GetString("data", "")

		if plugin == "" || id == "" || data == "" {
			return mcp.NewToolResultError("plugin, id, and data are required"), nil
		}

		key := plugin + "." + id
		if err := store.Save(rawKeyed{key: key, data: json.RawMessage(data)}); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("save failed: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("device %s saved", key)), nil
	})

	s.AddTool(mcp.NewTool("list_entities",
		mcp.WithDescription("List all entities in storage"),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		entries, err := store.Search("*.*.*")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("search failed: %v", err)), nil
		}
		out, _ := json.Marshal(entries)
		return mcp.NewToolResultText(string(out)), nil
	})

	s.AddTool(mcp.NewTool("create_entity",
		mcp.WithDescription("Create or update an entity in storage"),
		mcp.WithString("plugin", mcp.Required(), mcp.Description("Plugin ID, e.g. esphome")),
		mcp.WithString("device", mcp.Required(), mcp.Description("Device ID, e.g. living_room")),
		mcp.WithString("entity", mcp.Required(), mcp.Description("Entity ID, e.g. light_001")),
		mcp.WithString("data", mcp.Required(), mcp.Description("Entity JSON object")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		plugin := req.GetString("plugin", "")
		device := req.GetString("device", "")
		entity := req.GetString("entity", "")
		data := req.GetString("data", "")

		if plugin == "" || device == "" || entity == "" || data == "" {
			return mcp.NewToolResultError("plugin, device, entity, and data are required"), nil
		}

		key := plugin + "." + device + "." + entity
		if err := store.Save(rawKeyed{key: key, data: json.RawMessage(data)}); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("save failed: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("entity %s saved", key)), nil
	})

	s.AddTool(mcp.NewTool("send_command",
		mcp.WithDescription("Send a command to an entity via NATS"),
		mcp.WithString("plugin", mcp.Required()),
		mcp.WithString("device", mcp.Required()),
		mcp.WithString("entity", mcp.Required()),
		mcp.WithString("action", mcp.Required(), mcp.Description("Command action, e.g. turn_on")),
		mcp.WithString("payload", mcp.Description("Optional JSON command payload")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		plugin := req.GetString("plugin", "")
		device := req.GetString("device", "")
		entity := req.GetString("entity", "")
		action := req.GetString("action", "")
		payload := req.GetString("payload", "{}")

		subject := fmt.Sprintf("%s.%s.%s.command.%s", plugin, device, entity, action)
		if err := msg.Publish(subject, []byte(payload)); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("publish failed: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("command published to %s", subject)), nil
	})

	return s
}
