package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

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
		server.WithInstructions(`SlideBolt is a home-automation runtime for managing IoT devices, entities, and Lua automation scripts.

The user invokes SlideBolt tools by prefixing their request with "sb". Examples:
  - "sb list entities" → call list_entities
  - "sb turn the basement lights on" → call send_command with action "turn_on" targeting the appropriate entity
  - "sb run the partytime script" → call start_script with name "PartyTime"
  - "sb show me all entities tagged with Light" → call list_entities then filter by type/tag
  - "sb what devices are online" → call list_devices
  - "sb stop all scripts" → call list_scripts to find running instances, then stop_script for each
  - "sb save a new script called sunrise" → call save_script with the provided Lua source
  - "sb set brightness to 50 on kitchen light" → call send_command with action and JSON payload

Entity keys follow the pattern plugin.device.entity (e.g. plugin-automation.group.basementedison).
Command NATS subjects are plugin.device.entity.command.action.
Scripts are Lua and managed under the sb-script.scripts.* keyspace.`),
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

	s.AddTool(mcp.NewTool("delete_entity",
		mcp.WithDescription("Delete an entity from storage"),
		mcp.WithString("plugin", mcp.Required(), mcp.Description("Plugin ID, e.g. esphome")),
		mcp.WithString("device", mcp.Required(), mcp.Description("Device ID, e.g. living_room")),
		mcp.WithString("entity", mcp.Required(), mcp.Description("Entity ID, e.g. light_001")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		plugin := req.GetString("plugin", "")
		device := req.GetString("device", "")
		entity := req.GetString("entity", "")

		if plugin == "" || device == "" || entity == "" {
			return mcp.NewToolResultError("plugin, device, and entity are required"), nil
		}

		type simpleKey struct{ k string }
		key := plugin + "." + device + "." + entity
		if err := store.Delete(rawKeyed{key: key}); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("delete failed: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("entity %s deleted", key)), nil
	})

	s.AddTool(mcp.NewTool("list_scripts",
		mcp.WithDescription("List all saved Lua scripts with their running instances"),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		defEntries, err := store.Search("sb-script.scripts.>")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("search scripts failed: %v", err)), nil
		}
		instEntries, err := store.Search("sb-script.instances.>")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("search instances failed: %v", err)), nil
		}
		type scriptOut struct {
			Name      string            `json:"name"`
			Source    string            `json:"source,omitempty"`
			Running   bool              `json:"running"`
			Instances []json.RawMessage `json:"instances,omitempty"`
		}
		byName := map[string]*scriptOut{}
		for _, e := range defEntries {
			var def struct {
				Name   string `json:"name"`
				Source string `json:"source"`
			}
			if err := json.Unmarshal(e.Data, &def); err != nil || def.Name == "" {
				continue
			}
			byName[def.Name] = &scriptOut{Name: def.Name, Source: def.Source}
		}
		for _, e := range instEntries {
			var inst struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(e.Data, &inst); err != nil || inst.Name == "" {
				continue
			}
			s, ok := byName[inst.Name]
			if !ok {
				s = &scriptOut{Name: inst.Name}
				byName[inst.Name] = s
			}
			s.Running = true
			s.Instances = append(s.Instances, e.Data)
		}
		scripts := make([]scriptOut, 0, len(byName))
		for _, s := range byName {
			scripts = append(scripts, *s)
		}
		out, _ := json.Marshal(scripts)
		return mcp.NewToolResultText(string(out)), nil
	})

	s.AddTool(mcp.NewTool("get_script",
		mcp.WithDescription("Get a specific script definition and its running instances"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Script name")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := req.GetString("name", "")
		if name == "" {
			return mcp.NewToolResultError("name is required"), nil
		}
		defEntries, err := store.Search("sb-script.scripts.>")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("search failed: %v", err)), nil
		}
		var found *struct {
			Name   string `json:"name"`
			Source string `json:"source"`
		}
		for _, e := range defEntries {
			var def struct {
				Name   string `json:"name"`
				Source string `json:"source"`
			}
			if err := json.Unmarshal(e.Data, &def); err == nil && def.Name == name {
				found = &def
				break
			}
		}
		if found == nil {
			return mcp.NewToolResultError(fmt.Sprintf("script %q not found", name)), nil
		}
		instEntries, err := store.Search("sb-script.instances.>")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("search instances failed: %v", err)), nil
		}
		var instances []json.RawMessage
		for _, e := range instEntries {
			var inst struct{ Name string `json:"name"` }
			if err := json.Unmarshal(e.Data, &inst); err == nil && inst.Name == name {
				instances = append(instances, e.Data)
			}
		}
		result := map[string]any{
			"name":      found.Name,
			"source":    found.Source,
			"running":   len(instances) > 0,
			"instances": instances,
		}
		out, _ := json.Marshal(result)
		return mcp.NewToolResultText(string(out)), nil
	})

	s.AddTool(mcp.NewTool("save_script",
		mcp.WithDescription("Save or update a Lua script definition"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Script name")),
		mcp.WithString("source", mcp.Required(), mcp.Description("Lua source code")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := req.GetString("name", "")
		source := req.GetString("source", "")
		if name == "" || source == "" {
			return mcp.NewToolResultError("name and source are required"), nil
		}
		body, _ := json.Marshal(map[string]string{
			"type":     "script",
			"language": "lua",
			"name":     name,
			"source":   source,
		})
		if err := store.Save(rawKeyed{key: "sb-script.scripts." + name, data: body}); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("save failed: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("script %s saved", name)), nil
	})

	s.AddTool(mcp.NewTool("start_script",
		mcp.WithDescription("Start a script instance via NATS request/reply"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Script name")),
		mcp.WithString("queryRef", mcp.Description("Optional query reference for targeting")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := req.GetString("name", "")
		if name == "" {
			return mcp.NewToolResultError("name is required"), nil
		}
		queryRef := req.GetString("queryRef", "")
		payload := map[string]any{"name": name}
		if queryRef != "" {
			payload["queryRef"] = queryRef
		}
		data, _ := json.Marshal(payload)
		respMsg, err := msg.Request("script.start", data, 5*time.Second)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("script start request failed: %v", err)), nil
		}
		var resp struct {
			OK    bool   `json:"ok"`
			Hash  string `json:"hash,omitempty"`
			Error string `json:"error,omitempty"`
		}
		if err := json.Unmarshal(respMsg.Data, &resp); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid response: %v", err)), nil
		}
		if !resp.OK {
			return mcp.NewToolResultError(fmt.Sprintf("script engine error: %s", resp.Error)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("script %s started with hash %s", name, resp.Hash)), nil
	})

	s.AddTool(mcp.NewTool("stop_script",
		mcp.WithDescription("Stop a specific script instance"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Script name")),
		mcp.WithString("hash", mcp.Required(), mcp.Description("Instance hash")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := req.GetString("name", "")
		hash := req.GetString("hash", "")
		if name == "" || hash == "" {
			return mcp.NewToolResultError("name and hash are required"), nil
		}
		data, _ := json.Marshal(map[string]string{"name": name, "hash": hash})
		respMsg, err := msg.Request("script.stop", data, 5*time.Second)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("script stop request failed: %v", err)), nil
		}
		var resp struct {
			OK    bool   `json:"ok"`
			Error string `json:"error,omitempty"`
		}
		if err := json.Unmarshal(respMsg.Data, &resp); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid response: %v", err)), nil
		}
		if !resp.OK {
			return mcp.NewToolResultError(fmt.Sprintf("script engine error: %s", resp.Error)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("script %s instance %s stopped", name, hash)), nil
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
