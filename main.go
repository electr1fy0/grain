package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	pingPeriod = 15 * time.Second
	writeWait  = 5 * time.Second
	bufSize    = 256
)

type Message struct {
	ServerID string          `json:"server_id"`
	Payload  json.RawMessage `json:"payload"`
}

type Hub struct {
	id      string
	clients map[*Client]struct{}
	mu      sync.RWMutex
	rdb     *redis.Client
}

func NewHub(rdb *redis.Client) *Hub {
	return &Hub{
		id:      uuid.NewString(),
		clients: make(map[*Client]struct{}),
		rdb:     rdb,
	}
}

func (h *Hub) addClient(c *Client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) removeClient(c *Client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	close(c.send)
}

func (h *Hub) listenToRedis(ctx context.Context) {
	pubsub := h.rdb.Subscribe(ctx, "global_chat")
	defer pubsub.Close()

	log.Println("subscribed to redis:", h.id)

	for {
		msg, err := pubsub.ReceiveMessage(ctx)
		if err != nil {
			log.Println("redis receive error:", err)
			return
		}

		var envelope Message
		if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
			continue
		}

		h.mu.RLock()
		for c := range h.clients {
			select {
			case c.send <- envelope.Payload:
			default:
				// slow client → drop message
			}
		}
		h.mu.RUnlock()
	}
}

type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
}

func (c *Client) readPump(ctx context.Context) {
	defer func() {
		c.hub.removeClient(c)
		c.conn.Close(websocket.StatusAbnormalClosure, "read error")
	}()

	for {
		_, payload, err := c.conn.Read(ctx)
		if err != nil {
			return
		}

		msg := Message{
			ServerID: c.hub.id,
			Payload:  payload,
		}

		data, _ := json.Marshal(msg)
		if err := c.hub.rdb.Publish(ctx, "global_chat", data).Err(); err != nil {
			log.Println("redis publish error:", err)
		}
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
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			writeCtx, cancel := context.WithTimeout(ctx, writeWait)
			if err := c.conn.Write(writeCtx, websocket.MessageText, msg); err != nil {
				cancel()
				return
			}
			cancel()

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
		return
	}

	c := &Client{
		hub:  hub,
		conn: conn,
		send: make(chan []byte, bufSize),
	}

	hub.addClient(c)

	ctx := context.Background()
	go c.readPump(ctx)
	go c.writePump(ctx)
}

func main() {
	ctx := context.Background()

	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatal("redis connection failed:", err)
	}

	hub := NewHub(rdb)
	go hub.listenToRedis(ctx)

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(hub, w, r)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Println("server listening on:", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
