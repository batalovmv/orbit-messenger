package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

// integrations service — Webhooks, InsightFlow, Keitaro, HR-bot
// Port: 8087

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8087"
	}

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","service":"orbit-integrations"}`))
	})

	log.Printf("orbit-integrations starting on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Fprintf(os.Stderr, "failed to start: %v\n", err)
		os.Exit(1)
	}
}
