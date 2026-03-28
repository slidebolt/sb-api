package app

import "testing"

func TestHelloDeclaresRuntimeDependencies(t *testing.T) {
	h := New(DefaultConfig()).Hello()

	if len(h.DependsOn) != 2 {
		t.Fatalf("dependsOn: got %v want [messenger storage]", h.DependsOn)
	}
	if h.DependsOn[0] != "messenger" || h.DependsOn[1] != "storage" {
		t.Fatalf("dependsOn: got %v want [messenger storage]", h.DependsOn)
	}
}

func TestDefaultConfigUsesDefaultPort(t *testing.T) {
	t.Setenv("SB_API_PORT", "")
	t.Setenv("SB_API_LISTEN_ADDR", "")
	t.Setenv("SB_API_HTTP_URL", "")

	cfg := DefaultConfig()

	if cfg.ListenAddr != "127.0.0.1:29011" {
		t.Fatalf("ListenAddr: got %q want %q", cfg.ListenAddr, "127.0.0.1:29011")
	}
	if cfg.HTTPURL != "127.0.0.1" {
		t.Fatalf("HTTPURL: got %q want %q", cfg.HTTPURL, "127.0.0.1")
	}
}

func TestDefaultConfigUsesEnvOverrides(t *testing.T) {
	t.Setenv("SB_API_PORT", "30123")
	t.Setenv("SB_API_HTTP_URL", "api.example.internal")
	t.Setenv("SB_API_LISTEN_ADDR", "")

	cfg := DefaultConfig()
	if cfg.ListenAddr != "127.0.0.1:30123" {
		t.Fatalf("ListenAddr: got %q want %q", cfg.ListenAddr, "127.0.0.1:30123")
	}
	if cfg.HTTPURL != "api.example.internal" {
		t.Fatalf("HTTPURL: got %q want %q", cfg.HTTPURL, "api.example.internal")
	}
}

func TestDefaultConfigPrefersExplicitListenAddr(t *testing.T) {
	t.Setenv("SB_API_PORT", "30123")
	t.Setenv("SB_API_LISTEN_ADDR", "0.0.0.0:39011")

	cfg := DefaultConfig()
	if cfg.ListenAddr != "0.0.0.0:39011" {
		t.Fatalf("ListenAddr: got %q want %q", cfg.ListenAddr, "0.0.0.0:39011")
	}
}
