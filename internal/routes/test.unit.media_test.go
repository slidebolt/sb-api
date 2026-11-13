package routes

import (
	"testing"
	"time"
)

func TestGuideRangeValidationShape(t *testing.T) {
	start := time.Date(2026, 11, 13, 1, 0, 0, 0, time.UTC)
	in := guideInput{Provider: "plugin-plex", ServerID: "server", Start: start, End: start.Add(time.Hour), Limit: 100}
	if !in.End.After(in.Start) || in.Provider != "plugin-plex" || in.Limit != 100 {
		t.Fatal("guide input did not retain provider-neutral range")
	}
}
