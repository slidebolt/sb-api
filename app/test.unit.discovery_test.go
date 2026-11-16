package app

import "testing"

func TestCoreDiscoveryUsesDedicatedAPIService(t *testing.T) {
	if discoveryService != "_slidebolt-api._tcp" {
		t.Fatalf("core discovery service = %q, want %q", discoveryService, "_slidebolt-api._tcp")
	}
}
