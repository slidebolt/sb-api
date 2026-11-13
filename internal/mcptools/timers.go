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

func registerTimerTools(s *server.MCPServer, msg messenger.Messenger) {
	client := scheduler.NewClient(msg)
	s.AddTool(mcp.NewTool("create_timer",
		mcp.WithDescription("Create or replace a durable timer that turns a switch entity on and off"),
		mcp.WithReadOnlyHintAnnotation(false), mcp.WithDestructiveHintAnnotation(false), mcp.WithIdempotentHintAnnotation(true), mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("id", mcp.Required(), mcp.Description("Stable lowercase timer id")),
		mcp.WithString("name", mcp.Required(), mcp.Description("Display name")),
		mcp.WithString("timer", mcp.Required(), mcp.Description("Timer JSON containing target, input, on and off triggers")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var timer domain.Timer
		if err := json.Unmarshal([]byte(req.GetString("timer", "")), &timer); err != nil {
			return mcp.NewToolResultError("invalid timer JSON: " + err.Error()), nil
		}
		key, err := client.CreateTimer(scheduler.CreateTimerRequest{ID: strings.TrimSpace(req.GetString("id", "")), Name: req.GetString("name", ""), Timer: timer})
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(key.Key()), nil
	})
	s.AddTool(mcp.NewTool("delete_timer",
		mcp.WithDescription("Delete a timer by id"), mcp.WithReadOnlyHintAnnotation(false), mcp.WithDestructiveHintAnnotation(true), mcp.WithIdempotentHintAnnotation(true), mcp.WithOpenWorldHintAnnotation(false), mcp.WithString("id", mcp.Required(), mcp.Description("Timer id")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := strings.TrimSpace(req.GetString("id", ""))
		key := domain.EntityKey{Plugin: "sb-scheduler", DeviceID: "timer-" + id, ID: "timer"}
		if err := client.DeleteTimer(key); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("deleted %s", key.Key())), nil
	})
}
