package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

const (
	pingPeriod = 15 * time.Second
	writeWait  = 5 * time.Second
	bufSize    = 256
)

type Hub struct {
	// holds all connected clients
	clients map[*Client]bool

	// raw messages
	broadcast chan []byte

	// a new client
	register chan *Client

	// client to kill
	unregister chan *Client
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

// runs for each client
func (h *Hub) Run(ctx context.Context) {
	for {
		select {

		case <-ctx.Done():
			return

		// new client, yay
		case client := <-h.register:
			h.clients[client] = true

		// client left, sad
		case client := <-h.unregister:
			h.removeClient(client)

		// client sent a msg
		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.send <- message:

				// screw slow clients who can't receive a msg
				default:
					h.removeClient(client)
				}
			}
		}
	}
}

func (h *Hub) removeClient(c *Client) {
	if ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
	}
}

type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
}

func (c *Client) readPump(ctx context.Context) {
	// unreg client from hub on err
	defer func() {
		c.hub.unregister <- c
		c.conn.Close(websocket.StatusAbnormalClosure, "read error")
	}()

	c.conn.SetReadLimit(64 * 2 << 10) // 64KB

	for {
		_, msg, err := c.conn.Read(ctx)
		if err != nil {
			return
		}
		c.hub.broadcast <- msg
	}
}

func (c *Client) writePump(ctx context.Context) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close(websocket.StatusNormalClosure, "done")
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			writeCtx, cancel := context.WithTimeout(ctx, writeWait)
			err := c.conn.Write(writeCtx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				return
			}
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, writeWait)
			if err := c.conn.Ping(pingCtx); err != nil {
				cancel()
				return
			}
			cancel()
		}
	}
}

func serveWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}

	client := &Client{
		hub:  hub,
		conn: conn,
		send: make(chan []byte, bufSize),
	}

	hub.register <- client

	go client.writePump(r.Context())
	go client.readPump(r.Context())
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := NewHub()
	go hub.Run(ctx)

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(hub, w, r)
	})

	log.Println("Server starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
