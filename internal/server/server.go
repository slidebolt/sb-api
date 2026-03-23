package server

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/mark3labs/mcp-go/server"

	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"

	"github.com/slidebolt/sb-api/internal/mcptools"
	"github.com/slidebolt/sb-api/internal/routes"
)

func New(msg messenger.Messenger, store storage.Storage) http.Handler {
	router := chi.NewMux()

	config := huma.DefaultConfig("SlideBolt API", "0.1.0")
	config.Info.Description = "HTTP and MCP gateway to the SlideBolt runtime."

	api := humachi.New(router, config)

	routes.RegisterDevices(api, store)
	routes.RegisterEntities(api, store, msg)
	routes.RegisterQuery(api, store)
	routes.RegisterAutomations(api, store)
	routes.RegisterScripts(api, store, msg)

	mcpServer := mcptools.New(store, msg)
	router.Mount("/mcp", server.NewStreamableHTTPServer(mcpServer))

	return router
}
