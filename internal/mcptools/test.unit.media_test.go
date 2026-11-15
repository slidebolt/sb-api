package mcptools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	domain "github.com/slidebolt/sb-domain"
	testkit "github.com/slidebolt/sb-testkit"
)

func TestFindRecorderUsesAdvertisedGenericCommand(t *testing.T) {
	env := testkit.NewTestEnv(t)
	env.Start("storage")
	store := env.Storage()
	if err := store.Save(domain.Entity{ID: "dvr", Plugin: "plugin-media", DeviceID: "server", Type: "recorder", Commands: []string{"record_start"}, State: domain.Recorder{Status: "idle"}}); err != nil {
		t.Fatal(err)
	}
	got, err := findRecorder(store, "plugin-media", "server")
	if err != nil || got.ID != "dvr" {
		t.Fatalf("recorder = %+v, %v", got, err)
	}
}

func TestWaitForAiringScheduleRequiresScheduleEntity(t *testing.T) {
	env := testkit.NewTestEnv(t)
	env.Start("storage")
	store := env.Storage()
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
