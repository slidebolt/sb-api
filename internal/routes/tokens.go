package routes

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"
	storage "github.com/slidebolt/sb-storage-sdk"

	"github.com/slidebolt/sb-api/internal/auth"
)

type TokenCreateInput struct {
	Body struct {
		Name   string   `json:"name" doc:"Human-readable label for this token."`
		Scopes []string `json:"scopes" doc:"Access scopes: read, control, write, admin."`
	}
}

type TokenCreateOutput struct {
	Body struct {
		ID        string   `json:"id"`
		Name      string   `json:"name"`
		Token     string   `json:"token" doc:"Bearer secret — shown only once."`
		Scopes    []string `json:"scopes"`
		CreatedAt time.Time `json:"createdAt"`
	}
}

type TokenListOutput struct {
	Body []tokenSummary
}

type tokenSummary struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Scopes    []string  `json:"scopes"`
	CreatedAt time.Time `json:"createdAt"`
}

func RegisterTokens(api huma.API, store storage.Storage) {
	huma.Register(api, huma.Operation{
		Method:      "POST",
		Path:        "/tokens",
		Summary:     "Create an access token",
		Description: "Generates a new bearer token. The secret is returned only once.",
		Tags:        []string{"tokens"},
	}, func(ctx context.Context, input *TokenCreateInput) (*TokenCreateOutput, error) {
		if input.Body.Name == "" {
			return nil, huma.Error422UnprocessableEntity("name is required")
		}
		if len(input.Body.Scopes) == 0 {
			return nil, huma.Error422UnprocessableEntity("at least one scope is required")
		}
		for _, s := range input.Body.Scopes {
			if !auth.ValidScope(s) {
				return nil, huma.Error422UnprocessableEntity("invalid scope: " + s)
			}
		}

		id, err := auth.GenerateID()
		if err != nil {
			return nil, huma.Error500InternalServerError("generate id failed", err)
		}
		secret, err := auth.GenerateSecret()
		if err != nil {
			return nil, huma.Error500InternalServerError("generate secret failed", err)
		}

		tok := auth.Token{
			ID:        id,
			Name:      input.Body.Name,
			Hash:      auth.HashSecret(secret),
			Scopes:    input.Body.Scopes,
			CreatedAt: time.Now().UTC(),
		}
		if err := store.Save(tok); err != nil {
			return nil, huma.Error500InternalServerError("save token failed", err)
		}

		out := &TokenCreateOutput{}
		out.Body.ID = tok.ID
		out.Body.Name = tok.Name
		out.Body.Token = secret
		out.Body.Scopes = tok.Scopes
		out.Body.CreatedAt = tok.CreatedAt
		return out, nil
	})

	huma.Register(api, huma.Operation{
		Method:      "GET",
		Path:        "/tokens",
		Summary:     "List access tokens",
		Description: "Returns all tokens without secrets.",
		Tags:        []string{"tokens"},
	}, func(ctx context.Context, _ *struct{}) (*TokenListOutput, error) {
		tokens, err := auth.LoadTokens(store)
		if err != nil {
			return nil, huma.Error500InternalServerError("load tokens failed", err)
		}
		summaries := make([]tokenSummary, len(tokens))
		for i, t := range tokens {
			summaries[i] = tokenSummary{
				ID:        t.ID,
				Name:      t.Name,
				Scopes:    t.Scopes,
				CreatedAt: t.CreatedAt,
			}
		}
		return &TokenListOutput{Body: summaries}, nil
	})

	huma.Register(api, huma.Operation{
		Method:      "DELETE",
		Path:        "/tokens/{id}",
		Summary:     "Revoke an access token",
		Description: "Permanently deletes a token.",
		Tags:        []string{"tokens"},
	}, func(ctx context.Context, input *struct {
		ID string `path:"id"`
	}) (*struct{}, error) {
		tok := auth.Token{ID: input.ID}
		if err := store.Delete(tok); err != nil {
			return nil, huma.Error500InternalServerError("delete token failed", err)
		}
		return nil, nil
	})
}
