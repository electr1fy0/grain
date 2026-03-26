package internal

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/coder/websocket"
	"github.com/redis/go-redis/v9"
)

type Manager struct {
	ctx    context.Context
	cancel context.CancelFunc
	hubs   []*hub
	wg     sync.WaitGroup
}

type Message struct {
	ID       string          `json:"id"`
	ServerID string          `json:"server_id"`
	Username string          `json:"username"`
	Payload  json.RawMessage `json:"payload"`
}

type hub struct {
	manager    *Manager
	id         string
	clients    map[*client]bool
	register   chan *client
	unregister chan *client
	broadcast  chan []byte
	rdb        *redis.Client
}

type client struct {
	hub       *hub
	conn      *websocket.Conn
	send      chan []byte
	username  string
	id        string
	cancel    context.CancelFunc
	closeOnce sync.Once
}
