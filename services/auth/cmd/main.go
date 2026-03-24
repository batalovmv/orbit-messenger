package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

// auth service — Authentication, JWT, 2FA, sessions, invites
// Port: 8081

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","service":"orbit-auth"}`))
	})

	log.Printf("orbit-auth starting on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Fprintf(os.Stderr, "failed to start: %v\n", err)
		os.Exit(1)
	}
}
