package webhook

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestDeliveryTracker_Metrics(t *testing.T) {
	var callCount int32

	handler := func(payload []byte) error {
		count := atomic.AddInt32(&callCount, 1)
		// First call fails
		if count == 1 {
			return fmt.Errorf("first delivery fails")
		}
		return nil
	}

	config := DefaultConfig()
	config.RetryDelay = 10 * time.Millisecond
	tracker := NewDeliveryTracker(config, handler)

	// Process 3 deliveries
	for i := 0; i < 3; i++ {
		deliveryID := fmt.Sprintf("metrics-delivery-%d", i)
		_, err := tracker.ProcessDelivery(deliveryID, []byte(`{}`), "")
		if err != nil {
			t.Logf("Delivery %d failed (expected for first): %v", i, err)
		}
	}

	// Wait for retry to complete
	time.Sleep(200 * time.Millisecond)

	// Process duplicate
	_, _ = tracker.ProcessDelivery("metrics-delivery-0", []byte(`{}`), "")

	metrics := tracker.GetMetrics()

	t.Logf("Metrics: Total=%d, Successful=%d, Failed=%d, Duplicates=%d",
		metrics.TotalDeliveries, metrics.SuccessfulDeliveries,
		metrics.FailedDeliveries, metrics.DuplicateAttempts)

	// TotalDeliveries counts unique delivery IDs created
	if metrics.TotalDeliveries != 3 {
		t.Errorf("Expected 3 total deliveries, got %d", metrics.TotalDeliveries)
	}

	// All 3 should eventually succeed (first via retry)
	if metrics.SuccessfulDeliveries != 3 {
		t.Errorf("Expected 3 successful deliveries, got %d", metrics.SuccessfulDeliveries)
	}

	// The first delivery had a failed attempt
	if metrics.FailedDeliveries != 1 {
		t.Errorf("Expected 1 failed delivery, got %d", metrics.FailedDeliveries)
	}

	// Duplicate delivery ID was processed again
	if metrics.DuplicateAttempts != 1 {
		t.Errorf("Expected 1 duplicate attempt, got %d", metrics.DuplicateAttempts)
	}
}
