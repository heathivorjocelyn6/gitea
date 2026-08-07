package webhook

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTryClaim_FirstCallSucceeds(t *testing.T) {
	h := NewDedupHandler(10 * time.Second)
	ok, err := h.TryClaim("check-run-42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("first call should succeed")
	}
}

func TestTryClaim_DuplicateRejected(t *testing.T) {
	h := NewDedupHandler(10 * time.Second)
	h.TryClaim("check-run-42")

	// Second call while still processing
	ok, err := h.TryClaim("check-run-42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("duplicate call should be rejected while processing")
	}
}

func TestTryClaim_AfterCompletion_Rejected(t *testing.T) {
	h := NewDedupHandler(10 * time.Second)
	h.TryClaim("check-run-42")
	h.MarkCompleted("check-run-42")

	ok, err := h.TryClaim("check-run-42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("should reject after completion")
	}
}

func TestTryClaim_AfterFail_RetryAllowed(t *testing.T) {
	h := NewDedupHandler(10 * time.Second)
	h.TryClaim("check-run-42")
	h.MarkFailed("check-run-42")

	ok, err := h.TryClaim("check-run-42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("should allow retry after failure")
	}
}

func TestTryClaim_DifferentIDs_Independent(t *testing.T) {
	h := NewDedupHandler(10 * time.Second)
	h.TryClaim("check-run-1")
	h.MarkCompleted("check-run-1")

	// Different ID should succeed
	ok, err := h.TryClaim("check-run-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("different IDs should be independent")
	}
}

// TestConcurrentDeliveries_NoDuplicates is the critical test:
// N goroutines try to process the same external ID simultaneously.
// Exactly one must succeed; the rest must be rejected.
func TestConcurrentDeliveries_NoDuplicates(t *testing.T) {
	h := NewDedupHandler(10 * time.Second)
	const numGoroutines = 50
	externalID := "check-run-concurrent"

	var successCount int32
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			ok, _ := h.TryClaim(externalID)
			if ok {
				atomic.AddInt32(&successCount, 1)
				// Simulate work
				time.Sleep(10 * time.Millisecond)
				h.MarkCompleted(externalID)
			}
		}()
	}

	wg.Wait()

	if successCount != 1 {
		t.Errorf("expected exactly 1 successful claim, got %d", successCount)
	}
}

// TestConcurrentDifferentIDs_AllSucceed verifies that different IDs
// don't block each other.
func TestConcurrentDifferentIDs_AllSucceed(t *testing.T) {
	h := NewDedupHandler(10 * time.Second)
	const numIDs = 20

	var successCount int32
	var wg sync.WaitGroup
	wg.Add(numIDs)

	for i := 0; i < numIDs; i++ {
		id := fmt.Sprintf("check-run-%d", i)
		go func(extID string) {
			defer wg.Done()
			ok, _ := h.TryClaim(extID)
			if ok {
				atomic.AddInt32(&successCount, 1)
				h.MarkCompleted(extID)
			}
		}(id)
	}

	wg.Wait()

	if successCount != numIDs {
		t.Errorf("expected %d successes, got %d", numIDs, successCount)
	}
}

// TestWrap_EndToEnd tests the full Wrap flow.
func TestWrap_EndToEnd(t *testing.T) {
	h := NewDedupHandler(10 * time.Second)

	var processCount int32
	processFn := func(ctx context.Context, externalID string) error {
		atomic.AddInt32(&processCount, 1)
		return nil
	}

	wrapped := h.Wrap(processFn)

	// First call processes
	err := wrapped(context.Background(), "check-run-e2e")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Second call is duplicate — should not process
	err = wrapped(context.Background(), "check-run-e2e")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if processCount != 1 {
		t.Errorf("expected exactly 1 process invocation, got %d", processCount)
	}
}

// TestWrap_ErrorResetsState verifies that a failed process resets state
// so a retry can succeed.
func TestWrap_ErrorResetsState(t *testing.T) {
	h := NewDedupHandler(10 * time.Second)

	var attempts int32
	processFn := func(ctx context.Context, externalID string) error {
		count := atomic.AddInt32(&attempts, 1)
		if count == 1 {
			return fmt.Errorf("transient db error")
		}
		return nil
	}

	wrapped := h.Wrap(processFn)

	// First attempt fails
	err := wrapped(context.Background(), "check-run-retry")
	if err == nil {
		t.Fatal("expected error on first attempt")
	}

	// Second attempt should succeed (state was reset)
	err = wrapped(context.Background(), "check-run-retry")
	if err != nil {
		t.Fatalf("second attempt should succeed: %v", err)
	}

	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}
