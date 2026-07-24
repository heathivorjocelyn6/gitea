package main

import (
	"fmt"
	"log"
	"net/http"
	"time"
	
	"gitea-fork/webhook"
)

func main() {
	fmt.Println("Starting Gitea Webhook Server...")
	
	// Create webhook handler that processes deliveries
	webhookHandler := func(payload []byte) error {
		fmt.Printf("Processing webhook payload: %s\n", string(payload))
		// Add your actual webhook processing logic here
		// For example: trigger CI/CD, update database, send notifications, etc.
		time.Sleep(100 * time.Millisecond) // Simulate processing time
		return nil
	}
	
	// Configure webhook server
	config := webhook.DefaultConfig()
	config.Secret = "" // Set your webhook secret here for signature verification
	config.MaxRetries = 3
	config.RetryDelay = 5 * time.Second
	
	// Create webhook server
	server := webhook.NewWebhookServer(config, webhookHandler)
	
	// Register routes
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	
	// Start cleanup goroutine
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		
		for range ticker.C {
			removed := server.Cleanup(24 * time.Hour)
			if removed > 0 {
				fmt.Printf("Cleaned up %d old deliveries\n", removed)
			}
		}
	}()
	
	// Start server
	addr := ":8080"
	fmt.Printf("Server listening on %s\n", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
