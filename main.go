package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
)

// WebhookHandler processes webhooks idempotently.
// The bug: concurrent webhook deliveries cause duplicate check runs.
// Fix: Use a dedup cache keyed by event ID + delivery ID.
type WebhookHandler struct {
	mu    sync.Mutex
	seen  map[string]bool
}

func NewWebhookHandler() *WebhookHandler {
	return &WebhookHandler{seen: make(map[string]bool)}
}

func (h *WebhookHandler) Handle(eventID, deliveryID string) bool {
	key := eventID + ":" + deliveryID
	hash := sha256.Sum256([]byte(key))
	hashStr := hex.EncodeToString(hash[:16])

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.seen[hashStr] {
		return false // duplicate, already processed
	}
	h.seen[hashStr] = true
	return true
}

func main() {
	h := NewWebhookHandler()
	fmt.Println("Idempotent webhook handler: dedup by event+delivery ID")
}
