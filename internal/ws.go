package internal

import (
	"context"
	"hash/fnv"
	"net/http"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

// ServeWS upgrades to WS and binds each client to one hub.
// Also starts RW pumps per client.
func (m *Manager) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}

	username := r.URL.Query().Get("username")
	if username == "" {
		_ = conn.Close(websocket.StatusPolicyViolation, "username is required")
		return
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte(username))
	idx := int(h.Sum32()) % len(m.hubs)
	hub := m.hubs[idx]

	connCtx, cancel := context.WithCancel(context.Background())
	c := &client{
		hub:      hub,
		conn:     conn,
		send:     make(chan []byte, bufSize),
		username: username,
		id:       uuid.NewString(),
		cancel:   cancel,
	}

	hub.register <- c

	go c.readPump(connCtx)
	go c.writePump(connCtx)
}
