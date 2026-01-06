package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type ChatRoom struct {
	Persons     []Person
	ChatHistory []Message
	mutex       sync.Mutex
}

type Message struct {
	ID         int64
	MsgAuthor  string
	ReplyToID  *int64
	MsgContent string
}

type Person struct {
	Conn     *websocket.Conn
	Username string
}

var chat = ChatRoom{}

// TODO
// 1. Every message should be broadcasted to all clients but itself
// 2. Only members can join
// 2. New joinees should be able to see chat history
// 3. Chat should persist and members have access

func (C *ChatRoom) BroadCastMessage(ctx context.Context, msg Message) {
	C.mutex.Lock()
	defer C.mutex.Unlock()

	for _, person := range C.Persons {
		if person.Username == msg.MsgAuthor {
			continue
		}
		wsjson.Write(ctx, person.Conn, msg)
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}

	username := r.Header.Get("X-Username")
	chat.Persons = append(chat.Persons, Person{conn, username})

	defer conn.Close(websocket.StatusInternalError, "connection closed")

	ctx := r.Context()

	for {
		var msg Message

		err := wsjson.Read(ctx, conn, &msg)
		if err != nil {
			fmt.Println(err)
			return
		}

		fmt.Println("msg:", msg)
		// chat.ChatHistory = append(chat.ChatHistory, Message{ID: rand.Int64(), MsgAuthor: username, MsgContent: string(msg), ReplyTo: nil})
		// fmt.Println("chat:", chat)

		// if err != nil {
		// 	if websocket.CloseStatus(err) == websocket.StatusNormalClosure ||
		// 		websocket.CloseStatus(err) == websocket.StatusGoingAway {
		// 		break
		// 	}
		// 	log.Println("Read error:", err)

		chat.BroadCastMessage(ctx, msg)
		// 	break
	}

	// err = conn.Write(ctx, typ, []byte("is this what you sent: "+string(msg)))

}

func main() {
	http.HandleFunc("/ws", handler)
	fmt.Println("Server starting on :8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
