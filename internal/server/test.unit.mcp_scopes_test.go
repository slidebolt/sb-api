package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mcpsrv "github.com/mark3labs/mcp-go/server"
	logcfg "github.com/slidebolt/sb-logging"
	logserver "github.com/slidebolt/sb-logging/server"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"

	"github.com/slidebolt/sb-api/internal/auth"
)

type mcpScopeFixture struct {
	t         *testing.T
	url       string
	store     *fakeStore
	session   string
	published chan *messenger.Message
}

// newMCPScopeFixture starts the API with one token holding the given scopes and
// opens an MCP session with it.
func newMCPScopeFixture(t *testing.T, scopes []string) *mcpScopeFixture {
	access := auth.AccessRead
	if auth.HasScope(scopes, auth.ScopeAdmin) {
		access = auth.AccessAdmin
	} else if auth.HasScope(scopes, auth.ScopeControl) {
		access = auth.AccessControl
	}
	return newMCPAccessFixture(t, access, []string{"*"})
}

func newMCPAccessFixture(t *testing.T, access auth.Access, groups []string) *mcpScopeFixture {
	t.Helper()
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{}
	seedAccessToken(store, access, groups)
	seedEntity(store, "plugin-virtual.dev1.lamp", "light_turn_on", "light_turn_off")
	logSvc, err := logserver.New(logcfg.Config{Target: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	published := make(chan *messenger.Message, 4)
	sub, err := msg.Subscribe("plugin-virtual.dev1.lamp.command.>", func(m *messenger.Message) { published <- m })
	if err != nil {
		t.Fatal(err)
	}
	if err := msg.Flush(); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewWithLogger(msg, store, logSvc.Store()))
	t.Cleanup(func() { sub.Unsubscribe(); srv.Close(); msg.Close() })

	f := &mcpScopeFixture{t: t, url: srv.URL + "/mcp", store: store, published: published}
	resp := mcpPost(t, f.url, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"clientInfo":      map[string]any{"name": "scope-test", "version": "1"},
		},
	}, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("initialize with access %s groups %v: status %d body=%s", access, groups, resp.StatusCode, body)
	}
	f.session = resp.Header.Get(mcpsrv.HeaderKeySessionID)
	return f
}

func TestMCPSecurityGroupsPreserveHTTPPrincipal(t *testing.T) {
	f := newMCPAccessFixture(t, auth.AccessRead, []string{"media"})
	media := securityEntity("plugin-test", "server", "player", "media_player", "media")
	lock := securityEntity("plugin-test", "door", "lock", "lock", "security")
	f.store.entities = map[string]json.RawMessage{
		"plugin-test.server.player": media,
		"plugin-test.door.lock":     lock,
	}
	f.store.searchResultsByPattern["*.*.*"] = []storage.Entry{
		{Key: "plugin-test.server.player", Data: media},
		{Key: "plugin-test.door.lock", Data: lock},
	}

	isErr, listed := f.call("list_entities", map[string]any{})
	if isErr || !strings.Contains(listed, "plugin-test.server.player") || strings.Contains(listed, "plugin-test.door.lock") {
		t.Fatalf("media-only list_entities: error=%v result=%s", isErr, listed)
	}
	isErr, text := f.call("get_entity", map[string]any{"plugin": "plugin-test", "device": "door", "entity": "lock"})
	if !isErr || !strings.Contains(text, "not found") {
		t.Fatalf("media-only get security entity: error=%v result=%s", isErr, text)
	}
}

func (f *mcpScopeFixture) toolNames() map[string]bool {
	f.t.Helper()
	resp := mcpPost(f.t, f.url, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}, f.session)
	var out struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	decodeBody(f.t, resp, &out)
	names := map[string]bool{}
	for _, tool := range out.Result.Tools {
		names[tool.Name] = true
	}
	return names
}

// call returns whether the tool reported an error, and its text.
func (f *mcpScopeFixture) call(name string, args map[string]any) (bool, string) {
	f.t.Helper()
	resp := mcpPost(f.t, f.url, map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	}, f.session)
	var out struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	decodeBody(f.t, resp, &out)
	text := ""
	for _, c := range out.Result.Content {
		text += c.Text
	}
	return out.Result.IsError, text
}

var lampOn = map[string]any{
	"plugin": "plugin-virtual", "device": "dev1", "entity": "lamp",
	"action": "light_turn_on", "payload": `{}`,
}

func TestMCPScopes_ConnectingNeedsOnlyRead(t *testing.T) {
	if got := auth.RequiredScope(http.MethodPost, "/mcp"); got != auth.ScopeRead {
		t.Fatalf("POST /mcp: got %q want read", got)
	}
	newMCPScopeFixture(t, []string{"read"})
}

func TestMCPScopes_ReadControlTokenCanActButNotChangeStoredData(t *testing.T) {
	f := newMCPScopeFixture(t, []string{"read", "control"})

	names := f.toolNames()
	for _, want := range []string{"list_entities", "get_entity", "send_command"} {
		if !names[want] {
			t.Errorf("tools/list: %s should be offered to a read+control token", want)
		}
	}
	for _, hidden := range []string{"start_script", "save_script", "create_entity", "delete_entity", "save_query"} {
		if names[hidden] {
			t.Errorf("tools/list: %s should not be offered to a read+control token", hidden)
		}
	}

	isErr, text := f.call("save_script", map[string]any{"name": "Evil", "source": "print(1)", "type": "script"})
	if !isErr || !strings.Contains(text, "scope: admin") {
		t.Fatalf("save_script with read+control: want scope error, got isError=%v text=%q", isErr, text)
	}
	if f.store.savedKey != "" {
		t.Fatalf("save_script was refused but still saved %q", f.store.savedKey)
	}

	if isErr, text := f.call("send_command", lampOn); isErr {
		t.Fatalf("send_command with read+control: %s", text)
	}
	select {
	case <-f.published:
	case <-time.After(5 * time.Second):
		t.Fatal("send_command with read+control: nothing was published")
	}
}

func TestMCPScopes_ReadTokenCannotSendCommands(t *testing.T) {
	f := newMCPScopeFixture(t, []string{"read"})
	if f.toolNames()["send_command"] {
		t.Error("tools/list: send_command should not be offered to a read token")
	}
	isErr, text := f.call("send_command", lampOn)
	if !isErr || !strings.Contains(text, "scope: control") {
		t.Fatalf("send_command with read: want scope error, got isError=%v text=%q", isErr, text)
	}
	select {
	case m := <-f.published:
		t.Fatalf("send_command was refused but published %s", m.Subject)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestMCPScopes_FullTokenSeesEveryTool(t *testing.T) {
	f := newMCPScopeFixture(t, []string{"read", "control", "write", "admin"})
	names := f.toolNames()
	for _, want := range []string{"save_script", "send_command", "list_entities", "delete_entity"} {
		if !names[want] {
			t.Errorf("tools/list: %s missing for a full token", want)
		}
	}
}
