package routes

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	domain "github.com/slidebolt/sb-domain"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	scheduler "github.com/slidebolt/sb-scheduler-sdk"
)

type TimerCreateInput struct{ Body scheduler.CreateTimerRequest }
type TimerUpdateInput struct {
	ID   string `path:"id"`
	Body struct {
		Name  string       `json:"name,omitempty"`
		Timer domain.Timer `json:"timer"`
	}
}
type TimerDeleteInput struct {
	ID string `path:"id"`
}
type TimerOutput struct {
	Body struct {
		Key string `json:"key"`
	}
}

func RegisterTimers(api huma.API, msg messenger.Messenger) {
	client := scheduler.NewClient(msg)
	huma.Register(api, huma.Operation{Method: "POST", Path: "/timers", Summary: "Create timer", Tags: []string{"timers"}}, func(ctx context.Context, input *TimerCreateInput) (*TimerOutput, error) {
		key, err := client.CreateTimer(input.Body)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("create timer failed", err)
		}
		out := &TimerOutput{}
		out.Body.Key = key.Key()
		return out, nil
	})
	huma.Register(api, huma.Operation{Method: "PUT", Path: "/timers/{id}", Summary: "Update timer", Tags: []string{"timers"}}, func(ctx context.Context, input *TimerUpdateInput) (*TimerOutput, error) {
		key := domain.EntityKey{Plugin: "sb-scheduler", DeviceID: "timer-" + input.ID, ID: "timer"}
		if err := client.UpdateTimer(scheduler.UpdateTimerRequest{Key: key, Name: input.Body.Name, Timer: input.Body.Timer}); err != nil {
			return nil, huma.Error422UnprocessableEntity("update timer failed", err)
		}
		out := &TimerOutput{}
		out.Body.Key = key.Key()
		return out, nil
	})
	huma.Register(api, huma.Operation{Method: "DELETE", Path: "/timers/{id}", Summary: "Delete timer", Tags: []string{"timers"}}, func(ctx context.Context, input *TimerDeleteInput) (*struct{}, error) {
		key := domain.EntityKey{Plugin: "sb-scheduler", DeviceID: "timer-" + input.ID, ID: "timer"}
		if err := client.DeleteTimer(key); err != nil {
			return nil, huma.Error422UnprocessableEntity("delete timer failed", err)
		}
		return &struct{}{}, nil
	})
}
