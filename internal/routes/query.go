package routes

import (
	"context"
	"encoding/json"

	"github.com/danielgtaylor/huma/v2"
	storage "github.com/slidebolt/sb-storage-sdk"
)

type QueryInput struct {
	Body storage.Query
}

type QueryOutput struct {
	Body []EntityEntry
}

func RegisterQuery(api huma.API, store storage.Storage) {
	huma.Register(api, huma.Operation{
		Method:      "POST",
		Path:        "/query",
		Summary:     "Query storage",
		Description: "Run a structured query against storage with optional field filters.",
		Tags:        []string{"query"},
	}, func(ctx context.Context, input *QueryInput) (*QueryOutput, error) {
		entries, err := store.Query(input.Body)
		if err != nil {
			return nil, huma.Error500InternalServerError("query failed", err)
		}
		out := make([]EntityEntry, len(entries))
		for i, e := range entries {
			out[i] = EntityEntry{Key: e.Key, Data: json.RawMessage(e.Data)}
		}
		return &QueryOutput{Body: out}, nil
	})
}
