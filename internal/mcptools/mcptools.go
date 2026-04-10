package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	apitrc "github.com/slidebolt/sb-api/internal/trace"
	logging "github.com/slidebolt/sb-logging-sdk"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"
)

// rawKeyed satisfies storage.Keyed for arbitrary JSON blobs.
type rawKeyed struct {
	key  string
	data json.RawMessage
}

func (r rawKeyed) Key() string                  { return r.key }
func (r rawKeyed) MarshalJSON() ([]byte, error) { return r.data, nil }

const (
	scriptDefinitionTypeScript     = "script"
	scriptDefinitionTypeAutomation = "automation"
)

// New builds an MCP server wired to the SlideBolt storage and messenger.
func New(store storage.Storage, msg messenger.Messenger, logs logging.Store) *server.MCPServer {
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
  - "sb save a new automation called sunrise" → call save_script with the provided Lua source and type "automation"
  - "sb set brightness to 50 on kitchen light" → call send_command with action and JSON payload

Entity keys follow the pattern plugin.device.entity (e.g. plugin-automation.group.basementedison).
Command NATS subjects are plugin.device.entity.command.action.
Scripts are Lua and managed under the sb-script.scripts.* keyspace. Definition type "script" means manual start only. Definition type "automation" means sb-script auto-starts it on service startup.
Queries are named, reusable search definitions stored under sb-query.queries.* and referenced by name (queryRef) in scripts and automations.`),
	)

	s.AddTool(mcp.NewTool("list_devices",
		mcp.WithDescription("List all devices in storage"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		entries, err := store.Search("*.*")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("search failed: %v", err)), nil
		}
		out, _ := json.Marshal(entries)
		return mcp.NewToolResultText(string(out)), nil
	})

	if logs != nil {
		s.AddTool(mcp.NewTool("list_logs",
			mcp.WithDescription("List stored log events using simple exact-match filters"),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(false),
			mcp.WithString("since", mcp.Description("Optional RFC3339 lower-bound timestamp")),
			mcp.WithString("until", mcp.Description("Optional RFC3339 upper-bound timestamp")),
			mcp.WithString("source", mcp.Description("Optional exact source match")),
			mcp.WithString("kind", mcp.Description("Optional exact kind match")),
			mcp.WithString("level", mcp.Description("Optional exact level match")),
			mcp.WithString("plugin", mcp.Description("Optional exact plugin match")),
			mcp.WithString("device", mcp.Description("Optional exact device match")),
			mcp.WithString("entity", mcp.Description("Optional exact entity match")),
			mcp.WithString("trace_id", mcp.Description("Optional exact trace ID match")),
			mcp.WithString("limit", mcp.Description("Optional max number of events")),
		), listLogsHandler(logs))

		s.AddTool(mcp.NewTool("get_log",
			mcp.WithDescription("Get a single stored log event by ID"),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(false),
			mcp.WithString("id", mcp.Required(), mcp.Description("Log event ID")),
		), getLogHandler(logs))
	}

	s.AddTool(mcp.NewTool("create_device",
		mcp.WithDescription("Create or update a device in storage"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
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

	s.AddTool(mcp.NewTool("get_device",
		mcp.WithDescription("Get a single device by plugin and device ID"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("plugin", mcp.Required(), mcp.Description("Plugin ID, e.g. plugin-esphome")),
		mcp.WithString("device", mcp.Required(), mcp.Description("Device ID, e.g. switch_basement_edison")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		plugin := req.GetString("plugin", "")
		device := req.GetString("device", "")
		if plugin == "" || device == "" {
			return mcp.NewToolResultError("plugin and device are required"), nil
		}
		key := plugin + "." + device
		data, err := store.Get(rawKeyed{key: key})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("get failed: %v", err)), nil
		}
		out := map[string]any{"key": key, "data": json.RawMessage(data)}
		result, _ := json.Marshal(out)
		return mcp.NewToolResultText(string(result)), nil
	})

	s.AddTool(mcp.NewTool("list_entities",
		mcp.WithDescription("List all entities in storage"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		entries, err := store.Search("*.*.*")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("search failed: %v", err)), nil
		}
		out, _ := json.Marshal(entries)
		return mcp.NewToolResultText(string(out)), nil
	})

	s.AddTool(mcp.NewTool("get_entity",
		mcp.WithDescription("Get a single entity by its full key (plugin.device.entity)"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("plugin", mcp.Required(), mcp.Description("Plugin ID, e.g. plugin-esphome")),
		mcp.WithString("device", mcp.Required(), mcp.Description("Device ID, e.g. basement-09-edison-01")),
		mcp.WithString("entity", mcp.Required(), mcp.Description("Entity ID, e.g. basement-09-edison-01_668929361")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		plugin := req.GetString("plugin", "")
		device := req.GetString("device", "")
		entity := req.GetString("entity", "")
		if plugin == "" || device == "" || entity == "" {
			return mcp.NewToolResultError("plugin, device, and entity are required"), nil
		}
		key := plugin + "." + device + "." + entity
		data, err := store.Get(rawKeyed{key: key})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("get failed: %v", err)), nil
		}
		out := map[string]any{"key": key, "data": json.RawMessage(data)}
		result, _ := json.Marshal(out)
		return mcp.NewToolResultText(string(result)), nil
	})

	s.AddTool(mcp.NewTool("create_entity",
		mcp.WithDescription("Create or update an entity in storage"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
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
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
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
		mcp.WithDescription("List all saved Lua definitions with their type and running instances"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
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
			Type      string            `json:"type"`
			Name      string            `json:"name"`
			Source    string            `json:"source,omitempty"`
			Running   bool              `json:"running"`
			Instances []json.RawMessage `json:"instances,omitempty"`
		}
		byName := map[string]*scriptOut{}
		for _, e := range defEntries {
			var def struct {
				Type   string `json:"type"`
				Name   string `json:"name"`
				Source string `json:"source"`
			}
			if err := json.Unmarshal(e.Data, &def); err != nil || def.Name == "" {
				continue
			}
			byName[def.Name] = &scriptOut{Type: displayScriptDefinitionType(def.Type), Name: def.Name, Source: def.Source}
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
		mcp.WithDescription("Get a specific Lua definition with its type and running instances"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
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
			Type   string `json:"type"`
			Name   string `json:"name"`
			Source string `json:"source"`
		}
		for _, e := range defEntries {
			var def struct {
				Type   string `json:"type"`
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
			var inst struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(e.Data, &inst); err == nil && inst.Name == name {
				instances = append(instances, e.Data)
			}
		}
		result := map[string]any{
			"type":      displayScriptDefinitionType(found.Type),
			"name":      found.Name,
			"source":    found.Source,
			"running":   len(instances) > 0,
			"instances": instances,
		}
		out, _ := json.Marshal(result)
		return mcp.NewToolResultText(string(out)), nil
	})

	s.AddTool(mcp.NewTool("save_script",
		mcp.WithDescription("Save or update a Lua definition. Type script is manual-start only. Type automation auto-starts when sb-script boots."),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("name", mcp.Required(), mcp.Description("Script name")),
		mcp.WithString("type", mcp.Description("Optional definition type: script or automation. Defaults to script.")),
		mcp.WithString("source", mcp.Required(), mcp.Description("Lua source code")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := req.GetString("name", "")
		defType, err := normalizeScriptDefinitionType(req.GetString("type", ""))
		source := req.GetString("source", "")
		if name == "" || source == "" {
			return mcp.NewToolResultError("name and source are required"), nil
		}
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		body, _ := json.Marshal(map[string]string{
			"type":     defType,
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
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("name", mcp.Required(), mcp.Description("Script name")),
		mcp.WithString("queryRef", mcp.Description("Optional query reference for targeting")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		ctx, traceID := apitrc.Ensure(ctx)
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
		headers := apitrc.MessageHeaders(traceID, "sb-api", "script.start", "script.start")
		apitrc.AppendLog(ctx, logs, "sb-api", "mcp.script.request", "info", "MCP requested script start", traceID, map[string]any{"name": name, "queryRef": queryRef})
		respMsg, err := msg.RequestWithHeaders("script.start", data, headers, 5*time.Second)
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
		apitrc.AppendLog(ctx, logs, "sb-api", "mcp.script.started", "info", "MCP started script", traceID, map[string]any{"name": name, "hash": resp.Hash})
		return mcp.NewToolResultText(fmt.Sprintf("script %s started with hash %s", name, resp.Hash)), nil
	})

	s.AddTool(mcp.NewTool("stop_script",
		mcp.WithDescription("Stop a specific script instance"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("name", mcp.Required(), mcp.Description("Script name")),
		mcp.WithString("hash", mcp.Required(), mcp.Description("Instance hash")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		ctx, traceID := apitrc.Ensure(ctx)
		name := req.GetString("name", "")
		hash := req.GetString("hash", "")
		if name == "" || hash == "" {
			return mcp.NewToolResultError("name and hash are required"), nil
		}
		data, _ := json.Marshal(map[string]string{"name": name, "hash": hash})
		headers := apitrc.MessageHeaders(traceID, "sb-api", "script.stop", "script.stop")
		apitrc.AppendLog(ctx, logs, "sb-api", "mcp.script.request", "info", "MCP requested script stop", traceID, map[string]any{"name": name, "hash": hash})
		respMsg, err := msg.RequestWithHeaders("script.stop", data, headers, 5*time.Second)
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
		apitrc.AppendLog(ctx, logs, "sb-api", "mcp.script.stopped", "info", "MCP stopped script", traceID, map[string]any{"name": name, "hash": hash})
		return mcp.NewToolResultText(fmt.Sprintf("script %s instance %s stopped", name, hash)), nil
	})

	s.AddTool(mcp.NewTool("list_queries",
		mcp.WithDescription("List all saved query definitions"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		defs, err := storage.ListQueryDefinitions(store)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("list queries failed: %v", err)), nil
		}
		out, _ := json.Marshal(defs)
		return mcp.NewToolResultText(string(out)), nil
	})

	s.AddTool(mcp.NewTool("get_query",
		mcp.WithDescription("Get a single query definition by name"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("name", mcp.Required(), mcp.Description("Query name, e.g. basement_lights")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := req.GetString("name", "")
		if name == "" {
			return mcp.NewToolResultError("name is required"), nil
		}
		data, err := store.Get(storage.QueryKey{Name: name})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("get query failed: %v", err)), nil
		}
		out := map[string]any{"name": name, "data": json.RawMessage(data)}
		result, _ := json.Marshal(out)
		return mcp.NewToolResultText(string(result)), nil
	})

	s.AddTool(mcp.NewTool("save_query",
		mcp.WithDescription("Save or update a named query definition"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("name", mcp.Required(), mcp.Description("Query name, e.g. basement_lights")),
		mcp.WithString("pattern", mcp.Description("Key pattern for matching, e.g. > or plugin.device.*")),
		mcp.WithString("where", mcp.Description("JSON array of filters, e.g. [{\"field\":\"labels.Area\",\"op\":\"eq\",\"value\":\"Basement\"}]")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := req.GetString("name", "")
		if name == "" {
			return mcp.NewToolResultError("name is required"), nil
		}
		pattern := req.GetString("pattern", "")
		var filters []storage.Filter
		if whereStr := req.GetString("where", ""); whereStr != "" {
			if err := json.Unmarshal([]byte(whereStr), &filters); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("invalid where JSON: %v", err)), nil
			}
		}
		q := storage.Query{Pattern: pattern, Where: filters}
		if err := storage.SaveQueryDefinition(store, name, q); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("save query failed: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("query %s saved", name)), nil
	})

	s.AddTool(mcp.NewTool("delete_query",
		mcp.WithDescription("Delete a named query definition"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("name", mcp.Required(), mcp.Description("Query name to delete")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := req.GetString("name", "")
		if name == "" {
			return mcp.NewToolResultError("name is required"), nil
		}
		if err := store.Delete(storage.QueryKey{Name: name}); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("delete query failed: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("query %s deleted", name)), nil
	})

	s.AddTool(mcp.NewTool("send_command",
		mcp.WithDescription("Send a command to an entity via NATS"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("plugin", mcp.Required()),
		mcp.WithString("device", mcp.Required()),
		mcp.WithString("entity", mcp.Required()),
		mcp.WithString("action", mcp.Required(), mcp.Description("Command action, e.g. turn_on")),
		mcp.WithString("payload", mcp.Description("Optional JSON command payload")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		ctx, traceID := apitrc.Ensure(ctx)
		plugin := req.GetString("plugin", "")
		device := req.GetString("device", "")
		entity := req.GetString("entity", "")
		action := req.GetString("action", "")
		payload := req.GetString("payload", "{}")

		subject := fmt.Sprintf("%s.%s.%s.command.%s", plugin, device, entity, action)
		headers := apitrc.MessageHeaders(traceID, "sb-api", plugin+"."+device+"."+entity, action)
		if err := msg.PublishWithHeaders(subject, []byte(payload), headers); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("publish failed: %v", err)), nil
		}
		apitrc.AppendLog(ctx, logs, "sb-api", "mcp.command.published", "info", "MCP published entity command", traceID, map[string]any{
			"subject": subject,
			"plugin":  plugin,
			"device":  device,
			"entity":  entity,
			"action":  action,
		})
		return mcp.NewToolResultText(fmt.Sprintf("command published to %s", subject)), nil
	})

	return s
}

func listLogsHandler(logs logging.Store) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if logs == nil {
			return mcp.NewToolResultError("logging store not configured"), nil
		}
		listReq, err := parseListLogsRequest(req)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		events, err := logs.List(ctx, listReq)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("list logs failed: %v", err)), nil
		}
		out, _ := json.Marshal(events)
		return mcp.NewToolResultText(string(out)), nil
	}
}

func getLogHandler(logs logging.Store) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if logs == nil {
			return mcp.NewToolResultError("logging store not configured"), nil
		}
		id := strings.TrimSpace(req.GetString("id", ""))
		if id == "" {
			return mcp.NewToolResultError("id is required"), nil
		}
		event, err := logs.Get(ctx, id)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("get log failed: %v", err)), nil
		}
		out, _ := json.Marshal(event)
		return mcp.NewToolResultText(string(out)), nil
	}
}

func parseListLogsRequest(req mcp.CallToolRequest) (logging.ListRequest, error) {
	var out logging.ListRequest
	var err error
	if raw := strings.TrimSpace(req.GetString("since", "")); raw != "" {
		out.Since, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return logging.ListRequest{}, fmt.Errorf("invalid since: %w", err)
		}
	}
	if raw := strings.TrimSpace(req.GetString("until", "")); raw != "" {
		out.Until, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return logging.ListRequest{}, fmt.Errorf("invalid until: %w", err)
		}
	}
	out.Source = strings.TrimSpace(req.GetString("source", ""))
	out.Kind = strings.TrimSpace(req.GetString("kind", ""))
	out.Level = strings.TrimSpace(req.GetString("level", ""))
	out.Plugin = strings.TrimSpace(req.GetString("plugin", ""))
	out.Device = strings.TrimSpace(req.GetString("device", ""))
	out.Entity = strings.TrimSpace(req.GetString("entity", ""))
	out.TraceID = strings.TrimSpace(req.GetString("trace_id", ""))
	if raw := strings.TrimSpace(req.GetString("limit", "")); raw != "" {
		out.Limit, err = strconv.Atoi(raw)
		if err != nil {
			return logging.ListRequest{}, fmt.Errorf("invalid limit: %w", err)
		}
	}
	out.Normalize()
	return out, nil
}

func normalizeScriptDefinitionType(raw string) (string, error) {
	switch raw {
	case "", scriptDefinitionTypeScript:
		return scriptDefinitionTypeScript, nil
	case scriptDefinitionTypeAutomation:
		return scriptDefinitionTypeAutomation, nil
	default:
		return "", fmt.Errorf("invalid script type %q: must be %q or %q", raw, scriptDefinitionTypeScript, scriptDefinitionTypeAutomation)
	}
}

func displayScriptDefinitionType(raw string) string {
	if raw == "" {
		return scriptDefinitionTypeScript
	}
	return raw
}
