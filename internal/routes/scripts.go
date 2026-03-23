package routes

import (
	"context"
	"encoding/json"
	"fmt"
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

type ScriptStartStopInput struct {
	Name string `path:"name"`
	Body struct {
		QueryRef string `json:"queryRef,omitempty"`
	}
}

type ScriptStartOutput struct {
	Body struct {
		Hash string `json:"hash"`
	}
}

func RegisterScripts(api huma.API, store storage.Storage, msg messenger.Messenger) {
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

	huma.Register(api, huma.Operation{
		Method:      "POST",
		Path:        "/scripts/{name}/start",
		Summary:     "Start a script instance",
		Description: "Starts a named script in sb-script via NATS request/reply.",
		Tags:        []string{"scripts"},
	}, func(ctx context.Context, input *ScriptStartStopInput) (*ScriptStartOutput, error) {
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

	huma.Register(api, huma.Operation{
		Method:      "DELETE",
		Path:        "/scripts/{name}/start",
		Summary:     "Stop a script instance",
		Description: "Stops a named script in sb-script via NATS request/reply.",
		Tags:        []string{"scripts"},
	}, func(ctx context.Context, input *ScriptStartStopInput) (*struct{}, error) {
		req := map[string]any{
			"name":     input.Name,
			"queryRef": input.Body.QueryRef,
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
