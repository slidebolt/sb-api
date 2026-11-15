package server

import (
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
			group, ok := auth.ResourceSecurityGroup(entry.Data)
			if ok && auth.AllowsGroup(r.Context(), group) {
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
				group, ok := auth.ResourceSecurityGroup(data)
				if ok && auth.AllowsGroup(r.Context(), group) {
					events <- wsEventsMessage{Type: "upsert", Key: key, Data: data}
				}
			},
			OnUpdate: func(key string, data json.RawMessage) {
				group, ok := auth.ResourceSecurityGroup(data)
				if ok && auth.AllowsGroup(r.Context(), group) {
					events <- wsEventsMessage{Type: "upsert", Key: key, Data: data}
				}
			},
			OnRemove: func(key string, data json.RawMessage) {
				group, ok := auth.ResourceSecurityGroup(data)
				if ok && auth.AllowsGroup(r.Context(), group) {
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

func writeWebSocketMessage(conn *websocket.Conn, msg wsEventsMessage) error {
	if err := conn.WriteJSON(msg); err != nil {
		log.Printf("sb-api websocket write failed: %v", err)
		return err
	}
	return nil
}
