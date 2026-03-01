package main

import (
	"context"
	"log"
	"net/http"
	"os"

	grain "github.com/electr1fy0/grain/internal"
	"github.com/redis/go-redis/v9"
)

func main() {
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatal("redis connection failed:", err)
	}

	manager := grain.NewManager(rdb)
	manager.Start(ctx)

	http.HandleFunc("/ws", manager.ServeWS)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Println("starting the server on:", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
