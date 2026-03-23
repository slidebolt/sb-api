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
