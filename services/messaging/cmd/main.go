package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

// messaging service — Messages, chats, groups, channels
// Port: 8082

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8082"
	}

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","service":"orbit-messaging"}`))
	})

	log.Printf("orbit-messaging starting on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Fprintf(os.Stderr, "failed to start: %v\n", err)
		os.Exit(1)
	}
}
