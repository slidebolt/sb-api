package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"

	contract "github.com/slidebolt/sb-contract"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"

	"github.com/slidebolt/sb-api/internal/server"
)

type App struct {
	cfg      Config
	msg      messenger.Messenger
	store    storage.Storage
	httpSv   *http.Server
	listener net.Listener
}

func New(cfg Config) *App {
	return &App{cfg: cfg}
}

func (a *App) Hello() contract.HelloResponse {
	return contract.HelloResponse{
		ID:              "api",
		Kind:            contract.KindService,
		ContractVersion: contract.ContractVersion,
		DependsOn:       []string{"messenger", "storage"},
	}
}

func (a *App) OnStart(deps map[string]json.RawMessage) (json.RawMessage, error) {
	msg, err := messenger.Connect(deps)
	if err != nil {
		return nil, fmt.Errorf("connect messenger: %w", err)
	}
	a.msg = msg

	store, err := storage.Connect(deps)
	if err != nil {
		a.msg.Close()
		a.msg = nil
		return nil, fmt.Errorf("connect storage: %w", err)
	}
	a.store = store

	handler := server.New(msg, store)
	a.httpSv = &http.Server{
		Addr:    a.cfg.ListenAddr,
		Handler: handler,
	}

	ln, err := net.Listen("tcp", a.cfg.ListenAddr)
	if err != nil {
		a.store.Close()
		a.msg.Close()
		a.store = nil
		a.msg = nil
		return nil, fmt.Errorf("listen %s: %w", a.cfg.ListenAddr, err)
	}
	a.listener = ln

	go func() {
		if err := a.httpSv.Serve(ln); err != nil && err != http.ErrServerClosed {
			panic(fmt.Sprintf("api: http server error: %v", err))
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	payload, _ := json.Marshal(map[string]any{
		"http_url":  a.cfg.HTTPURL,
		"http_port": addr.Port,
	})
	return payload, nil
}

func (a *App) OnShutdown() error {
	if a.httpSv != nil {
		a.httpSv.Shutdown(context.Background())
	}
	if a.listener != nil {
		a.listener.Close()
	}
	if a.store != nil {
		a.store.Close()
	}
	if a.msg != nil {
		a.msg.Close()
	}
	return nil
}
