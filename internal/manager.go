package internal

import (
	"context"
	"runtime"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// NewManager builds one hub per CPU for WS fanout.
func NewManager(rdb *redis.Client) *Manager {
	numCPU := runtime.NumCPU()
	m := &Manager{}
	hubs := make([]*hub, numCPU)

	for i := range numCPU {
		hubs[i] = &hub{
			id:         uuid.NewString(),
			clients:    make(map[*client]bool),
			register:   make(chan *client, 128),
			unregister: make(chan *client, 128),
			broadcast:  make(chan []byte, 256),
			rdb:        rdb,
		}
	}

	m.hubs = hubs
	return m
}

// Start launches each hub loop and Redis subscriber worker.
func (m *Manager) Start(ctx context.Context) {
	for _, h := range m.hubs {
		go h.run()
		go h.listenToRedis(ctx)
	}
}
