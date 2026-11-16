package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/slidebolt/sb-api/internal/auth"

	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"
)

type wsEventsMessage struct {
	Type    string          `json:"type"`
	Pattern string          `json:"pattern,omitempty"`
	Key     string          `json:"key,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Items   []storage.Entry `json:"items,omitempty"`
	Detail  string          `json:"detail,omitempty"`
}

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func webSocketEvents(store storage.Storage, msg messenger.Messenger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pattern := r.URL.Query().Get("pattern")
		if pattern == "" {
			pattern = ">"
		}

		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		entries, err := store.Search(pattern)
		if err != nil {
			writeWebSocketMessage(conn, wsEventsMessage{
				Type:   "error",
				Detail: "search failed: " + err.Error(),
			})
			return
		}
		visible := make([]storage.Entry, 0, len(entries))
		for _, entry := range entries {
			if data, ok := authorizedWebSocketData(r.Context(), entry.Data); ok {
				entry.Data = data
				visible = append(visible, entry)
			}
		}
		if err := writeWebSocketMessage(conn, wsEventsMessage{
			Type:    "snapshot",
			Pattern: pattern,
			Items:   visible,
		}); err != nil {
			return
		}

		events := make(chan wsEventsMessage, 32)
		watcher, err := storage.Watch(msg, storage.Query{Pattern: pattern}, storage.WatchHandlers{
			OnAdd: func(key string, data json.RawMessage) {
				if data, ok := authorizedWebSocketData(r.Context(), data); ok {
					events <- wsEventsMessage{Type: "upsert", Key: key, Data: data}
				}
			},
			OnUpdate: func(key string, data json.RawMessage) {
				if data, ok := authorizedWebSocketData(r.Context(), data); ok {
					events <- wsEventsMessage{Type: "upsert", Key: key, Data: data}
				}
			},
			OnRemove: func(key string, data json.RawMessage) {
				if data, ok := authorizedWebSocketData(r.Context(), data); ok {
					events <- wsEventsMessage{Type: "remove", Key: key, Data: data}
				}
			},
		})
		if err != nil {
			writeWebSocketMessage(conn, wsEventsMessage{
				Type:   "error",
				Detail: "watch failed: " + err.Error(),
			})
			return
		}
		defer watcher.Stop()

		for _, entry := range entries {
			watcher.Populate(entry.Key, entry.Data)
		}

		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()

		for {
			select {
			case <-done:
				return
			case event := <-events:
				if err := writeWebSocketMessage(conn, event); err != nil {
					return
				}
			}
		}
	}
}

func authorizedWebSocketData(ctx context.Context, data json.RawMessage) (json.RawMessage, bool) {
	group, ok := auth.ResourceSecurityGroup(data)
	if !ok || !auth.AllowsGroup(ctx, group) {
		return nil, false
	}
	var envelope map[string]any
	if json.Unmarshal(data, &envelope) != nil {
		return nil, false
	}
	if current, _ := envelope["securityGroup"].(string); current == "" {
		envelope["securityGroup"] = group
		data, _ = json.Marshal(envelope)
	}
	return data, true
}

func writeWebSocketMessage(conn *websocket.Conn, msg wsEventsMessage) error {
	if err := conn.WriteJSON(msg); err != nil {
		log.Printf("sb-api websocket write failed: %v", err)
		return err
	}
	return nil
}
