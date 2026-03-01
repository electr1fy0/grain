package internal

import "time"

const (
	pingPeriod      = 15 * time.Second
	writeWait       = 5 * time.Second
	bufSize         = 1024
	globalChatTopic = "global_chat"
	chatHistoryKey  = "chat_history"
)
