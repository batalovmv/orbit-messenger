package main

import "fmt"

// This is a stub entry point for Saturn/Coolify build compatibility.
// Saturn generates a wrapper Dockerfile that runs: go build -o /server ./cmd/main.go
// The actual service entry points are in services/*/cmd/main.go.
// Each service has its own Dockerfile that builds the correct binary.
func main() {
	fmt.Println("ERROR: This is the root stub. You should be running a specific service binary.")
	fmt.Println("Available services: gateway, auth, messaging, media, calls, bots, integrations")
}
