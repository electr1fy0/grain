package main

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type HubManager struct {
	Hubs []*Hub
}

func NewHubManager(rdb *redis.Client, numCpu int) *HubManager {
	h := &HubManager{}
	hubs := make([]*Hub, numCpu)

	for i := range numCpu {
		hubs[i] = &Hub{
			id:         uuid.NewString(),
			clients:    make(map[*Client]bool),
			register:   make(chan *Client),
			broadcast:  make(chan []byte),
			unregister: make(chan *Client),
			rdb:        rdb,
		}
	}
	h.Hubs = hubs
	return h
}

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
	id         string
	clients    map[*Client]bool
	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte
	rdb        *redis.Client
}

func (h *Hub) Run() {
	for {
		select {
		case c := <-h.register:
			h.clients[c] = true
		case c := <-h.unregister:
			delete(h.clients, c)
			close(c.send)
		case m := <-h.broadcast:
			var envelope Message
			if err := json.Unmarshal(m, &envelope); err != nil {
				continue
			}
			for c := range h.clients {
				if c.id == envelope.ID {
					continue
				}
				select {
				case c.send <- m:
				default:
					log.Printf("client behind, dropping conn")
					close(c.send)
					delete(h.clients, c)
				}
			}
		}
	}
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

		fmt.Println("msg.string():", msg.String())
		fmt.Println("msg.payload:", msg.Payload)

		h.broadcast <- []byte(msg.Payload)

	}
}

type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	send     chan []byte
	username string
	id       string
}

// Read Payload -> Marshal to Message
// -> Lpush History -> Trim History -> Publish
func (c *Client) readPump(username string, ctx context.Context) {
	defer func() {
		c.hub.unregister <- c
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

		data, err := json.Marshal(msg)
		if err != nil {
			log.Fatal("failed to marshal data:", err)
		}

		pipe := c.hub.rdb.Pipeline()
		pipe.LPush(ctx, "chat_history", data)
		pipe.LTrim(ctx, "chat_history", 0, 99)
		if _, err := pipe.Exec(ctx); err != nil {
			log.Println("persistence err:", err)
		}

		fmt.Println("gonna publish to redis:", string(data))
		if err := c.hub.rdb.Publish(ctx, "global_chat", data).Err(); err != nil {
			log.Println("redis publish error:", err)
		}
	}
}

// Receive message to client -> Write to the conn
// Also a ticker for ping periodic ping tests
func (c *Client) writePump(ctx context.Context) {
	tick := time.Tick(pingPeriod)
	defer func() {
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

		case <-tick:
			pingCtx, cancel := context.WithTimeout(ctx, writeWait)
			if err := c.conn.Ping(pingCtx); err != nil {
				cancel()
				return
			}
			cancel()
		}
	}
}

// Upgrade connection to WS -> Replay History -> Send register client msg to hub
// Initiates read and write loops too
func serveWs(hubManager *HubManager, w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}

	username := r.URL.Query().Get("username")

	hash := fnv.New32a()
	hash.Write([]byte(username))
	idx := int(hash.Sum32()) % len(hubManager.Hubs)
	hub := hubManager.Hubs[idx]

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

	hub.register <- c

	go c.readPump(username, context.Background())
	go c.writePump(context.Background())
}

func main() {
	ctx := context.Background()
	cpuCnt := runtime.NumCPU()
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatal("redis connection failed:", err)
	}

	hubManager := NewHubManager(rdb, cpuCnt)

	for _, hub := range hubManager.Hubs {
		go hub.Run()
		go hub.listenToRedis(ctx)
	}

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(hubManager, w, r)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Println("starting the server on:", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
