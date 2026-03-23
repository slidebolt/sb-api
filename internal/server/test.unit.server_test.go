package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	domain "github.com/slidebolt/sb-domain"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"
)

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

	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/devices")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusOK)
	}

	var devices []map[string]any
	decodeBody(t, resp, &devices)
	if store.searchPattern != "*.*" {
		t.Fatalf("search pattern: got %q want %q", store.searchPattern, "*.*")
	}
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
	resp, err = http.DefaultClient.Do(req)
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
	resp, err := http.DefaultClient.Do(req)
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

	resp, err := http.Get(srv.URL + "/entities/plugin/dev1/ent1")
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
	resp, err = http.Post(srv.URL+"/query", "application/json", bytes.NewReader([]byte(queryBody)))
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

	resp, err = http.Post(srv.URL+"/entities/plugin/dev1/ent1/command/turn_on", "application/json", bytes.NewReader([]byte(`{"level":42}`)))
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

	resp, err := http.Post(
		srv.URL+"/entities/plugin-automation/group/basement/command/script_run",
		"application/json",
		bytes.NewReader([]byte(`{"name":"party_time"}`)),
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
	resp, err = http.DefaultClient.Do(req)
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
	resp, err := http.DefaultClient.Do(req)
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
	resp, err = http.DefaultClient.Do(req)
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

	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/entities")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusOK)
	}
	var entities []map[string]any
	decodeBody(t, resp, &entities)
	if store.searchPattern != "*.*.*" {
		t.Fatalf("search pattern: got %q want %q", store.searchPattern, "*.*.*")
	}
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
		resp, err := http.DefaultClient.Do(req)
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
		resp, err := http.DefaultClient.Do(req)
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
			resp, err := http.DefaultClient.Do(req)
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

func TestScriptsRoutes_SaveDefinition(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	store := &fakeStore{}

	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	luaSource := "Automation(\"PartyTime\", function() return true end)"
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/scripts/PartyTime", bytes.NewReader([]byte(luaSource)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := http.DefaultClient.Do(req)
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

	resp, err := http.Post(srv.URL+"/scripts/PartyTime/start", "application/json", bytes.NewReader([]byte(`{"queryRef":"room_main"}`)))
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

	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/scripts/PartyTime/start", bytes.NewReader([]byte(`{"queryRef":"room_main"}`)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusNoContent)
	}

	select {
	case got := <-reqCh:
		if got["name"] != "PartyTime" || got["queryRef"] != "room_main" {
			t.Fatalf("unexpected stop request: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected script.stop request")
	}
}

func TestAutomationsRoutes_ListAndRunning(t *testing.T) {
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

	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/automations")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusOK)
	}
	var automations []map[string]any
	decodeBody(t, resp, &automations)
	if len(automations) != 2 {
		t.Fatalf("automations len: got %d want 2", len(automations))
	}
	if automations[0]["name"] != "PartyTime" || automations[1]["name"] != "WelcomeHome" {
		t.Fatalf("unexpected automations order/body: %+v", automations)
	}
	if automations[0]["running"] != true {
		t.Fatalf("expected PartyTime running: %+v", automations[0])
	}
	instances, ok := automations[0]["instances"].([]any)
	if !ok || len(instances) != 1 {
		t.Fatalf("expected one running instance, got %+v", automations[0]["instances"])
	}
	if len(store.searchPatterns) < 2 || store.searchPatterns[0] != "sb-script.scripts.>" || store.searchPatterns[1] != "sb-script.instances.>" {
		t.Fatalf("unexpected search patterns: %+v", store.searchPatterns)
	}

	resp, err = http.Get(srv.URL + "/automations/running")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusOK)
	}
	var running []map[string]any
	decodeBody(t, resp, &running)
	if len(running) != 1 || running[0]["hash"] != "a1b2" {
		t.Fatalf("unexpected running response: %+v", running)
	}
	if running[0]["name"] != "PartyTime" || running[0]["queryRef"] != "group_main_lb" {
		t.Fatalf("unexpected running instance body: %+v", running[0])
	}
}

func TestAutomationsRoutes_GetOne(t *testing.T) {
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

	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/automations/PartyTime")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusOK)
	}
	var automation map[string]any
	decodeBody(t, resp, &automation)
	if automation["name"] != "PartyTime" || automation["running"] != true {
		t.Fatalf("unexpected automation response: %+v", automation)
	}

	resp, err = http.Get(srv.URL + "/automations/Missing")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing status: got %d want %d", resp.StatusCode, http.StatusNotFound)
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
