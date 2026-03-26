package internal

import (
	"encoding/json"
	"log"
)

func (h *hub) done() <-chan struct{} {
	return h.manager.ctx.Done()
}

func (h *hub) removeClient(c *client) {
	if _, ok := h.clients[c]; !ok {
		return
	}
	delete(h.clients, c)
	close(c.send)
}

func (h *hub) registerClient(c *client) bool {
	select {
	case h.register <- c:
		return true
	case <-h.done():
		return false
	}
}

func (h *hub) unregisterClient(c *client) {
	select {
	case h.unregister <- c:
	case <-h.done():
	}
}

// run is the single owner of hub state.
// Listens to events from everywhere.
func (h *hub) run() {
	for {
		select {
		case <-h.done():
			for c := range h.clients {
				h.removeClient(c)
			}
			return
		case c := <-h.register:
			h.clients[c] = true
		case c := <-h.unregister:
			h.removeClient(c)
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
					h.removeClient(c)
				}
			}
		}
	}
}

// listenToRedis reads global pubsub payloads and forwards them.
func (h *hub) listenToRedis() {
	pubsub := h.rdb.Subscribe(h.manager.ctx, globalChatTopic)
	defer func() {
		_ = pubsub.Close()
	}()

	go func() {
		<-h.done()
		_ = pubsub.Close()
	}()

	log.Println("subscribed to redis:", h.id)

	for {
		msg, err := pubsub.ReceiveMessage(h.manager.ctx)
		if err != nil {
			if h.manager.ctx.Err() == nil {
				log.Println("redis receive error:", err)
			}
			return
		}

		select {
		case h.broadcast <- []byte(msg.Payload):
		case <-h.done():
			return
		}
	}
}
