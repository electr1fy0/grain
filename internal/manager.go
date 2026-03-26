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
			manager:    m,
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
func (m *Manager) Start(parent context.Context) {
	if m.ctx != nil {
		return
	}

	m.ctx, m.cancel = context.WithCancel(parent)

	for _, h := range m.hubs {
		m.wg.Add(2)
		go func(h *hub) {
			defer m.wg.Done()
			h.run()
		}(h)

		go func(h *hub) {
			defer m.wg.Done()
			h.listenToRedis()
		}(h)
	}
}

func (m *Manager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
}

func (m *Manager) Wait() {
	m.wg.Wait()
}
