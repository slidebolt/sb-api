package routes

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/slidebolt/sb-api/internal/commands"
)

type phrasebookListInput struct {
	Search string `query:"search" doc:"Optional words to rank entries by, as phrase_lookup does"`
}

type phrasebookListOutput struct {
	Body []commands.Entry
}

type phrasebookIDInput struct {
	ID string `path:"id"`
}

type phrasebookEntryOutput struct {
	Body commands.Entry
}

type phrasebookPatchInput struct {
	ID   string `path:"id"`
	Body struct {
		Status string `json:"status" enum:"verified,unverified,suspect,wrong"`
		Reason string `json:"reason,omitempty"`
	}
}

// RegisterPhrasebook serves the learned words-to-command table for review:
// list or search it, correct an entry's status, or delete an entry.
func RegisterPhrasebook(api huma.API, book *commands.Book) {
	huma.Register(api, huma.Operation{
		Method:  http.MethodGet,
		Path:    "/phrasebook",
		Summary: "List phrasebook entries",
		Description: "The learned mapping from what people said to the command that was sent, with whether it " +
			"was verified, suspect, unverified or wrong. With search, entries are ranked by similarity.",
		Tags: []string{"phrasebook"},
	}, func(ctx context.Context, input *phrasebookListInput) (*phrasebookListOutput, error) {
		if input.Search != "" {
			matches, err := book.Lookup(input.Search, 50)
			if err != nil {
				return nil, huma.Error500InternalServerError("lookup failed", err)
			}
			out := make([]commands.Entry, len(matches))
			for i, m := range matches {
				out[i] = m.Entry
			}
			return &phrasebookListOutput{Body: out}, nil
		}
		entries, err := book.List()
		if err != nil {
			return nil, huma.Error500InternalServerError("list failed", err)
		}
		return &phrasebookListOutput{Body: entries}, nil
	})

	huma.Register(api, huma.Operation{
		Method:  http.MethodGet,
		Path:    "/phrasebook/{id}",
		Summary: "Get a phrasebook entry",
		Tags:    []string{"phrasebook"},
	}, func(ctx context.Context, input *phrasebookIDInput) (*phrasebookEntryOutput, error) {
		e, err := book.Get(input.ID)
		if err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		return &phrasebookEntryOutput{Body: e}, nil
	})

	huma.Register(api, huma.Operation{
		Method:  http.MethodPatch,
		Path:    "/phrasebook/{id}",
		Summary: "Set a phrasebook entry's status",
		Tags:    []string{"phrasebook"},
	}, func(ctx context.Context, input *phrasebookPatchInput) (*phrasebookEntryOutput, error) {
		e, err := book.Update(input.ID, input.Body.Status, input.Body.Reason)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return &phrasebookEntryOutput{Body: e}, nil
	})

	huma.Register(api, huma.Operation{
		Method:        http.MethodDelete,
		Path:          "/phrasebook/{id}",
		Summary:       "Delete a phrasebook entry",
		Tags:          []string{"phrasebook"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, input *phrasebookIDInput) (*struct{}, error) {
		if err := book.Delete(input.ID); err != nil {
			return nil, huma.Error500InternalServerError("delete failed", err)
		}
		return nil, nil
	})
}
