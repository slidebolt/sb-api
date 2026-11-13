package routes

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"
	logging "github.com/slidebolt/sb-logging-sdk"
)

type LogsInput struct {
	Since   time.Time `query:"since,omitempty"`
	Until   time.Time `query:"until,omitempty"`
	Source  string    `query:"source,omitempty"`
	Kind    string    `query:"kind,omitempty"`
	Level   string    `query:"level,omitempty"`
	Plugin  string    `query:"plugin,omitempty"`
	Device  string    `query:"device,omitempty"`
	Entity  string    `query:"entity,omitempty"`
	TraceID string    `query:"trace_id,omitempty"`
	Limit   int       `query:"limit,omitempty" minimum:"1" maximum:"1000" default:"200"`
}

type LogsOutput struct {
	Body []logging.Event
}

type LogInput struct {
	ID string `path:"id"`
}

type LogOutput struct {
	Body logging.Event
}

// RegisterLogs exposes the existing structured logging store to read-only API
// clients. Provider-specific details remain in Event.Data; the common filter
// fields are deliberately provider-neutral.
func RegisterLogs(api huma.API, store logging.Store) {
	huma.Register(api, huma.Operation{
		Method: "GET", Path: "/logs", Summary: "List structured logs", Tags: []string{"logs"},
	}, func(ctx context.Context, in *LogsInput) (*LogsOutput, error) {
		events, err := store.List(ctx, logging.ListRequest{
			Since: in.Since, Until: in.Until, Source: in.Source, Kind: in.Kind,
			Level: in.Level, Plugin: in.Plugin, Device: in.Device, Entity: in.Entity,
			TraceID: in.TraceID, Limit: in.Limit,
		})
		if err != nil {
			return nil, huma.Error500InternalServerError("list logs failed", err)
		}
		if events == nil {
			events = []logging.Event{}
		}
		return &LogsOutput{Body: events}, nil
	})

	huma.Register(api, huma.Operation{
		Method: "GET", Path: "/logs/{id}", Summary: "Get a structured log", Tags: []string{"logs"},
	}, func(ctx context.Context, in *LogInput) (*LogOutput, error) {
		event, err := store.Get(ctx, in.ID)
		if err != nil {
			return nil, huma.Error404NotFound("log not found")
		}
		return &LogOutput{Body: event}, nil
	})
}
