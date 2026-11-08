package routes

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/slidebolt/sb-api/internal/inventory"
	storage "github.com/slidebolt/sb-storage-sdk"
)

type InventoryInput struct {
	Scope   string `query:"scope" enum:"controllable,all" default:"controllable" doc:"controllable lists entities that accept commands; all lists every entity"`
	State   bool   `query:"state" doc:"Add a short state column (on/off, brightness)"`
	Section string `query:"section" enum:"full,summary" default:"full" doc:"summary leaves out the entity rows"`
}

type InventoryOutput struct {
	ContentType string `header:"Content-Type"`
	Body        []byte
}

// RegisterInventory serves the whole system as one compact text document.
func RegisterInventory(api huma.API, store storage.Storage) {
	huma.Register(api, huma.Operation{
		Method:      "GET",
		Path:        "/inventory",
		Summary:     "Everything in one text document",
		Description: "Places, groups, saved queries and one tab-separated line per entity, built from the full entity list.",
		Tags:        []string{"entities"},
	}, func(ctx context.Context, in *InventoryInput) (*InventoryOutput, error) {
		entries, err := store.Search("*.*.*")
		if err != nil {
			return nil, huma.Error500InternalServerError("storage search failed", err)
		}
		text := inventory.Render(entries, inventory.Options{Scope: in.Scope, State: in.State, Section: in.Section})
		return &InventoryOutput{ContentType: "text/plain; charset=utf-8", Body: []byte(text)}, nil
	})
}
