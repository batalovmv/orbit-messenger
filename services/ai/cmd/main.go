package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

// ai service — Claude API, Whisper transcription, embeddings
// Port: 8085

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8085"
	}

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","service":"orbit-ai"}`))
	})

	log.Printf("orbit-ai starting on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Fprintf(os.Stderr, "failed to start: %v\n", err)
		os.Exit(1)
	}
}
