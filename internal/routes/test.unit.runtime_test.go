package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
)

func TestRuntimeComponentsPreserveExternalResources(t *testing.T) {
	manager := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status/components" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"plugin-example","kind":"plugin","state":"degraded","contractVersion":1,"restartCount":0,"resources":[{"id":"example-api","kind":"service","name":"Example API","description":"Supplies example data.","state":"degraded","lastCheckedAt":"2026-11-14T12:00:00Z","lastError":{"code":"unavailable","message":"connection refused","retryable":true}}]}]`))
	}))
	defer manager.Close()

	router := chi.NewMux()
	api := humachi.New(router, huma.DefaultConfig("test", "1"))
	RegisterRuntime(api, manager.URL)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/runtime/components", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var statuses []ComponentStatus
	if err := json.Unmarshal(response.Body.Bytes(), &statuses); err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || len(statuses[0].Resources) != 1 {
		t.Fatalf("statuses = %+v", statuses)
	}
	resource := statuses[0].Resources[0]
	if resource.Name != "Example API" || resource.Description != "Supplies example data." || resource.LastError == nil || resource.LastError.Code != "unavailable" {
		t.Fatalf("resource = %+v", resource)
	}
}
