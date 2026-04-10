package mcptools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	logcfg "github.com/slidebolt/sb-logging"
	logging "github.com/slidebolt/sb-logging-sdk"
	logserver "github.com/slidebolt/sb-logging/server"
)

func newLogger(t *testing.T) logging.Store {
	t.Helper()
	svc, err := logserver.New(logcfg.Config{Target: "memory"})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	return svc.Store()
}

func textResult(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if result == nil || len(result.Content) != 1 {
		t.Fatalf("unexpected content: %+v", result)
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("unexpected content type: %T", result.Content[0])
	}
	return text.Text
}

func TestListLogsHandlerFiltersByTraceID(t *testing.T) {
	logs := newLogger(t)
	ctx := context.Background()
	if err := logs.Append(ctx, logging.Event{
		ID:      "evt-1",
		TS:      time.Date(2026, 4, 9, 14, 0, 0, 0, time.UTC),
		Source:  "sb-virtual",
		Kind:    "fanout.published",
		Level:   "info",
		Message: "virtual fanout",
		Entity:  "plugin-automation.group.basement",
		TraceID: "trace-basement-1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := logs.Append(ctx, logging.Event{
		ID:      "evt-2",
		TS:      time.Date(2026, 4, 9, 14, 0, 1, 0, time.UTC),
		Source:  "plugin-esphome",
		Kind:    "command.received",
		Level:   "info",
		Message: "received command",
		Entity:  "plugin-esphome.basement-light-1.basement-light-1",
		TraceID: "trace-basement-1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := logs.Append(ctx, logging.Event{
		ID:      "evt-3",
		TS:      time.Date(2026, 4, 9, 14, 0, 2, 0, time.UTC),
		Source:  "plugin-esphome",
		Kind:    "command.received",
		Level:   "info",
		Message: "received command",
		Entity:  "plugin-esphome.kitchen-light.kitchen-light_1",
		TraceID: "trace-kitchen-1",
	}); err != nil {
		t.Fatal(err)
	}

	result, err := listLogsHandler(logs)(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "list_logs",
			Arguments: map[string]any{
				"trace_id": "trace-basement-1",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := textResult(t, result)
	var events []logging.Event
	if err := json.Unmarshal([]byte(body), &events); err != nil {
		t.Fatalf("unmarshal result: %v body=%s", err, body)
	}
	if len(events) != 2 {
		t.Fatalf("events len: got %d want 2", len(events))
	}
	if events[0].ID != "evt-1" || events[1].ID != "evt-2" {
		t.Fatalf("event order: got %q %q", events[0].ID, events[1].ID)
	}
}

func TestGetLogHandlerReturnsSingleEvent(t *testing.T) {
	logs := newLogger(t)
	ctx := context.Background()
	if err := logs.Append(ctx, logging.Event{
		ID:      "evt-1",
		TS:      time.Date(2026, 4, 9, 14, 0, 0, 0, time.UTC),
		Source:  "sb-virtual",
		Kind:    "command.received",
		Level:   "info",
		Message: "virtual command received",
		Entity:  "plugin-automation.group.basement",
		TraceID: "trace-basement-1",
	}); err != nil {
		t.Fatal(err)
	}

	result, err := getLogHandler(logs)(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "get_log",
			Arguments: map[string]any{
				"id": "evt-1",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := textResult(t, result)
	var event logging.Event
	if err := json.Unmarshal([]byte(body), &event); err != nil {
		t.Fatalf("unmarshal result: %v body=%s", err, body)
	}
	if event.ID != "evt-1" || event.TraceID != "trace-basement-1" {
		t.Fatalf("event: %+v", event)
	}
}
