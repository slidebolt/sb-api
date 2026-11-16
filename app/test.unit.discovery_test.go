package app

import "testing"

func TestCoreDiscoveryUsesDedicatedAPIService(t *testing.T) {
	if discoveryService != "_slidebolt-api._tcp" {
		t.Fatalf("core discovery service = %q, want %q", discoveryService, "_slidebolt-api._tcp")
	}
}

func TestDiscoverySelectsAtMostOneOutboundInterface(t *testing.T) {
	if got := outboundInterfaces(); len(got) > 1 {
		t.Fatalf("outboundInterfaces returned %d interfaces, want at most one", len(got))
	}
}
