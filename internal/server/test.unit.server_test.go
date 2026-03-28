package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	domain "github.com/slidebolt/sb-domain"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"

	"github.com/slidebolt/sb-api/internal/auth"
)

// ---------------------------------------------------------------------------
// fakeStore
// ---------------------------------------------------------------------------

type fakeStore struct {
	searchPattern  string
	searchPatterns []string
	queryArg       storage.Query
	savedKey       string
	savedBody      json.RawMessage
	profileKey     string
	profileBody    json.RawMessage
	deletedKey     string

	searchResult           []storage.Entry
	searchResultsByPattern map[string][]storage.Entry
	queryResult            []storage.Entry
	getResult              json.RawMessage
}

func (f *fakeStore) Save(v storage.Keyed) error {
	f.savedKey = v.Key()
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f.savedBody = data
	return nil
}

func (f *fakeStore) Get(key storage.Keyed) (json.RawMessage, error) {
	return f.getResult, nil
}

func (f *fakeStore) Delete(key storage.Keyed) error {
	f.deletedKey = key.Key()
	return nil
}

func (f *fakeStore) Search(pattern string) ([]storage.Entry, error) {
	f.searchPattern = pattern
	f.searchPatterns = append(f.searchPatterns, pattern)
	if f.searchResultsByPattern != nil {
		if results, ok := f.searchResultsByPattern[pattern]; ok {
			return results, nil
		}
	}
	return f.searchResult, nil
}

func (f *fakeStore) SearchFiles(target storage.StorageTarget, pattern string) ([]storage.Entry, error) {
	return f.Search(pattern)
}

func (f *fakeStore) Query(q storage.Query) ([]storage.Entry, error) {
	f.queryArg = q
	return f.queryResult, nil
}

func (f *fakeStore) WriteFile(target storage.StorageTarget, key storage.Keyed, data json.RawMessage) error {
	return nil
}

func (f *fakeStore) ReadFile(target storage.StorageTarget, key storage.Keyed) (json.RawMessage, error) {
	return nil, nil
}

func (f *fakeStore) DeleteFile(target storage.StorageTarget, key storage.Keyed) error {
	return nil
}

func (f *fakeStore) SetPrivate(key storage.Keyed, data json.RawMessage) error {
	return nil
}

func (f *fakeStore) GetPrivate(key storage.Keyed) (json.RawMessage, error) {
	return nil, nil
}

func (f *fakeStore) DeletePrivate(key storage.Keyed) error {
	return nil
}

func (f *fakeStore) SetInternal(key storage.Keyed, data json.RawMessage) error {
	return nil
}

func (f *fakeStore) GetInternal(key storage.Keyed) (json.RawMessage, error) {
	return nil, nil
}

func (f *fakeStore) DeleteInternal(key storage.Keyed) error {
	return nil
}

func (f *fakeStore) SetProfile(key storage.Keyed, data json.RawMessage) error {
	f.profileKey = key.Key()
	f.profileBody = data
	return nil
}

func (f *fakeStore) Close() {}

// ---------------------------------------------------------------------------
// test helpers
// ---------------------------------------------------------------------------

const testTokenSecret = "test-token-secret"

func seedToken(store *fakeStore, scopes []string) {
	hash := auth.HashSecret(testTokenSecret)
	data, _ := json.Marshal(auth.Token{
		ID:        "test-token",
		Name:      "Test",
		Hash:      hash,
		Scopes:    scopes,
		CreatedAt: time.Now().UTC(),
	})
	if store.searchResultsByPattern == nil {
		store.searchResultsByPattern = map[string][]storage.Entry{}
	}
	store.searchResultsByPattern["sb-api.tokens.>"] = []storage.Entry{
		{Key: "sb-api.tokens.test-token", Data: data},
	}
}

func authGet(url string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+testTokenSecret)
	return http.DefaultClient.Do(req)
}

func authPost(url, contentType string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+testTokenSecret)
	return http.DefaultClient.Do(req)
}

func authDo(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+testTokenSecret)
	return http.DefaultClient.Do(req)
}

func decodeBody(t *testing.T, resp *http.Response, dest any) {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, dest); err != nil {
		t.Fatalf("unmarshal %s: %v", string(body), err)
	}
}

func decodeWS(t *testing.T, conn *websocket.Conn, dest any) {
	t.Helper()
	if err := conn.ReadJSON(dest); err != nil {
		t.Fatalf("read websocket json: %v", err)
	}
}

func subscribeScriptAPI(t *testing.T, msg messenger.Messenger, subject string, handler func(*messenger.Message)) {
	t.Helper()
	_, err := msg.Subscribe(subject, handler)
	if err != nil {
		t.Fatalf("subscribe %s: %v", subject, err)
	}
	if err := msg.Flush(); err != nil {
		t.Fatalf("flush subscriptions: %v", err)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// ---------------------------------------------------------------------------
// Auth middleware tests
// ---------------------------------------------------------------------------

func TestAuth_BootstrapMode(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	store := &fakeStore{}
	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	// No tokens exist — all endpoints blocked except POST /tokens.
	resp, err := http.Get(srv.URL + "/devices")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("bootstrap GET /devices: got %d want %d", resp.StatusCode, http.StatusForbidden)
	}

	resp, err = http.Get(srv.URL + "/entities")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("bootstrap GET /entities: got %d want %d", resp.StatusCode, http.StatusForbidden)
	}

	// POST /tokens should be allowed (bootstrap).
	resp, err = http.Post(srv.URL+"/tokens", "application/json",
		bytes.NewReader([]byte(`{"name":"Admin","scopes":["read","control","write","admin"]}`)))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("bootstrap POST /tokens: got %d want %d body=%s", resp.StatusCode, http.StatusOK, body)
	}

	var created map[string]any
	decodeBody(t, resp, &created)
	if created["token"] == nil || created["token"] == "" {
		t.Fatalf("expected token secret in response: %+v", created)
	}
	if created["id"] == nil || created["id"] == "" {
		t.Fatalf("expected id in response: %+v", created)
	}
}

func TestAuth_MissingToken(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	store := &fakeStore{}
	seedToken(store, []string{"read", "control", "write", "admin"})
	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	// No Authorization header.
	resp, err := http.Get(srv.URL + "/devices")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing token: got %d want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestAuth_WebSocketBootstrapModeForbidden(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	store := &fakeStore{}
	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events"
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected websocket dial to fail without bootstrap token")
	}
	if resp == nil {
		t.Fatalf("expected handshake response, got nil: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("websocket bootstrap: got %d want %d", resp.StatusCode, http.StatusForbidden)
	}
}

func TestWebSocketEvents_StreamSnapshotAndUpdates(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	initial := mustJSON(t, map[string]any{
		"id":    "ent1",
		"state": map[string]any{"power": false},
	})
	updated := mustJSON(t, map[string]any{
		"id":    "ent1",
		"state": map[string]any{"power": true},
	})
	store := &fakeStore{
		searchResult: []storage.Entry{
			{Key: "plugin.dev1.ent1", Data: initial},
		},
	}
	seedToken(store, []string{"read"})

	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events?pattern=plugin.dev1.>"
	header := http.Header{}
	header.Set("Authorization", "Bearer "+testTokenSecret)
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		if resp != nil {
			t.Fatalf("dial websocket: %v (status %d)", err, resp.StatusCode)
		}
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	var snapshot struct {
		Type    string          `json:"type"`
		Pattern string          `json:"pattern"`
		Items   []storage.Entry `json:"items"`
	}
	decodeWS(t, conn, &snapshot)
	if snapshot.Type != "snapshot" {
		t.Fatalf("snapshot type: got %q want %q", snapshot.Type, "snapshot")
	}
	if snapshot.Pattern != "plugin.dev1.>" {
		t.Fatalf("snapshot pattern: got %q want %q", snapshot.Pattern, "plugin.dev1.>")
	}
	if len(snapshot.Items) != 1 || snapshot.Items[0].Key != "plugin.dev1.ent1" {
		t.Fatalf("snapshot items: got %+v", snapshot.Items)
	}
	if got := store.searchPattern; got != "plugin.dev1.>" {
		t.Fatalf("search pattern: got %q want %q", got, "plugin.dev1.>")
	}

	if err := msg.Publish("state.changed.plugin.dev1.ent1", updated); err != nil {
		t.Fatal(err)
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var event struct {
		Type string          `json:"type"`
		Key  string          `json:"key"`
		Data json.RawMessage `json:"data"`
	}
	decodeWS(t, conn, &event)
	if event.Type != "upsert" {
		t.Fatalf("event type: got %q want %q", event.Type, "upsert")
	}
	if event.Key != "plugin.dev1.ent1" {
		t.Fatalf("event key: got %q want %q", event.Key, "plugin.dev1.ent1")
	}
	if string(event.Data) != string(updated) {
		t.Fatalf("event data: got %s want %s", string(event.Data), string(updated))
	}
}

func TestWebSocketEvents_AcceptsAccessTokenQuery(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	store := &fakeStore{}
	seedToken(store, []string{"read"})
	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events?access_token=" + testTokenSecret
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		if resp != nil {
			t.Fatalf("dial websocket with query token: %v (status %d)", err, resp.StatusCode)
		}
		t.Fatalf("dial websocket with query token: %v", err)
	}
	defer conn.Close()

	var snapshot struct {
		Type string `json:"type"`
	}
	decodeWS(t, conn, &snapshot)
	if snapshot.Type != "snapshot" {
		t.Fatalf("snapshot type: got %q want %q", snapshot.Type, "snapshot")
	}
}

func TestAuth_InvalidToken(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	store := &fakeStore{}
	seedToken(store, []string{"read", "control", "write", "admin"})
	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/devices", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer wrong-secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("invalid token: got %d want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestAuth_InsufficientScope_ReadOnly(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	store := &fakeStore{
		searchResult: []storage.Entry{
			{Key: "plugin.dev1", Data: json.RawMessage(`{"name":"Device 1"}`)},
		},
	}
	seedToken(store, []string{"read"})
	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	// Read should work.
	resp, err := authGet(srv.URL + "/devices")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("read-scoped GET /devices: got %d want %d", resp.StatusCode, http.StatusOK)
	}

	// Command (control scope) should fail.
	resp, err = authPost(srv.URL+"/entities/plugin/dev1/ent1/command/turn_on", "application/json", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("read-scoped POST command: got %d want %d", resp.StatusCode, http.StatusForbidden)
	}

	// Write should fail.
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/devices/plugin/dev2", bytes.NewReader([]byte(`{
		"id":"dev2","plugin":"plugin","name":"Device 2","entities":[]
	}`)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err = authDo(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("read-scoped PUT device: got %d want %d", resp.StatusCode, http.StatusForbidden)
	}

	// Admin should fail.
	resp, err = authGet(srv.URL + "/tokens")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("read-scoped GET /tokens: got %d want %d", resp.StatusCode, http.StatusForbidden)
	}
}

func TestAuth_ControlScope(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	store := &fakeStore{}
	seedToken(store, []string{"control"})

	commandCh := make(chan *messenger.Message, 1)
	_, err = msg.Subscribe("plugin.dev1.ent1.command.turn_on", func(m *messenger.Message) {
		commandCh <- m
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := msg.Flush(); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	// Command should work.
	resp, err := authPost(srv.URL+"/entities/plugin/dev1/ent1/command/turn_on", "application/json", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("control-scoped POST command: got %d want %d", resp.StatusCode, http.StatusNoContent)
	}

	select {
	case <-commandCh:
	case <-time.After(2 * time.Second):
		t.Fatal("expected command publish")
	}

	// Read should fail.
	resp, err = authGet(srv.URL + "/devices")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("control-scoped GET /devices: got %d want %d", resp.StatusCode, http.StatusForbidden)
	}
}

func TestAuth_QueryIsReadScope(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	store := &fakeStore{
		queryResult: []storage.Entry{
			{Key: "plugin.dev1.ent1", Data: json.RawMessage(`{"id":"ent1"}`)},
		},
	}
	seedToken(store, []string{"read"})
	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	resp, err := authPost(srv.URL+"/query", "application/json", []byte(`{"pattern":"plugin.dev1.*"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("read-scoped POST /query: got %d want %d", resp.StatusCode, http.StatusOK)
	}
	resp.Body.Close()
}

func TestAuth_TokenCRUD(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	store := &fakeStore{}
	seedToken(store, []string{"admin"})
	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	// Create a new token.
	resp, err := authPost(srv.URL+"/tokens", "application/json",
		[]byte(`{"name":"Dashboard","scopes":["read"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("POST /tokens: got %d want %d body=%s", resp.StatusCode, http.StatusOK, body)
	}
	var created map[string]any
	decodeBody(t, resp, &created)
	if created["token"] == nil || created["token"] == "" {
		t.Fatalf("expected token secret in response: %+v", created)
	}

	// List tokens.
	resp, err = authGet(srv.URL + "/tokens")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /tokens: got %d want %d", resp.StatusCode, http.StatusOK)
	}
	resp.Body.Close()

	// Delete a token.
	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/tokens/some-id", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err = authDo(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE /tokens: got %d want %d", resp.StatusCode, http.StatusNoContent)
	}
	if store.deletedKey != "sb-api.tokens.some-id" {
		t.Fatalf("deleted key: got %q want %q", store.deletedKey, "sb-api.tokens.some-id")
	}
}

func TestAuth_TokenValidation(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	store := &fakeStore{}
	seedToken(store, []string{"admin"})
	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{"missing name", `{"scopes":["read"]}`, http.StatusUnprocessableEntity},
		{"missing scopes", `{"name":"Test"}`, http.StatusUnprocessableEntity},
		{"empty scopes", `{"name":"Test","scopes":[]}`, http.StatusUnprocessableEntity},
		{"invalid scope", `{"name":"Test","scopes":["root"]}`, http.StatusUnprocessableEntity},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := authPost(srv.URL+"/tokens", "application/json", []byte(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status: got %d want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Device routes
// ---------------------------------------------------------------------------

func TestDevicesRoutes_ListAndUpsert(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	store := &fakeStore{
		searchResult: []storage.Entry{
			{Key: "plugin.dev1", Data: json.RawMessage(`{"name":"Device 1"}`)},
			{Key: "plugin.dev1.ent1", Data: json.RawMessage(`{"name":"Entity 1"}`)},
		},
	}
	seedToken(store, []string{"read", "write", "control", "admin"})

	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	resp, err := authGet(srv.URL + "/devices")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusOK)
	}

	var devices []map[string]any
	decodeBody(t, resp, &devices)
	if len(devices) != 1 || devices[0]["key"] != "plugin.dev1" {
		t.Fatalf("unexpected devices response: %+v", devices)
	}

	req, err := http.NewRequest(http.MethodPut, srv.URL+"/devices/plugin/dev2", bytes.NewReader([]byte(`{
		"id":"dev2",
		"plugin":"plugin",
		"name":"Device 2",
		"labels":{"Area":["Basement"]},
		"profile":{"name":"Basement Device 2","id":"basement-device-2"},
		"entities":[]
	}`)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err = authDo(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusNoContent)
	}
	if store.savedKey != "plugin.dev2" {
		t.Fatalf("saved key: got %q want %q", store.savedKey, "plugin.dev2")
	}
	var got domain.Device
	if err := json.Unmarshal(store.savedBody, &got); err != nil {
		t.Fatalf("unmarshal saved device: %v", err)
	}
	if got.Plugin != "plugin" || got.ID != "dev2" || got.Name != "Device 2" {
		t.Fatalf("saved device: %+v", got)
	}
	if store.profileKey != "plugin.dev2" {
		t.Fatalf("profile key: got %q want %q", store.profileKey, "plugin.dev2")
	}
	var profile map[string]any
	if err := json.Unmarshal(store.profileBody, &profile); err != nil {
		t.Fatalf("unmarshal saved profile: %v", err)
	}
	labels, ok := profile["labels"].(map[string]any)
	if !ok || len(labels) != 1 {
		t.Fatalf("profile labels: %+v", profile)
	}
	prof, ok := profile["profile"].(map[string]any)
	if !ok || prof["id"] != "basement-device-2" {
		t.Fatalf("profile body: %+v", profile)
	}
}

func TestDevicesRoutes_SetProfile(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	store := &fakeStore{}
	seedToken(store, []string{"read", "write", "control", "admin"})
	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPatch, srv.URL+"/devices/plugin/dev2/profile", bytes.NewReader([]byte(`{
		"labels":{"Area":["Basement"],"Technology":["ESPHome"]},
		"profile":{"name":"Basement Device 2","id":"basement-device-2"}
	}`)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := authDo(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusNoContent)
	}
	if store.profileKey != "plugin.dev2" {
		t.Fatalf("profile key: got %q want %q", store.profileKey, "plugin.dev2")
	}
	var got map[string]any
	if err := json.Unmarshal(store.profileBody, &got); err != nil {
		t.Fatalf("unmarshal profile body: %v", err)
	}
	if _, ok := got["labels"]; !ok {
		t.Fatalf("profile body missing labels: %+v", got)
	}
	prof, ok := got["profile"].(map[string]any)
	if !ok || prof["id"] != "basement-device-2" {
		t.Fatalf("profile body: %+v", got)
	}
}

// ---------------------------------------------------------------------------
// Entity routes
// ---------------------------------------------------------------------------

func TestEntitiesCommandAndQueryRoutes(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	store := &fakeStore{
		getResult: json.RawMessage(`{"id":"ent1"}`),
		queryResult: []storage.Entry{
			{Key: "plugin.dev1.ent1", Data: json.RawMessage(`{"id":"ent1"}`)},
		},
	}
	seedToken(store, []string{"read", "write", "control", "admin"})

	commandCh := make(chan *messenger.Message, 1)
	_, err = msg.Subscribe("plugin.dev1.ent1.command.turn_on", func(m *messenger.Message) {
		commandCh <- m
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := msg.Flush(); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	resp, err := authGet(srv.URL + "/entities/plugin/dev1/ent1")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusOK)
	}
	var entity map[string]any
	decodeBody(t, resp, &entity)
	if entity["id"] != "ent1" {
		t.Fatalf("unexpected entity response: %+v", entity)
	}

	queryBody := `{"pattern":"plugin.dev1.*","where":[{"field":"state.power","op":"eq","value":true}]}`
	resp, err = authPost(srv.URL+"/query", "application/json", []byte(queryBody))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusOK)
	}
	var results []map[string]any
	decodeBody(t, resp, &results)
	if store.queryArg.Pattern != "plugin.dev1.*" || len(store.queryArg.Where) != 1 {
		t.Fatalf("unexpected query arg: %+v", store.queryArg)
	}
	if len(results) != 1 || results[0]["key"] != "plugin.dev1.ent1" {
		t.Fatalf("unexpected query response: %+v", results)
	}

	resp, err = authPost(srv.URL+"/entities/plugin/dev1/ent1/command/turn_on", "application/json", []byte(`{"level":42}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusNoContent)
	}

	select {
	case got := <-commandCh:
		if string(got.Data) != `{"level":42}` {
			t.Fatalf("command payload: got %s", string(got.Data))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected command publish")
	}
}

func TestEntitiesCommandRoutes_GroupScriptCommands(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	store := &fakeStore{}
	seedToken(store, []string{"read", "write", "control", "admin"})

	runCh := make(chan *messenger.Message, 1)
	_, err = msg.Subscribe("plugin-automation.group.basement.command.script_run", func(m *messenger.Message) {
		runCh <- m
	})
	if err != nil {
		t.Fatal(err)
	}

	stopCh := make(chan *messenger.Message, 1)
	_, err = msg.Subscribe("plugin-automation.group.basement.command.script_stop_all", func(m *messenger.Message) {
		stopCh <- m
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := msg.Flush(); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	resp, err := authPost(
		srv.URL+"/entities/plugin-automation/group/basement/command/script_run",
		"application/json",
		[]byte(`{"name":"party_time"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("script_run status: got %d want %d", resp.StatusCode, http.StatusNoContent)
	}

	select {
	case got := <-runCh:
		if string(got.Data) != `{"name":"party_time"}` {
			t.Fatalf("script_run payload: got %s", string(got.Data))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected script_run publish")
	}

	req, err := http.NewRequest(
		http.MethodPost,
		srv.URL+"/entities/plugin-automation/group/basement/command/script_stop_all",
		bytes.NewReader([]byte(`{}`)),
	)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err = authDo(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("script_stop_all status: got %d want %d", resp.StatusCode, http.StatusNoContent)
	}

	select {
	case got := <-stopCh:
		if string(got.Data) != `{}` {
			t.Fatalf("script_stop_all payload: got %s", string(got.Data))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected script_stop_all publish")
	}
}

func TestEntitiesRoutes_UpsertAndDelete(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	store := &fakeStore{}
	seedToken(store, []string{"read", "write", "control", "admin"})

	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	// PUT /entities — upsert
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/entities/plugin/dev1/ent1", bytes.NewReader([]byte(`{
		"id":"ent1",
		"plugin":"plugin",
		"deviceID":"dev1",
		"type":"switch",
		"name":"Outlet",
		"state":{"power":true}
	}`)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := authDo(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT status: got %d want %d", resp.StatusCode, http.StatusNoContent)
	}
	if store.savedKey != "plugin.dev1.ent1" {
		t.Fatalf("saved key: got %q want %q", store.savedKey, "plugin.dev1.ent1")
	}
	var got domain.Entity
	if err := json.Unmarshal(store.savedBody, &got); err != nil {
		t.Fatalf("unmarshal saved entity: %v", err)
	}
	if got.Type != "switch" || got.Name != "Outlet" {
		t.Fatalf("saved entity: %+v", got)
	}
	state, ok := got.State.(domain.Switch)
	if !ok {
		t.Fatalf("saved state type: got %T want domain.Switch", got.State)
	}
	if !state.Power {
		t.Fatalf("saved state: %+v", state)
	}

	// DELETE /entities
	req, err = http.NewRequest(http.MethodDelete, srv.URL+"/entities/plugin/dev1/ent1", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err = authDo(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE status: got %d want %d", resp.StatusCode, http.StatusNoContent)
	}
	if store.deletedKey != "plugin.dev1.ent1" {
		t.Fatalf("deleted key: got %q want %q", store.deletedKey, "plugin.dev1.ent1")
	}
}

func TestEntitiesRoutes_List(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	store := &fakeStore{
		searchResult: []storage.Entry{
			{Key: "plugin.dev1", Data: json.RawMessage(`{"name":"Device 1"}`)},
			{Key: "plugin.dev1.ent1", Data: json.RawMessage(`{"id":"ent1"}`)},
			{Key: "plugin.dev1.ent2", Data: json.RawMessage(`{"id":"ent2"}`)},
		},
	}
	seedToken(store, []string{"read", "write", "control", "admin"})

	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	resp, err := authGet(srv.URL + "/entities")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusOK)
	}
	var entities []map[string]any
	decodeBody(t, resp, &entities)
	if len(entities) != 3 {
		t.Fatalf("expected 3 entities, got %d: %+v", len(entities), entities)
	}
}

func TestEntitiesRoutes_UpsertValidation_KnownAndCustomTypes(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	store := &fakeStore{}
	seedToken(store, []string{"read", "write", "control", "admin"})
	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	t.Run("known light type uses typed state", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPut, srv.URL+"/entities/plugin/dev1/light1", bytes.NewReader([]byte(`{
			"id":"light1",
			"plugin":"plugin",
			"deviceID":"dev1",
			"type":"light",
			"name":"Lamp",
			"state":{"power":true,"brightness":123}
		}`)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := authDo(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusNoContent)
		}

		var got domain.Entity
		if err := json.Unmarshal(store.savedBody, &got); err != nil {
			t.Fatalf("unmarshal saved entity: %v", err)
		}
		state, ok := got.State.(domain.Light)
		if !ok {
			t.Fatalf("saved state type: got %T want domain.Light", got.State)
		}
		if !state.Power || state.Brightness != 123 {
			t.Fatalf("saved light state: %+v", state)
		}
	})

	t.Run("custom type preserves generic state", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPut, srv.URL+"/entities/plugin/dev1/custom1", bytes.NewReader([]byte(`{
			"id":"custom1",
			"plugin":"plugin",
			"deviceID":"dev1",
			"type":"custom_sensor",
			"name":"Custom Sensor",
			"state":{"reading":42,"unit":"widgets"}
		}`)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := authDo(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusNoContent)
		}

		var got domain.Entity
		if err := json.Unmarshal(store.savedBody, &got); err != nil {
			t.Fatalf("unmarshal saved entity: %v", err)
		}
		state, ok := got.State.(map[string]any)
		if !ok {
			t.Fatalf("saved state type: got %T want map[string]any", got.State)
		}
		if state["unit"] != "widgets" || state["reading"] != float64(42) {
			t.Fatalf("saved custom state: %+v", state)
		}
	})
}

func TestEntitiesRoutes_UpsertValidation_RejectsBadJSON(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	store := &fakeStore{}
	seedToken(store, []string{"read", "write", "control", "admin"})
	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "invalid json",
			body:       `{"id":"ent1",`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "path mismatch",
			body: `{
				"id":"ent2",
				"plugin":"plugin",
				"deviceID":"dev1",
				"type":"switch",
				"name":"Outlet",
				"state":{"power":true}
			}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "bad typed state",
			body: `{
				"id":"ent1",
				"plugin":"plugin",
				"deviceID":"dev1",
				"type":"light",
				"name":"Lamp",
				"state":{"power":"yes"}
			}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "missing required field",
			body: `{
				"id":"ent1",
				"plugin":"plugin",
				"deviceID":"dev1",
				"type":"switch",
				"state":{"power":true}
			}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store.savedKey = ""
			store.savedBody = nil

			req, err := http.NewRequest(http.MethodPut, srv.URL+"/entities/plugin/dev1/ent1", bytes.NewReader([]byte(tt.body)))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := authDo(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status: got %d want %d", resp.StatusCode, tt.wantStatus)
			}
			if store.savedKey != "" || store.savedBody != nil {
				t.Fatalf("expected no save on invalid payload, got key=%q body=%s", store.savedKey, string(store.savedBody))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Script routes
// ---------------------------------------------------------------------------

func TestScriptsRoutes_SaveDefinition(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	store := &fakeStore{}
	seedToken(store, []string{"read", "write", "control", "admin"})

	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	luaSource := "Automation(\"PartyTime\", function() return true end)"
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/scripts/PartyTime", bytes.NewReader([]byte(luaSource)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := authDo(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusNoContent)
	}
	if store.savedKey != "sb-script.scripts.PartyTime" {
		t.Fatalf("saved key: got %q want %q", store.savedKey, "sb-script.scripts.PartyTime")
	}
	var got map[string]any
	if err := json.Unmarshal(store.savedBody, &got); err != nil {
		t.Fatalf("unmarshal saved body: %v", err)
	}
	if got["type"] != "script" || got["language"] != "lua" || got["name"] != "PartyTime" || got["source"] != luaSource {
		t.Fatalf("unexpected saved body: %+v", got)
	}
}

func TestScriptsRoutes_Start(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	store := &fakeStore{}
	seedToken(store, []string{"read", "write", "control", "admin"})
	reqCh := make(chan map[string]any, 1)
	subscribeScriptAPI(t, msg, "script.start", func(m *messenger.Message) {
		var req map[string]any
		if err := json.Unmarshal(m.Data, &req); err != nil {
			t.Errorf("unmarshal start request: %v", err)
			return
		}
		reqCh <- req
		m.Respond([]byte(`{"ok":true,"hash":"abc123"}`))
	})

	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	resp, err := authPost(srv.URL+"/scripts/PartyTime/instances", "application/json", []byte(`{"queryRef":"room_main"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]string
	decodeBody(t, resp, &body)
	if body["hash"] != "abc123" {
		t.Fatalf("unexpected start response: %+v", body)
	}

	select {
	case got := <-reqCh:
		if got["name"] != "PartyTime" || got["queryRef"] != "room_main" {
			t.Fatalf("unexpected start request: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected script.start request")
	}
}

func TestScriptsRoutes_Stop(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	store := &fakeStore{}
	seedToken(store, []string{"read", "write", "control", "admin"})
	reqCh := make(chan map[string]any, 1)
	subscribeScriptAPI(t, msg, "script.stop", func(m *messenger.Message) {
		var req map[string]any
		if err := json.Unmarshal(m.Data, &req); err != nil {
			t.Errorf("unmarshal stop request: %v", err)
			return
		}
		reqCh <- req
		m.Respond([]byte(`{"ok":true}`))
	})

	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/scripts/PartyTime/instances/abc123", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := authDo(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusNoContent)
	}

	select {
	case got := <-reqCh:
		if got["name"] != "PartyTime" || got["hash"] != "abc123" {
			t.Fatalf("unexpected stop request: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected script.stop request")
	}
}

func TestScriptsRoutes_ListAndInstances(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	startedAt := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)
	lastFiredAt := startedAt.Add(30 * time.Second)
	nextFireAt := lastFiredAt.Add(5 * time.Second)

	store := &fakeStore{
		searchResultsByPattern: map[string][]storage.Entry{
			"sb-script.scripts.>": {
				{Key: "sb-script.scripts.PartyTime", Data: json.RawMessage(`{"name":"PartyTime","source":"Automation(\"PartyTime\", ...)"} `)},
				{Key: "sb-script.scripts.WelcomeHome", Data: json.RawMessage(`{"name":"WelcomeHome","source":"Automation(\"WelcomeHome\", ...)"}`)},
			},
			"sb-script.instances.>": {
				{Key: "sb-script.instances.a1b2", Data: mustJSON(t, map[string]any{
					"name":            "PartyTime",
					"queryRef":        "group_main_lb",
					"hash":            "a1b2",
					"status":          "running",
					"trigger":         map[string]any{"kind": "interval", "minSeconds": 5.0, "maxSeconds": 5.0},
					"targets":         map[string]any{"kind": "query_ref", "queryRef": "group_main_lb"},
					"resolvedTargets": []string{"esphome.main_lb_01.light", "esphome.main_lb_02.light"},
					"startedAt":       startedAt.Format(time.RFC3339Nano),
					"lastFiredAt":     lastFiredAt.Format(time.RFC3339Nano),
					"nextFireAt":      nextFireAt.Format(time.RFC3339Nano),
					"fireCount":       3,
				})},
			},
		},
	}
	seedToken(store, []string{"read", "write", "control", "admin"})

	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	// GET /scripts — list all scripts with instances
	resp, err := authGet(srv.URL + "/scripts")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusOK)
	}
	var scripts []map[string]any
	decodeBody(t, resp, &scripts)
	if len(scripts) != 2 {
		t.Fatalf("scripts len: got %d want 2", len(scripts))
	}
	if scripts[0]["name"] != "PartyTime" || scripts[1]["name"] != "WelcomeHome" {
		t.Fatalf("unexpected scripts order/body: %+v", scripts)
	}
	if scripts[0]["running"] != true {
		t.Fatalf("expected PartyTime running: %+v", scripts[0])
	}
	instances, ok := scripts[0]["instances"].([]any)
	if !ok || len(instances) != 1 {
		t.Fatalf("expected one running instance, got %+v", scripts[0]["instances"])
	}

	// GET /scripts/PartyTime/instances — list instances for a specific script
	resp, err = authGet(srv.URL + "/scripts/PartyTime/instances")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusOK)
	}
	var running []map[string]any
	decodeBody(t, resp, &running)
	if len(running) != 1 || running[0]["hash"] != "a1b2" {
		t.Fatalf("unexpected instances response: %+v", running)
	}
	if running[0]["name"] != "PartyTime" || running[0]["queryRef"] != "group_main_lb" {
		t.Fatalf("unexpected instance body: %+v", running[0])
	}
}

func TestScriptsRoutes_GetOne(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	store := &fakeStore{
		searchResultsByPattern: map[string][]storage.Entry{
			"sb-script.scripts.>": {
				{Key: "sb-script.scripts.PartyTime", Data: json.RawMessage(`{"name":"PartyTime","source":"Automation(\"PartyTime\", ...)"}`)},
			},
			"sb-script.instances.>": {
				{Key: "sb-script.instances.a1b2", Data: json.RawMessage(`{"name":"PartyTime","queryRef":"group_main_lb","hash":"a1b2","status":"running","targets":{"kind":"query_ref","queryRef":"group_main_lb"}}`)},
			},
		},
	}
	seedToken(store, []string{"read", "write", "control", "admin"})

	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	resp, err := authGet(srv.URL + "/scripts/PartyTime")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusOK)
	}
	var script map[string]any
	decodeBody(t, resp, &script)
	if script["name"] != "PartyTime" || script["running"] != true {
		t.Fatalf("unexpected script response: %+v", script)
	}

	resp, err = authGet(srv.URL + "/scripts/Missing")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing status: got %d want %d", resp.StatusCode, http.StatusNotFound)
	}
}

// ---------------------------------------------------------------------------
// Command validation
// ---------------------------------------------------------------------------

func TestCommandRoute_RejectsMalformedPayload(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	store := &fakeStore{}
	seedToken(store, []string{"read", "write", "control", "admin"})

	published := make(chan struct{}, 1)
	if _, err := msg.Subscribe("plugin.dev1.light1.command.light_set_brightness", func(m *messenger.Message) {
		published <- struct{}{}
	}); err != nil {
		t.Fatal(err)
	}
	if err := msg.Flush(); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	resp, err := authPost(
		srv.URL+"/entities/plugin/dev1/light1/command/light_set_brightness",
		"application/json",
		[]byte(`{"brightness":"potato"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: got %d want %d (malformed payload should be rejected)", resp.StatusCode, http.StatusBadRequest)
	}

	select {
	case <-published:
		t.Fatal("command was published to NATS despite malformed payload")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestCommandRoute_KnownActionValidPayload(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	store := &fakeStore{}
	seedToken(store, []string{"read", "write", "control", "admin"})

	published := make(chan []byte, 1)
	if _, err := msg.Subscribe("plugin.dev1.light1.command.light_set_brightness", func(m *messenger.Message) {
		published <- m.Data
	}); err != nil {
		t.Fatal(err)
	}
	if err := msg.Flush(); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	resp, err := authPost(
		srv.URL+"/entities/plugin/dev1/light1/command/light_set_brightness",
		"application/json",
		[]byte(`{"brightness":200}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusNoContent)
	}

	select {
	case data := <-published:
		var cmd domain.LightSetBrightness
		if err := json.Unmarshal(data, &cmd); err != nil {
			t.Fatalf("unmarshal published command: %v", err)
		}
		if cmd.Brightness != 200 {
			t.Fatalf("brightness: got %d want 200", cmd.Brightness)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for command publish")
	}
}

func TestCommandRoute_UnknownActionPassesThrough(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	store := &fakeStore{}
	seedToken(store, []string{"read", "write", "control", "admin"})

	published := make(chan []byte, 1)
	if _, err := msg.Subscribe("plugin-automation.group.living.command.script_run", func(m *messenger.Message) {
		published <- m.Data
	}); err != nil {
		t.Fatal(err)
	}
	if err := msg.Flush(); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	resp, err := authPost(
		srv.URL+"/entities/plugin-automation/group/living/command/script_run",
		"application/json",
		[]byte(`{"name":"party_time"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status: got %d want %d (unknown actions should pass through)", resp.StatusCode, http.StatusNoContent)
	}

	select {
	case data := <-published:
		if string(data) != `{"name":"party_time"}` {
			t.Fatalf("payload: got %s", string(data))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for plugin-specific command")
	}
}
