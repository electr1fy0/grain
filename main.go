package main

import (
	"context"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

const (
	pingWait   = time.Second * 10
	pongWait   = time.Second * 10
	pingPeriod = pongWait * 9 / 10
	bufSize    = 256
)

type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
}

func NewHub() *Hub {
	return &Hub{
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
	}
}

func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case client := <-h.register:
			h.clients[client] = true
		case client := <-h.unregister:
			if h.clients[client] {
				delete(h.clients, client)
				close(client.send)
			}
		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					h.removeClient(client)
				}
			}
		}
	}
}

func (h *Hub) removeClient(c *Client) {
	delete(h.clients, c)
	close(c.send)
}

type Client struct {
	conn *websocket.Conn
	send chan []byte
	hub  *Hub
}

func (c *Client) readPump(ctx context.Context) {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close(websocket.StatusAbnormalClosure, "bye")
	}()

	c.conn.SetReadLimit(512)
	for {
		_, msg, err := c.conn.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				return
			}
			c.hub.broadcast <- msg
		}
	}
}

func (c *Client) writePump(ctx context.Context) {
	ticker := time.NewTicker(pingWait)
	defer ticker.Stop()
	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				return
			}

			err := c.conn.Write(ctx, websocket.MessageText, msg)
			if err != nil {
				return
			}
		case <-ticker.C:
			if err := c.conn.Ping(ctx); err != nil {
				return
			}
		}
	}
}

func serveHTTP(w http.ResponseWriter, r *http.Request) {
	conn, _ := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	send := make(chan byte, bufSize)
	client := &Client{conn, send}
}
