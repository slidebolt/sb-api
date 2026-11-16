package server

import (
	"context"
	"net/http"
	"os"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/mark3labs/mcp-go/server"
	logging "github.com/slidebolt/sb-logging-sdk"

	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"

	"github.com/slidebolt/sb-api/internal/auth"
	"github.com/slidebolt/sb-api/internal/commands"
	"github.com/slidebolt/sb-api/internal/mcptools"
	"github.com/slidebolt/sb-api/internal/routes"
	apitrc "github.com/slidebolt/sb-api/internal/trace"
)

func New(msg messenger.Messenger, store storage.Storage) http.Handler {
	return NewWithLogger(msg, store, logging.ClientFrom(msg))
}

func NewWithLogger(msg messenger.Messenger, store storage.Storage, logger logging.Store) http.Handler {
	router := chi.NewMux()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r, traceID := apitrc.WithRequestTrace(r)
			if traceID != "" {
				w.Header().Set(apitrc.HeaderTraceID, traceID)
			}
			next.ServeHTTP(w, r)
		})
	})
	router.Use(auth.Middleware(store))

	config := huma.DefaultConfig("SlideBolt API", "1.0.33")
	config.Info.Description = "HTTP and MCP gateway to the SlideBolt runtime. Tokens combine cumulative read, control, or admin access with resource security groups."
	config.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"bearerAuth": {Type: "http", Scheme: "bearer", BearerFormat: "opaque", Description: "SlideBolt access token."},
	}
	config.Security = []map[string][]string{{"bearerAuth": {}}}

	api := humachi.New(router, config)

	routes.RegisterTokens(api, store)
	routes.RegisterAuth(api)
	routes.RegisterDevices(api, store)
	routes.RegisterEntities(api, store, msg, logger)
	routes.RegisterQuery(api, store)
	routes.RegisterInventory(api, store)
	routes.RegisterScripts(api, store, msg, logger)
	routes.RegisterSchedules(api, store, msg)
	routes.RegisterMedia(api, msg, store)
	routes.RegisterLogs(api, logger)
	managerURL := os.Getenv("SB_MANAGER_STATUS_URL")
	if managerURL == "" {
		managerURL = "http://127.0.0.1:29010"
	}
	routes.RegisterRuntime(api, managerURL)
	routes.RegisterDefinitions(api, store, managerURL)

	// One phrasebook and sender for the whole API, so repeat detection sees
	// every attempt.
	book := &commands.Book{Store: store}
	sender := &commands.Sender{Store: store, Msg: msg, Logs: logger, Book: book}
	routes.RegisterPhrasebook(api, book)

	mcpServer := mcptools.NewWithSender(store, msg, logger, sender)
	router.Get("/ws/events", webSocketEvents(store, msg))
	router.Mount("/mcp", server.NewStreamableHTTPServer(mcpServer, server.WithHTTPContextFunc(func(ctx context.Context, r *http.Request) context.Context {
		traceID := apitrc.FromContext(r.Context())
		if traceID == "" {
			_, traceID = apitrc.WithRequestTrace(r)
		}
		if principal, ok := auth.PrincipalFromContext(r.Context()); ok {
			ctx = auth.WithPrincipal(ctx, principal)
		}
		return apitrc.WithContext(ctx, traceID)
	})))

	return router
}
