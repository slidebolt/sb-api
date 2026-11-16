package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/grandcat/zeroconf"
	contract "github.com/slidebolt/sb-contract"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"

	"github.com/slidebolt/sb-api/internal/server"
)

type App struct {
	cfg       Config
	msg       messenger.Messenger
	store     storage.Storage
	httpSv    *http.Server
	listener  net.Listener
	discovery *zeroconf.Server
}

const discoveryService = "_slidebolt-api._tcp"

func New(cfg Config) *App {
	return &App{cfg: cfg}
}

func (a *App) Hello() contract.HelloResponse {
	return contract.HelloResponse{
		ID:              "api",
		Kind:            contract.KindService,
		ContractVersion: contract.ContractVersion,
		Setup: contract.RequiredSetup("Core",
			contract.SetupField{Key: "port", Environment: "SB_API_PORT", Name: "API port", Description: "Local HTTP port used by the SlideBolt API.", Control: contract.SetupText, Generate: "free_port"},
			contract.SetupField{Key: "manager_status_url", Environment: "SB_MANAGER_STATUS_URL", Name: "Manager status URL", Description: "Internal manager health endpoint used to report component state.", Control: contract.SetupText, Advanced: true},
		),
		DependsOn: []string{"messenger", "storage", "scheduler"},
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
	a.startDiscovery()

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

func (a *App) startDiscovery() {
	if strings.TrimSpace(a.cfg.PublicURL) == "" {
		return
	}
	u, err := url.Parse(a.cfg.PublicURL)
	if err != nil || u.Hostname() == "" {
		return
	}
	port := 443
	if u.Port() != "" {
		if _, err := fmt.Sscanf(u.Port(), "%d", &port); err != nil {
			return
		}
	}
	name := strings.TrimSpace(a.cfg.SystemName)
	if name == "" {
		name = "SlideBolt"
	}
	txt := []string{"id=" + a.cfg.SystemID, "api=" + a.cfg.PublicURL, "version=1.0.33", "path=" + u.Path}
	server, err := zeroconf.Register(name, discoveryService, "local.", port, txt, outboundInterfaces())
	if err == nil {
		a.discovery = server
	}
}

// outboundInterfaces limits DNS-SD to the interface carrying the default
// route. Without this, zeroconf also publishes Docker bridge addresses that
// are unreachable from LAN clients.
func outboundInterfaces() []net.Interface {
	conn, err := net.Dial("udp", "1.1.1.1:53")
	if err != nil {
		return nil
	}
	defer conn.Close()
	localIP := conn.LocalAddr().(*net.UDPAddr).IP

	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch value := addr.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			if ip != nil && ip.Equal(localIP) {
				return []net.Interface{iface}
			}
		}
	}
	return nil
}

func (a *App) OnShutdown() error {
	if a.discovery != nil {
		a.discovery.Shutdown()
	}
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
