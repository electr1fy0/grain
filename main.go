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
	bufSize    = 1024
)

type Message struct {
	ID       string
	ServerID string          `json:"server_id"`
	Username string          `json:"username"`
	Time     string          `json:"time"`
	Payload  json.RawMessage `json:"payload"`
}

type Hub struct {
	id      string
	clients map[*Client]bool
	mu      sync.RWMutex
	rdb     *redis.Client
}

func NewHub(rdb *redis.Client) *Hub {
	return &Hub{
		id:      uuid.NewString(),
		clients: make(map[*Client]bool),
		rdb:     rdb,
	}
}

func (h *Hub) addClient(c *Client) {
	h.mu.Lock()
	h.clients[c] = true
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
			if c.id == envelope.ID {
				continue
			}
			select {

			case c.send <- []byte(msg.Payload):

			default:
				log.Printf("client behind, dropping conn")
				close(c.send)
			}
		}
		h.mu.RUnlock()
	}
}

type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	send     chan []byte
	username string
	id       string
}

func (c *Client) readPump(username string, ctx context.Context) {
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
			Username: username,
			ID:       c.id,
		}

		data, _ := json.Marshal(msg)

		pipe := c.hub.rdb.Pipeline()
		pipe.LPush(ctx, "chat_history", data)
		pipe.LTrim(ctx, "chat_history", 0, 99)
		if _, err := pipe.Exec(ctx); err != nil {
			log.Println("persistence err:", err)
		}

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

	username := r.URL.Query().Get("username")

	c := &Client{
		hub:      hub,
		conn:     conn,
		send:     make(chan []byte, bufSize),
		username: username,
		id:       uuid.NewString(),
	}

	ctx := r.Context()
	history, err := hub.rdb.LRange(ctx, "chat_history", 0, -1).Result()
	if err == nil {
		for i := len(history) - 1; i >= 0; i-- {
			var envelope Message
			if err := json.Unmarshal([]byte(history[i]), &envelope); err == nil {
				c.send <- envelope.Payload
			}
		}
	}

	hub.addClient(c)

	go c.readPump(username, context.Background())
	go c.writePump(context.Background())
}

func main() {
	ctx := context.Background()

	rdb := redis.NewClient(&redis.Options{
		Addr: "0.0.0.0:6379",
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
	log.Fatal(http.ListenAndServe("0.0.0.0:"+port, nil))
}
