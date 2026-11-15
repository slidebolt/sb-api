package mcptools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	domain "github.com/slidebolt/sb-domain"
	storage "github.com/slidebolt/sb-storage-sdk"
)

type mediaTestStore struct{ entries map[string]json.RawMessage }

func newMediaTestStore() *mediaTestStore {
	return &mediaTestStore{entries: map[string]json.RawMessage{}}
}
func (s *mediaTestStore) Save(v storage.Keyed) error {
	b, e := json.Marshal(v)
	s.entries[v.Key()] = b
	return e
}
func (s *mediaTestStore) Get(k storage.Keyed) (json.RawMessage, error) {
	return s.entries[k.Key()], nil
}
func (s *mediaTestStore) Delete(k storage.Keyed) error { delete(s.entries, k.Key()); return nil }
func (s *mediaTestStore) Search(_ string) ([]storage.Entry, error) {
	out := make([]storage.Entry, 0, len(s.entries))
	for k, v := range s.entries {
		out = append(out, storage.Entry{Key: k, Data: v})
	}
	return out, nil
}
func (s *mediaTestStore) SearchFiles(storage.StorageTarget, string) ([]storage.Entry, error) {
	return nil, nil
}
func (s *mediaTestStore) Query(storage.Query) ([]storage.Entry, error) { return nil, nil }
func (s *mediaTestStore) WriteFile(storage.StorageTarget, storage.Keyed, json.RawMessage) error {
	return nil
}
func (s *mediaTestStore) ReadFile(storage.StorageTarget, storage.Keyed) (json.RawMessage, error) {
	return nil, nil
}
func (s *mediaTestStore) DeleteFile(storage.StorageTarget, storage.Keyed) error { return nil }
func (s *mediaTestStore) SetPrivate(storage.Keyed, json.RawMessage) error       { return nil }
func (s *mediaTestStore) GetPrivate(storage.Keyed) (json.RawMessage, error)     { return nil, nil }
func (s *mediaTestStore) DeletePrivate(storage.Keyed) error                     { return nil }
func (s *mediaTestStore) SetInternal(storage.Keyed, json.RawMessage) error      { return nil }
func (s *mediaTestStore) GetInternal(storage.Keyed) (json.RawMessage, error)    { return nil, nil }
func (s *mediaTestStore) DeleteInternal(storage.Keyed) error                    { return nil }
func (s *mediaTestStore) SetProfile(storage.Keyed, json.RawMessage) error       { return nil }
func (s *mediaTestStore) Close()                                                {}

func TestFindRecorderUsesAdvertisedGenericCommand(t *testing.T) {
	store := newMediaTestStore()
	if err := store.Save(domain.Entity{ID: "dvr", Plugin: "plugin-media", DeviceID: "server", Type: "recorder", Commands: []string{"record_start"}, State: domain.Recorder{Status: "idle"}}); err != nil {
		t.Fatal(err)
	}
	got, err := findRecorder(store, "plugin-media", "server")
	if err != nil || got.ID != "dvr" {
		t.Fatalf("recorder = %+v, %v", got, err)
	}
}

func TestWaitForAiringScheduleRequiresScheduleEntity(t *testing.T) {
	store := newMediaTestStore()
	input, _ := json.Marshal(map[string]any{"metadata": map[string]any{"program": map[string]any{"airingID": "air-7"}}})
	state := domain.Schedule{Enabled: true, Steps: []domain.ScheduleStep{{ID: "start", Actions: []domain.Action{{Target: "plugin-media.server.dvr", Command: "record_start", Input: input}}}}}
	if err := store.Save(domain.Entity{ID: "recording-air-7", Plugin: "plugin-media", DeviceID: "server", Type: "schedule", State: state}); err != nil {
		t.Fatal(err)
	}
	if got := waitForAiringSchedule(context.Background(), store, "plugin-media", "server", "air-7", time.Second); got != "plugin-media.server.recording-air-7" {
		t.Fatalf("schedule = %q", got)
	}
}

func TestOptionalTime(t *testing.T) {
	fallback := time.Unix(10, 0)
	if got, err := optionalTime("", fallback); err != nil || !got.Equal(fallback) {
		t.Fatalf("fallback = %v, %v", got, err)
	}
	if _, err := optionalTime("not-a-time", fallback); err == nil {
		t.Fatal("invalid time accepted")
	}
}
