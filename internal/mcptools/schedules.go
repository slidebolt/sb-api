package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	domain "github.com/slidebolt/sb-domain"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	scheduler "github.com/slidebolt/sb-scheduler-sdk"
)

func registerScheduleTools(s *server.MCPServer, msg messenger.Messenger) {
	client := scheduler.NewClient(msg)
	s.AddTool(mcp.NewTool("create_schedule", mcp.WithDescription("Create or replace a durable schedule of generic entity actions"), mcp.WithReadOnlyHintAnnotation(false), mcp.WithDestructiveHintAnnotation(false), mcp.WithIdempotentHintAnnotation(true), mcp.WithOpenWorldHintAnnotation(false), mcp.WithString("id", mcp.Required(), mcp.Description("Stable lowercase schedule id")), mcp.WithString("name", mcp.Required(), mcp.Description("Display name")), mcp.WithString("schedule", mcp.Required(), mcp.Description("Schedule JSON containing trigger steps and actions"))), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var schedule domain.Schedule
		if err := json.Unmarshal([]byte(req.GetString("schedule", "")), &schedule); err != nil {
			return mcp.NewToolResultError("invalid schedule JSON: " + err.Error()), nil
		}
		key, err := client.CreateSchedule(scheduler.CreateScheduleRequest{ID: strings.TrimSpace(req.GetString("id", "")), Name: req.GetString("name", ""), Schedule: schedule})
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(key.Key()), nil
	})
	s.AddTool(mcp.NewTool("delete_schedule", mcp.WithDescription("Delete a schedule by id"), mcp.WithReadOnlyHintAnnotation(false), mcp.WithDestructiveHintAnnotation(true), mcp.WithIdempotentHintAnnotation(true), mcp.WithOpenWorldHintAnnotation(false), mcp.WithString("id", mcp.Required(), mcp.Description("Schedule id"))), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := strings.TrimSpace(req.GetString("id", ""))
		key := domain.EntityKey{Plugin: "sb-scheduler", DeviceID: "schedule-" + id, ID: "schedule"}
		if err := client.DeleteSchedule(key); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("deleted %s", key.Key())), nil
	})
}
