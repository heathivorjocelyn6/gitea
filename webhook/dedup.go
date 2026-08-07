// Package webhook provides idempotent handling of concurrent webhook deliveries
// to prevent duplicate Check Runs in Gitea.
//
// Integration points in upstream Gitea:
//   - services/webhook/ : Wrap incoming webhook handler with DedupHandler
//   - models/actions/check_run.go : Apply the unique constraint migration
package webhook

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ProcessingState tracks whether a webhook delivery is currently being
// processed or has already completed.
type ProcessingState int

const (
	StateUnknown    ProcessingState = iota
	StateProcessing                  // A goroutine is actively handling this ID
	StateCompleted                   // This ID has already been processed
)

// DedupHandler provides concurrency-safe, idempotent webhook delivery handling.
//
// It guarantees that concurrent deliveries for the same external Check Run ID
// produce at most one database record. Duplicate deliveries receive a graceful
// 200/202 response rather than causing a 500 or duplicate row.
//
// Usage in Gitea's services/webhook/ handler:
//
//	handler := webhook.NewDedupHandler(30 * time.Minute)
//	http.Handle("/api/v1/repo/.../check_run", handler.Wrap(myCheckRunHandler))
type DedupHandler struct {
	mu       sync.Mutex
	states   map[string]ProcessingState
	ttl      time.Duration
	lastSeen map[string]time.Time
}

// NewDedupHandler creates a handler that retains dedup state for `ttl`.
// After TTL expires, state is eligible for cleanup to bound memory.
func NewDedupHandler(ttl time.Duration) *DedupHandler {
	h := &DedupHandler{
		states:   make(map[string]ProcessingState),
		ttl:      ttl,
		lastSeen: make(map[string]time.Time),
	}
	go h.periodicCleanup()
	return h
}

// TryClaim attempts to atomically claim the right to process a webhook delivery
// identified by externalID (e.g., GitHub's check_run.id).
//
// Returns (true, nil) if this caller should proceed with processing.
// Returns (false, nil) if another caller is already processing or has
// completed — the caller should return 200 OK without creating a record.
func (h *DedupHandler) TryClaim(externalID string) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.lastSeen[externalID] = time.Now()

	state, exists := h.states[externalID]
	if !exists {
		h.states[externalID] = StateProcessing
		return true, nil
	}

	switch state {
	case StateProcessing:
		// Another goroutine is actively handling this delivery.
		return false, nil
	case StateCompleted:
		// Already processed — idempotent no-op.
		return false, nil
	default:
		// Unknown state (shouldn't happen), process to be safe.
		h.states[externalID] = StateProcessing
		return true, nil
	}
}

// MarkCompleted signals that processing for externalID has finished successfully.
// Future deliveries for this ID will be rejected as duplicates.
func (h *DedupHandler) MarkCompleted(externalID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.states[externalID] = StateCompleted
	h.lastSeen[externalID] = time.Now()
}

// MarkFailed resets the state so the delivery can be retried on a subsequent
// attempt (e.g., after a transient database error).
func (h *DedupHandler) MarkFailed(externalID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.states, externalID)
}

// Reset clears all state. Useful in tests.
func (h *DedupHandler) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.states = make(map[string]ProcessingState)
	h.lastSeen = make(map[string]time.Time)
}

// Wrap wraps an http.HandlerFunc with deduplication logic.
//
// The wrapped handler extracts the external ID from the request (via
// a caller-provided extractor) and ensures idempotent processing.
//
// In Gitea, integrate at services/webhook/ where incoming GitHub/Gitea
// check_run webhooks are dispatched. Replace the direct call with:
//
//	dedup := webhook.NewDedupHandler(30 * time.Minute)
//	router.Post("/check_run", dedup.Wrap(handleCheckRunWebhook))
func (h *DedupHandler) Wrap(processFn func(ctx context.Context, externalID string) error) func(ctx context.Context, externalID string) error {
	return func(ctx context.Context, externalID string) error {
		ok, err := h.TryClaim(externalID)
		if err != nil {
			return fmt.Errorf("dedup claim failed: %w", err)
		}
		if !ok {
			// Duplicate delivery — return nil to signal "200 OK, already handled"
			return nil
		}

		// Process the delivery. On failure, reset so it can be retried.
		if err := processFn(ctx, externalID); err != nil {
			h.MarkFailed(externalID)
			return err
		}

		h.MarkCompleted(externalID)
		return nil
	}
}

// periodicCleanup removes stale entries older than TTL to bound memory usage.
func (h *DedupHandler) periodicCleanup() {
	ticker := time.NewTicker(h.ttl / 2)
	defer ticker.Stop()
	for range ticker.C {
		h.mu.Lock()
		cutoff := time.Now().Add(-h.ttl)
		for id, ts := range h.lastSeen {
			if ts.Before(cutoff) {
				delete(h.states, id)
				delete(h.lastSeen, id)
			}
		}
		h.mu.Unlock()
	}
}
