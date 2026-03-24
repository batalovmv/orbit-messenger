package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

// gateway service — API gateway, WebSocket hub, reverse proxy
// Port: 8080

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","service":"orbit-gateway"}`))
	})

	log.Printf("orbit-gateway starting on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Fprintf(os.Stderr, "failed to start: %v\n", err)
		os.Exit(1)
	}
}
