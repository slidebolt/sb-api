package routes

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	domain "github.com/slidebolt/sb-domain"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	scheduler "github.com/slidebolt/sb-scheduler-sdk"
)

type ScheduleCreateInput struct {
	Body scheduler.CreateScheduleRequest
}
type ScheduleUpdateInput struct {
	ID   string `path:"id"`
	Body struct {
		Name     string          `json:"name,omitempty"`
		Schedule domain.Schedule `json:"schedule"`
	}
}
type ScheduleDeleteInput struct {
	ID string `path:"id"`
}
type ScheduleOutput struct {
	Body struct {
		Key string `json:"key"`
	}
}

func RegisterSchedules(api huma.API, msg messenger.Messenger) {
	client := scheduler.NewClient(msg)
	huma.Register(api, huma.Operation{Method: "POST", Path: "/schedules", Summary: "Create schedule", Tags: []string{"schedules"}}, func(ctx context.Context, input *ScheduleCreateInput) (*ScheduleOutput, error) {
		key, err := client.CreateSchedule(input.Body)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("create schedule failed", err)
		}
		out := &ScheduleOutput{}
		out.Body.Key = key.Key()
		return out, nil
	})
	huma.Register(api, huma.Operation{Method: "PUT", Path: "/schedules/{id}", Summary: "Update schedule", Tags: []string{"schedules"}}, func(ctx context.Context, input *ScheduleUpdateInput) (*ScheduleOutput, error) {
		key := domain.EntityKey{Plugin: "sb-scheduler", DeviceID: "schedule-" + input.ID, ID: "schedule"}
		if err := client.UpdateSchedule(scheduler.UpdateScheduleRequest{Key: key, Name: input.Body.Name, Schedule: input.Body.Schedule}); err != nil {
			return nil, huma.Error422UnprocessableEntity("update schedule failed", err)
		}
		out := &ScheduleOutput{}
		out.Body.Key = key.Key()
		return out, nil
	})
	huma.Register(api, huma.Operation{Method: "DELETE", Path: "/schedules/{id}", Summary: "Delete schedule", Tags: []string{"schedules"}}, func(ctx context.Context, input *ScheduleDeleteInput) (*struct{}, error) {
		key := domain.EntityKey{Plugin: "sb-scheduler", DeviceID: "schedule-" + input.ID, ID: "schedule"}
		if err := client.DeleteSchedule(key); err != nil {
			return nil, huma.Error422UnprocessableEntity("delete schedule failed", err)
		}
		return &struct{}{}, nil
	})
}
