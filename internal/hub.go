package internal

import (
	"context"
	"encoding/json"
	"log"
)

func (h *hub) removeClient(c *client) {
	if _, ok := h.clients[c]; !ok {
		return
	}
	delete(h.clients, c)
	close(c.send)
}

// run is the single owner of hub state.
// Listens to events from everywhere.
func (h *hub) run() {
	for {
		select {
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
func (h *hub) listenToRedis(ctx context.Context) {
	pubsub := h.rdb.Subscribe(ctx, globalChatTopic)
	defer pubsub.Close()

	log.Println("subscribed to redis:", h.id)

	for {
		msg, err := pubsub.ReceiveMessage(ctx)
		if err != nil {
			log.Println("redis receive error:", err)
			return
		}

		h.broadcast <- []byte(msg.Payload)
	}
}
