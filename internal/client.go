package internal

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/coder/websocket"
)

func (c *client) close(code websocket.StatusCode, reason string) {
	c.closeOnce.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
		c.hub.unregister <- c
		_ = c.conn.Close(code, reason)
	})
}

// readPump receives websocket payloads from one client.
// Adds the message to history and publishes it to Redis.
func (c *client) readPump(ctx context.Context) {
	defer c.close(websocket.StatusNormalClosure, "read loop closed")

	for {
		_, payload, err := c.conn.Read(ctx)
		if err != nil {
			return
		}

		msg := Message{
			ServerID: c.hub.id,
			Payload:  payload,
			Username: c.username,
			ID:       c.id,
		}

		data, err := json.Marshal(msg)
		if err != nil {
			log.Println("marshal error:", err)
			continue
		}

		pipe := c.hub.rdb.Pipeline()
		pipe.LPush(ctx, chatHistoryKey, data)
		pipe.LTrim(ctx, chatHistoryKey, 0, 99)

		if _, err := pipe.Exec(ctx); err != nil {
			log.Println("persistence err:", err)
		}

		if err := c.hub.rdb.Publish(ctx, globalChatTopic, data).Err(); err != nil {
			log.Println("redis publish error:", err)
		}
	}
}

// writePump sends outbound messages to the clients ws conn.
// Tick is used for pings.
func (c *client) writePump(ctx context.Context) {
	tick := time.Tick(pingPeriod)
	defer c.close(websocket.StatusNormalClosure, "write loop closed")

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
