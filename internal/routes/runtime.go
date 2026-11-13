package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

type ComponentStatus struct {
	ID              string    `json:"id"`
	Kind            string    `json:"kind"`
	State           string    `json:"state"`
	ContractVersion int       `json:"contractVersion"`
	Dependencies    []string  `json:"dependencies,omitempty"`
	StartedAt       time.Time `json:"startedAt,omitempty"`
	ReadyAt         time.Time `json:"readyAt,omitempty"`
	RestartCount    int       `json:"restartCount"`
	LastError       string    `json:"lastError,omitempty"`
}

type RuntimeOutput struct{ Body []ComponentStatus }

func RegisterRuntime(api huma.API, managerURL string) {
	client := &http.Client{Timeout: 2 * time.Second}
	huma.Register(api, huma.Operation{Method: "GET", Path: "/runtime/components", Summary: "List managed component runtime status", Tags: []string{"runtime"}}, func(ctx context.Context, _ *struct{}) (*RuntimeOutput, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(managerURL, "/")+"/status/components", nil)
		if err != nil {
			return nil, huma.Error500InternalServerError("manager request failed", err)
		}
		res, err := client.Do(req)
		if err != nil {
			return nil, huma.Error503ServiceUnavailable("manager status unavailable", err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return nil, huma.Error503ServiceUnavailable("manager status unavailable")
		}
		var statuses []ComponentStatus
		if err := json.NewDecoder(res.Body).Decode(&statuses); err != nil {
			return nil, huma.Error502BadGateway("invalid manager status", err)
		}
		return &RuntimeOutput{Body: statuses}, nil
	})
}
