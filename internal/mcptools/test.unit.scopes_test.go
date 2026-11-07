package mcptools

import (
	"testing"

	messenger "github.com/slidebolt/sb-messenger-sdk"
)

// Every tool must have a deliberate scope; a new tool left out of toolScopes
// would silently need write.
func TestEveryToolHasAnExplicitScope(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()
	s := New(nil, msg, nil)
	tools := s.ListTools()
	if len(tools) == 0 {
		t.Fatal("no tools registered")
	}
	for name := range tools {
		if _, ok := toolScopes[name]; !ok {
			t.Errorf("tool %q has no entry in toolScopes", name)
		}
	}
	for name := range toolScopes {
		if _, ok := tools[name]; !ok && name != "list_logs" && name != "get_log" {
			t.Errorf("toolScopes lists %q, which is not a registered tool", name)
		}
	}
}
