package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/slidebolt/sb-api/internal/commands"
	domain "github.com/slidebolt/sb-domain"
	media "github.com/slidebolt/sb-media-sdk"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"
)

func registerMediaTools(s *server.MCPServer, store storage.Storage, msg messenger.Messenger, sender *commands.Sender) {
	client := media.NewClient(msg)
	s.AddTool(mcp.NewTool("browse_tv_guide",
		mcp.WithDescription("Search a provider's television guide. Use this instead of searching entities for programs. Provider and server come from a tv_channel or media_server entity. Times are RFC3339; without them the window is now through six hours from now."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("provider", mcp.Required(), mcp.Description("Provider plugin ID, e.g. plugin-plex")),
		mcp.WithString("server", mcp.Required(), mcp.Description("Media server ID")),
		mcp.WithString("start", mcp.Description("RFC3339 window start; defaults to five minutes ago")),
		mcp.WithString("end", mcp.Description("RFC3339 window end; defaults to six hours from now")),
		mcp.WithString("query", mcp.Description("Program title or episode-title search")),
		mcp.WithString("channel", mcp.Description("Optional channel ID or number")),
		mcp.WithNumber("limit", mcp.Description("Maximum programs, default 100 and maximum 500")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		provider := strings.TrimSpace(req.GetString("provider", ""))
		serverID := strings.TrimSpace(req.GetString("server", ""))
		if provider == "" || serverID == "" {
			return mcp.NewToolResultError("provider and server are required"), nil
		}
		if !mcpProviderAllowed(ctx, store, provider, serverID) {
			return mcp.NewToolResultError("television provider not found"), nil
		}
		now := time.Now()
		start, err := optionalTime(req.GetString("start", ""), now.Add(-5*time.Minute))
		if err != nil {
			return mcp.NewToolResultError("invalid start: " + err.Error()), nil
		}
		end, err := optionalTime(req.GetString("end", ""), now.Add(6*time.Hour))
		if err != nil {
			return mcp.NewToolResultError("invalid end: " + err.Error()), nil
		}
		if !end.After(start) {
			return mcp.NewToolResultError("end must be after start"), nil
		}
		limit := int(req.GetFloat("limit", 100))
		if limit <= 0 || limit > 500 {
			limit = 100
		}
		page, err := client.Guide(provider, media.GuideRequest{ServerID: serverID, Start: start, End: end, Channel: strings.TrimSpace(req.GetString("channel", "")), Query: strings.TrimSpace(req.GetString("query", "")), Limit: limit})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("guide provider unavailable: %v", err)), nil
		}
		if page.Error != "" {
			return mcp.NewToolResultError(page.Error), nil
		}
		out, _ := json.Marshal(page)
		return mcp.NewToolResultText(string(out)), nil
	})

	s.AddTool(mcp.NewTool("record_tv_program",
		mcp.WithDescription("Create a one-time recording from a program returned by browse_tv_guide. This discovers the provider's recorder, sends the complete generic record_start payload, and verifies that a matching schedule appears."),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("provider", mcp.Required(), mcp.Description("Provider plugin ID from providerRef.provider")),
		mcp.WithString("server", mcp.Required(), mcp.Description("Server ID from providerRef.serverID")),
		mcp.WithString("airing_id", mcp.Required(), mcp.Description("Airing ID returned by browse_tv_guide")),
		mcp.WithString("item_id", mcp.Description("Optional providerRef.itemID returned by browse_tv_guide")),
		mcp.WithString("request", mcp.Description("The person's original words")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		provider := strings.TrimSpace(req.GetString("provider", ""))
		serverID := strings.TrimSpace(req.GetString("server", ""))
		airingID := strings.TrimSpace(req.GetString("airing_id", ""))
		if provider == "" || serverID == "" || airingID == "" {
			return mcp.NewToolResultError("provider, server, and airing_id are required"), nil
		}
		if !mcpProviderAllowed(ctx, store, provider, serverID) {
			return mcp.NewToolResultError("recorder not found"), nil
		}
		recorder, err := findRecorder(store, provider, serverID)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		payload, _ := json.Marshal(domain.RecordStart{
			Source:   domain.PlayableSource{Kind: "item", Item: &domain.MediaItemRef{Provider: provider, ServerID: serverID, ItemID: strings.TrimSpace(req.GetString("item_id", ""))}},
			Metadata: map[string]json.RawMessage{"airingID": json.RawMessage(strconvQuote(airingID))},
		})
		res, err := sender.Send(ctx, commands.Request{Plugin: provider, Device: serverID, Entity: recorder.ID, Action: "record_start", Payload: payload, Words: strings.TrimSpace(req.GetString("request", "")), Strict: true})
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if res.Rejection != nil {
			return mcp.NewToolResultError(res.Text()), nil
		}
		if schedule := waitForAiringSchedule(ctx, store, provider, serverID, airingID, 5*time.Second); schedule != "" {
			return mcp.NewToolResultText("verified: one-time recording scheduled as " + schedule), nil
		}
		return mcp.NewToolResultText("sent but not yet confirmed: the recorder accepted the request but no matching schedule appeared within 5 seconds"), nil
	})
}

func mcpProviderAllowed(ctx context.Context, store storage.Storage, provider, server string) bool {
	entries, err := store.Search(provider + "." + server + ".*")
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if allowedResource(ctx, entry.Data) {
			return true
		}
	}
	return false
}

func optionalTime(raw string, fallback time.Time) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	return time.Parse(time.RFC3339, raw)
}

func strconvQuote(value string) []byte {
	b, _ := json.Marshal(value)
	return b
}

func findRecorder(store storage.Storage, provider, serverID string) (domain.Entity, error) {
	entries, err := store.Search(provider + "." + serverID + ".*")
	if err != nil {
		return domain.Entity{}, fmt.Errorf("find recorder: %w", err)
	}
	for _, entry := range entries {
		var entity domain.Entity
		if json.Unmarshal(entry.Data, &entity) != nil || entity.Type != "recorder" {
			continue
		}
		for _, action := range entity.Commands {
			if action == "record_start" {
				return entity, nil
			}
		}
	}
	return domain.Entity{}, fmt.Errorf("no recorder advertising record_start for %s.%s", provider, serverID)
}

func waitForAiringSchedule(ctx context.Context, store storage.Storage, provider, serverID, airingID string, limit time.Duration) string {
	deadline := time.NewTimer(limit)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	needle := `"airingID":"` + airingID + `"`
	for {
		entries, _ := store.Search(provider + "." + serverID + ".*")
		for _, entry := range entries {
			var entity struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(entry.Data, &entity) == nil && entity.Type == "schedule" && strings.Contains(string(entry.Data), needle) {
				return entry.Key
			}
		}
		select {
		case <-ctx.Done():
			return ""
		case <-deadline.C:
			return ""
		case <-tick.C:
		}
	}
}
