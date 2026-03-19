package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"
)

type fakeStore struct {
	searchPattern string
	queryArg      storage.Query
	savedKey      string
	savedBody     json.RawMessage
	deletedKey    string

	searchResult []storage.Entry
	queryResult  []storage.Entry
	getResult    json.RawMessage
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
	return f.searchResult, nil
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

	req, err := http.NewRequest(http.MethodPut, srv.URL+"/devices/plugin/dev2", bytes.NewReader([]byte(`{"name":"Device 2"}`)))
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
	if string(store.savedBody) != `{"name":"Device 2"}` {
		t.Fatalf("saved body: got %s", string(store.savedBody))
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
