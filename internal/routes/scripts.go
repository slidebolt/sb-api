package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"
)

const scriptRequestTimeout = 5 * time.Second

type scriptAPIResponse struct {
	OK    bool   `json:"ok"`
	Hash  string `json:"hash,omitempty"`
	Error string `json:"error,omitempty"`
}

type ScriptDefinitionInput struct {
	Name    string `path:"name"`
	RawBody []byte `contentType:"text/plain"`
}

type ScriptInstanceStartInput struct {
	Name string `path:"name"`
	Body struct {
		QueryRef string `json:"queryRef,omitempty"`
	}
}

type ScriptInstanceStopInput struct {
	Name string `path:"name"`
	Hash string `path:"hash"`
}

type ScriptStartOutput struct {
	Body struct {
		Hash string `json:"hash"`
	}
}

type scriptDefinition struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

type scriptTrigger struct {
	Kind       string  `json:"kind,omitempty"`
	QueryRef   string  `json:"queryRef,omitempty"`
	Query      string  `json:"query,omitempty"`
	MinSeconds float64 `json:"minSeconds,omitempty"`
	MaxSeconds float64 `json:"maxSeconds,omitempty"`
}

type scriptTargets struct {
	Kind     string `json:"kind,omitempty"`
	QueryRef string `json:"queryRef,omitempty"`
	Query    string `json:"query,omitempty"`
}

type ScriptInstance struct {
	Name            string        `json:"name"`
	QueryRef        string        `json:"queryRef,omitempty"`
	Hash            string        `json:"hash"`
	Status          string        `json:"status,omitempty"`
	Trigger         scriptTrigger `json:"trigger,omitempty"`
	Targets         scriptTargets `json:"targets,omitempty"`
	ResolvedTargets []string      `json:"resolvedTargets,omitempty"`
	StartedAt       *time.Time    `json:"startedAt,omitempty"`
	LastFiredAt     *time.Time    `json:"lastFiredAt,omitempty"`
	NextFireAt      *time.Time    `json:"nextFireAt,omitempty"`
	LastError       string        `json:"lastError,omitempty"`
	FireCount       int           `json:"fireCount,omitempty"`
	State           map[string]any `json:"state,omitempty"`
}

type Script struct {
	Name      string           `json:"name"`
	Source    string           `json:"source"`
	Running   bool             `json:"running"`
	Instances []ScriptInstance `json:"instances,omitempty"`
}

type ScriptsListOutput struct {
	Body []Script
}

type ScriptGetOutput struct {
	Body Script
}

type ScriptInstancesOutput struct {
	Body []ScriptInstance
}

func RegisterScripts(api huma.API, store storage.Storage, msg messenger.Messenger) {
	// GET /scripts — list all scripts with their instances
	huma.Register(api, huma.Operation{
		Method:      "GET",
		Path:        "/scripts",
		Summary:     "List scripts",
		Description: "Returns saved script definitions with any running instances.",
		Tags:        []string{"scripts"},
	}, func(ctx context.Context, _ *struct{}) (*ScriptsListOutput, error) {
		scripts, err := loadScripts(store)
		if err != nil {
			return nil, huma.Error500InternalServerError("load scripts failed", err)
		}
		return &ScriptsListOutput{Body: scripts}, nil
	})

	// GET /scripts/{name} — get a single script with its instances
	huma.Register(api, huma.Operation{
		Method:      "GET",
		Path:        "/scripts/{name}",
		Summary:     "Get a script",
		Description: "Returns a saved script definition and its running instances.",
		Tags:        []string{"scripts"},
	}, func(ctx context.Context, input *struct {
		Name string `path:"name"`
	}) (*ScriptGetOutput, error) {
		scripts, err := loadScripts(store)
		if err != nil {
			return nil, huma.Error500InternalServerError("load scripts failed", err)
		}
		for _, s := range scripts {
			if s.Name == input.Name {
				return &ScriptGetOutput{Body: s}, nil
			}
		}
		return nil, huma.Error404NotFound("script not found")
	})

	// PUT /scripts/{name} — save or update a script definition
	huma.Register(api, huma.Operation{
		Method:      "PUT",
		Path:        "/scripts/{name}",
		Summary:     "Save or update a script definition",
		Description: "Stores a Lua script definition in storage under the canonical sb-script.scripts.* keyspace.",
		Tags:        []string{"scripts"},
	}, func(ctx context.Context, input *ScriptDefinitionInput) (*struct{}, error) {
		body, err := json.Marshal(map[string]string{
			"type":     "script",
			"language": "lua",
			"name":     input.Name,
			"source":   string(input.RawBody),
		})
		if err != nil {
			return nil, huma.Error500InternalServerError("marshal failed", err)
		}
		key := rawKeyed{
			key:  "sb-script.scripts." + input.Name,
			data: body,
		}
		if err := store.Save(key); err != nil {
			return nil, huma.Error500InternalServerError("save failed", err)
		}
		return nil, nil
	})

	// GET /scripts/{name}/instances — list running instances for a script
	huma.Register(api, huma.Operation{
		Method:      "GET",
		Path:        "/scripts/{name}/instances",
		Summary:     "List running instances",
		Description: "Returns currently running instances for a script.",
		Tags:        []string{"scripts"},
	}, func(ctx context.Context, input *struct {
		Name string `path:"name"`
	}) (*ScriptInstancesOutput, error) {
		instances, err := loadScriptInstances(store)
		if err != nil {
			return nil, huma.Error500InternalServerError("load script instances failed", err)
		}
		var filtered []ScriptInstance
		for _, inst := range instances {
			if inst.Name == input.Name {
				filtered = append(filtered, inst)
			}
		}
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].Hash < filtered[j].Hash
		})
		return &ScriptInstancesOutput{Body: filtered}, nil
	})

	// POST /scripts/{name}/instances — start a new instance
	huma.Register(api, huma.Operation{
		Method:      "POST",
		Path:        "/scripts/{name}/instances",
		Summary:     "Start a script instance",
		Description: "Starts a named script in sb-script via NATS request/reply.",
		Tags:        []string{"scripts"},
	}, func(ctx context.Context, input *ScriptInstanceStartInput) (*ScriptStartOutput, error) {
		req := map[string]any{
			"name":     input.Name,
			"queryRef": input.Body.QueryRef,
		}
		var resp scriptAPIResponse
		if err := requestScriptAPI(msg, "script.start", req, &resp); err != nil {
			return nil, err
		}
		out := &ScriptStartOutput{}
		out.Body.Hash = resp.Hash
		return out, nil
	})

	// DELETE /scripts/{name}/instances/{hash} — stop a specific instance
	huma.Register(api, huma.Operation{
		Method:      "DELETE",
		Path:        "/scripts/{name}/instances/{hash}",
		Summary:     "Stop a script instance",
		Description: "Stops a specific script instance in sb-script via NATS request/reply.",
		Tags:        []string{"scripts"},
	}, func(ctx context.Context, input *ScriptInstanceStopInput) (*struct{}, error) {
		req := map[string]any{
			"name": input.Name,
			"hash": input.Hash,
		}
		if err := requestScriptAPI(msg, "script.stop", req, nil); err != nil {
			return nil, err
		}
		return nil, nil
	})
}

func requestScriptAPI(msg messenger.Messenger, subject string, body any, dest *scriptAPIResponse) error {
	data, err := json.Marshal(body)
	if err != nil {
		return huma.Error500InternalServerError("marshal failed", err)
	}

	respMsg, err := msg.Request(subject, data, scriptRequestTimeout)
	if err != nil {
		return huma.Error500InternalServerError("script request failed", err)
	}

	var resp scriptAPIResponse
	if err := json.Unmarshal(respMsg.Data, &resp); err != nil {
		return huma.Error502BadGateway("invalid script response", err)
	}
	if !resp.OK {
		return huma.Error502BadGateway(fmt.Sprintf("script engine error: %s", resp.Error))
	}
	if dest != nil {
		*dest = resp
	}
	return nil
}

func loadScripts(store storage.Storage) ([]Script, error) {
	defEntries, err := store.Search("sb-script.scripts.>")
	if err != nil {
		return nil, err
	}
	instEntries, err := store.Search("sb-script.instances.>")
	if err != nil {
		return nil, err
	}

	byName := map[string]*Script{}
	for _, entry := range defEntries {
		if strings.Count(entry.Key, ".") < 2 {
			continue
		}
		var def scriptDefinition
		if err := json.Unmarshal(entry.Data, &def); err != nil {
			continue
		}
		byName[def.Name] = &Script{Name: def.Name, Source: def.Source}
	}

	for _, entry := range instEntries {
		var inst ScriptInstance
		if err := json.Unmarshal(entry.Data, &inst); err != nil {
			continue
		}
		s, ok := byName[inst.Name]
		if !ok {
			s = &Script{Name: inst.Name}
			byName[inst.Name] = s
		}
		s.Running = true
		s.Instances = append(s.Instances, inst)
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]Script, 0, len(names))
	for _, name := range names {
		s := byName[name]
		sort.Slice(s.Instances, func(i, j int) bool { return s.Instances[i].Hash < s.Instances[j].Hash })
		out = append(out, *s)
	}
	return out, nil
}

func loadScriptInstances(store storage.Storage) ([]ScriptInstance, error) {
	instEntries, err := store.Search("sb-script.instances.>")
	if err != nil {
		return nil, err
	}
	out := make([]ScriptInstance, 0, len(instEntries))
	for _, entry := range instEntries {
		var inst ScriptInstance
		if err := json.Unmarshal(entry.Data, &inst); err != nil {
			continue
		}
		out = append(out, inst)
	}
	return out, nil
}
