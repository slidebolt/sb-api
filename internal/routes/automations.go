package routes

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	storage "github.com/slidebolt/sb-storage-sdk"
)

type automationDefinition struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

type automationTrigger struct {
	Kind       string  `json:"kind,omitempty"`
	QueryRef   string  `json:"queryRef,omitempty"`
	Query      string  `json:"query,omitempty"`
	MinSeconds float64 `json:"minSeconds,omitempty"`
	MaxSeconds float64 `json:"maxSeconds,omitempty"`
}

type automationTargets struct {
	Kind     string `json:"kind,omitempty"`
	QueryRef string `json:"queryRef,omitempty"`
	Query    string `json:"query,omitempty"`
}

type automationInstance struct {
	Name            string            `json:"name"`
	QueryRef        string            `json:"queryRef,omitempty"`
	Hash            string            `json:"hash"`
	Status          string            `json:"status,omitempty"`
	Trigger         automationTrigger `json:"trigger,omitempty"`
	Targets         automationTargets `json:"targets,omitempty"`
	ResolvedTargets []string          `json:"resolvedTargets,omitempty"`
	StartedAt       *time.Time        `json:"startedAt,omitempty"`
	LastFiredAt     *time.Time        `json:"lastFiredAt,omitempty"`
	NextFireAt      *time.Time        `json:"nextFireAt,omitempty"`
	LastError       string            `json:"lastError,omitempty"`
	FireCount       int               `json:"fireCount,omitempty"`
	State           map[string]any    `json:"state,omitempty"`
}

type Automation struct {
	Name      string               `json:"name"`
	Source    string               `json:"source"`
	Running   bool                 `json:"running"`
	Instances []automationInstance `json:"instances,omitempty"`
}

type AutomationsOutput struct {
	Body []Automation
}

type AutomationOutput struct {
	Body Automation
}

type RunningAutomationsOutput struct {
	Body []automationInstance
}

func RegisterAutomations(api huma.API, store storage.Storage) {
	huma.Register(api, huma.Operation{
		Method:      "GET",
		Path:        "/automations",
		Summary:     "List automations",
		Description: "Returns saved automation definitions with any running instances.",
		Tags:        []string{"automations"},
	}, func(ctx context.Context, _ *struct{}) (*AutomationsOutput, error) {
		automations, err := loadAutomations(store)
		if err != nil {
			return nil, huma.Error500InternalServerError("load automations failed", err)
		}
		return &AutomationsOutput{Body: automations}, nil
	})

	huma.Register(api, huma.Operation{
		Method:      "GET",
		Path:        "/automations/{name}",
		Summary:     "Get an automation",
		Description: "Returns a saved automation definition and its running instances.",
		Tags:        []string{"automations"},
	}, func(ctx context.Context, input *struct {
		Name string `path:"name"`
	}) (*AutomationOutput, error) {
		automations, err := loadAutomations(store)
		if err != nil {
			return nil, huma.Error500InternalServerError("load automations failed", err)
		}
		for _, automation := range automations {
			if automation.Name == input.Name {
				return &AutomationOutput{Body: automation}, nil
			}
		}
		return nil, huma.Error404NotFound("automation not found")
	})

	huma.Register(api, huma.Operation{
		Method:      "GET",
		Path:        "/automations/running",
		Summary:     "List running automations",
		Description: "Returns currently running automation instances.",
		Tags:        []string{"automations"},
	}, func(ctx context.Context, _ *struct{}) (*RunningAutomationsOutput, error) {
		instances, err := loadAutomationInstances(store)
		if err != nil {
			return nil, huma.Error500InternalServerError("load automation instances failed", err)
		}
		sort.Slice(instances, func(i, j int) bool {
			if instances[i].Name == instances[j].Name {
				return instances[i].Hash < instances[j].Hash
			}
			return instances[i].Name < instances[j].Name
		})
		return &RunningAutomationsOutput{Body: instances}, nil
	})
}

func loadAutomations(store storage.Storage) ([]Automation, error) {
	defEntries, err := store.Search("sb-script.scripts.>")
	if err != nil {
		return nil, err
	}
	instEntries, err := store.Search("sb-script.instances.>")
	if err != nil {
		return nil, err
	}

	byName := map[string]*Automation{}
	for _, entry := range defEntries {
		if strings.Count(entry.Key, ".") < 2 {
			continue
		}
		var def automationDefinition
		if err := json.Unmarshal(entry.Data, &def); err != nil {
			continue
		}
		byName[def.Name] = &Automation{Name: def.Name, Source: def.Source}
	}

	for _, entry := range instEntries {
		var inst automationInstance
		if err := json.Unmarshal(entry.Data, &inst); err != nil {
			continue
		}
		a, ok := byName[inst.Name]
		if !ok {
			a = &Automation{Name: inst.Name}
			byName[inst.Name] = a
		}
		a.Running = true
		a.Instances = append(a.Instances, inst)
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]Automation, 0, len(names))
	for _, name := range names {
		a := byName[name]
		sort.Slice(a.Instances, func(i, j int) bool { return a.Instances[i].Hash < a.Instances[j].Hash })
		out = append(out, *a)
	}
	return out, nil
}

func loadAutomationInstances(store storage.Storage) ([]automationInstance, error) {
	instEntries, err := store.Search("sb-script.instances.>")
	if err != nil {
		return nil, err
	}
	out := make([]automationInstance, 0, len(instEntries))
	for _, entry := range instEntries {
		var inst automationInstance
		if err := json.Unmarshal(entry.Data, &inst); err != nil {
			continue
		}
		out = append(out, inst)
	}
	return out, nil
}
