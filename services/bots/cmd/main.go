package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

// bots service — Telegram-compatible Bot API, webhooks
// Port: 8086

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8086"
	}

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","service":"orbit-bots"}`))
	})

	log.Printf("orbit-bots starting on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Fprintf(os.Stderr, "failed to start: %v\n", err)
		os.Exit(1)
	}
}
