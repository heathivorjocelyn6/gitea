package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDeliveryTracker_Idempotency(t *testing.T) {
	var processCount int32

	handler := func(payload []byte) error {
		atomic.AddInt32(&processCount, 1)
		return nil
	}

	config := DefaultConfig()
	tracker := NewDeliveryTracker(config, handler)

	payload := []byte(`{"action":"push","ref":"refs/heads/main"}`)
	deliveryID := GenerateDeliveryID(payload, time.Now())

	// Process the same delivery multiple times concurrently
	var wg sync.WaitGroup
	results := make([]*Delivery, 5)
	errors := make([]error, 5)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results[index], errors[index] = tracker.ProcessDelivery(deliveryID, payload, "")
		}(i)
	}

	wg.Wait()

	// Verify that only one processing occurred
	actualCount := atomic.LoadInt32(&processCount)
	if actualCount != 1 {
		t.Errorf("Expected 1 processing, got %d", actualCount)
	}

	// Verify all results are successful and identical
	for i := 0; i < 5; i++ {
		if errors[i] != nil {
			t.Errorf("Delivery %d failed: %v", i, errors[i])
		}
		if results[i] == nil {
			t.Errorf("Delivery %d returned nil", i)
			continue
		}
		if results[i].Status != StatusDelivered {
			t.Errorf("Delivery %d has wrong status: %v", i, results[i].Status)
		}
		if results[i].ID != deliveryID {
			t.Errorf("Delivery %d has wrong ID: %s", i, results[i].ID)
		}
	}

	// Verify metrics
	metrics := tracker.GetMetrics()
	if metrics.DuplicateAttempts != 4 {
		t.Errorf("Expected 4 duplicate attempts, got %d", metrics.DuplicateAttempts)
	}
}

func TestDeliveryTracker_ConcurrentDifferentDeliveries(t *testing.T) {
	var processCount int32

	handler := func(payload []byte) error {
		atomic.AddInt32(&processCount, 1)
		time.Sleep(10 * time.Millisecond) // Simulate processing time
		return nil
	}

	config := DefaultConfig()
	tracker := NewDeliveryTracker(config, handler)

	// Process different deliveries concurrently
	var wg sync.WaitGroup
	const numDeliveries = 10
	results := make([]*Delivery, numDeliveries)
	errors := make([]error, numDeliveries)

	for i := 0; i < numDeliveries; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			payload := []byte(fmt.Sprintf(`{"delivery":%d}`, index))
			deliveryID := fmt.Sprintf("delivery-%d", index)
			results[index], errors[index] = tracker.ProcessDelivery(deliveryID, payload, "")
		}(i)
	}

	wg.Wait()

	// Verify all deliveries were processed
	actualCount := atomic.LoadInt32(&processCount)
	if actualCount != numDeliveries {
		t.Errorf("Expected %d processings, got %d", numDeliveries, actualCount)
	}

	// Verify all deliveries were successful
	for i := 0; i < numDeliveries; i++ {
		if errors[i] != nil {
			t.Errorf("Delivery %d failed: %v", i, errors[i])
		}
		if results[i] == nil {
			t.Errorf("Delivery %d returned nil", i)
			continue
		}
		if results[i].Status != StatusDelivered {
			t.Errorf("Delivery %d has wrong status: %v", i, results[i].Status)
		}
	}
}

func TestDeliveryTracker_RetryOnFailure(t *testing.T) {
	var processCount int32
	shouldFail := true

	handler := func(payload []byte) error {
		count := atomic.AddInt32(&processCount, 1)
		if shouldFail && count <= 2 {
			return fmt.Errorf("temporary failure")
		}
		return nil
	}

	config := DefaultConfig()
	config.RetryDelay = 50 * time.Millisecond // Faster retries for testing
	tracker := NewDeliveryTracker(config, handler)

	payload := []byte(`{"test":"retry"}`)
	deliveryID := "retry-delivery-1"

	// First attempt will fail
	_, err := tracker.ProcessDelivery(deliveryID, payload, "")
	if err == nil {
		t.Error("Expected error on first attempt")
	}

	// Wait for retries to complete (up to 3 retries with 50ms delay each)
	time.Sleep(500 * time.Millisecond)

	// Check that delivery eventually succeeded
	delivery, exists := tracker.GetDelivery(deliveryID)
	if !exists {
		t.Error("Delivery not found")
		return
	}

	if delivery.Status != StatusDelivered {
		t.Errorf("Expected delivered status, got %v (error: %s)", delivery.Status, delivery.LastError)
	}

	// Verify retry was attempted
	actualCount := atomic.LoadInt32(&processCount)
	if actualCount < 2 {
		t.Errorf("Expected at least 2 attempts, got %d", actualCount)
	}
}

func TestDeliveryTracker_RetryExhausted(t *testing.T) {
	var processCount int32

	handler := func(payload []byte) error {
		atomic.AddInt32(&processCount, 1)
		return fmt.Errorf("permanent failure")
	}

	config := DefaultConfig()
	config.RetryDelay = 50 * time.Millisecond
	config.MaxRetries = 3
	tracker := NewDeliveryTracker(config, handler)

	payload := []byte(`{"test":"retry-exhausted"}`)
	deliveryID := "exhaust-delivery-1"

	// First attempt
	_, err := tracker.ProcessDelivery(deliveryID, payload, "")
	if err == nil {
		t.Error("Expected error on first attempt")
	}

	// Wait for all retries
	time.Sleep(500 * time.Millisecond)

	// Check that delivery failed after all retries
	delivery, exists := tracker.GetDelivery(deliveryID)
	if !exists {
		t.Error("Delivery not found")
		return
	}

	if delivery.Status != StatusFailed {
		t.Errorf("Expected failed status, got %v", delivery.Status)
	}

	// Verify all attempts were made (1 initial + 3 retries = 4, but MaxRetries=3 means 3 total)
	actualCount := atomic.LoadInt32(&processCount)
	if actualCount != 3 {
		t.Errorf("Expected 3 attempts (MaxRetries), got %d", actualCount)
	}
}

func TestDeliveryTracker_SignatureVerification(t *testing.T) {
	secret := "test-secret-123"

	handler := func(payload []byte) error {
		return nil
	}

	config := DefaultConfig()
	config.Secret = secret
	tracker := NewDeliveryTracker(config, handler)

	payload := []byte(`{"test":"signature"}`)

	// Generate valid signature
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	validSignature := hex.EncodeToString(mac.Sum(nil))

	// Test with valid signature
	deliveryID1 := "sig-valid-1"
	_, err := tracker.ProcessDelivery(deliveryID1, payload, validSignature)
	if err != nil {
		t.Errorf("Valid signature should pass: %v", err)
	}

	// Test with invalid signature
	deliveryID2 := "sig-invalid-1"
	_, err = tracker.ProcessDelivery(deliveryID2, payload, "invalid-signature")
	if err == nil {
		t.Error("Invalid signature should fail")
	}

	// Test with sha256= prefix
	deliveryID3 := "sig-prefix-1"
	_, err = tracker.ProcessDelivery(deliveryID3, payload, "sha256="+validSignature)
	if err != nil {
		t.Errorf("Valid signature with prefix should pass: %v", err)
	}
}

func TestDeliveryTracker_Cleanup(t *testing.T) {
	handler := func(payload []byte) error {
		return nil
	}

	config := DefaultConfig()
	tracker := NewDeliveryTracker(config, handler)

	// Add some deliveries
	for i := 0; i < 5; i++ {
		deliveryID := fmt.Sprintf("cleanup-%d", i)
		tracker.ProcessDelivery(deliveryID, []byte(`{}`), "")
	}

	// Verify deliveries exist
	if count := tracker.GetDeliveryCount(); count != 5 {
		t.Errorf("Expected 5 deliveries, got %d", count)
	}

	// Cleanup with zero age (should remove all completed deliveries)
	removed := tracker.Cleanup(0)
	if removed != 5 {
		t.Errorf("Expected 5 removed, got %d", removed)
	}

	// Verify deliveries were removed
	if count := tracker.GetDeliveryCount(); count != 0 {
		t.Errorf("Expected 0 deliveries after cleanup, got %d", count)
	}
}



func TestGenerateDeliveryID_Consistency(t *testing.T) {
	payload := []byte(`{"consistent":"id"}`)
	timestamp := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	id1 := GenerateDeliveryID(payload, timestamp)
	id2 := GenerateDeliveryID(payload, timestamp)

	if id1 != id2 {
		t.Errorf("Same payload and timestamp should generate same ID: %s != %s", id1, id2)
	}

	// Different payload should generate different ID
	id3 := GenerateDeliveryID([]byte(`{"different":"payload"}`), timestamp)
	if id1 == id3 {
		t.Error("Different payloads should generate different IDs")
	}

	// Different timestamp should generate different ID
	id4 := GenerateDeliveryID(payload, time.Now())
	if id1 == id4 {
		t.Error("Different timestamps should generate different IDs")
	}
}

func TestDeliveryTracker_PendingStateWaiting(t *testing.T) {
	var processCount int32

	handler := func(payload []byte) error {
		count := atomic.AddInt32(&processCount, 1)
		if count == 1 {
			// First delivery takes a while
			time.Sleep(100 * time.Millisecond)
		}
		return nil
	}

	config := DefaultConfig()
	tracker := NewDeliveryTracker(config, handler)

	payload := []byte(`{"test":"pending-wait"}`)
	deliveryID := "pending-wait-1"

	// Launch two goroutines that try to process the same delivery
	var wg sync.WaitGroup
	results := make([]*Delivery, 2)
	errors := make([]error, 2)

	// First goroutine starts processing
	wg.Add(1)
	go func() {
		defer wg.Done()
		results[0], errors[0] = tracker.ProcessDelivery(deliveryID, payload, "")
	}()

	// Give first goroutine time to start
	time.Sleep(10 * time.Millisecond)

	// Second goroutine tries the same delivery
	wg.Add(1)
	go func() {
		defer wg.Done()
		results[1], errors[1] = tracker.ProcessDelivery(deliveryID, payload, "")
	}()

	wg.Wait()

	// Both should succeed
	for i := 0; i < 2; i++ {
		if errors[i] != nil {
			t.Errorf("Goroutine %d failed: %v", i, errors[i])
		}
		if results[i] == nil {
			t.Errorf("Goroutine %d returned nil", i)
			continue
		}
		if results[i].Status != StatusDelivered {
			t.Errorf("Goroutine %d has wrong status: %v", i, results[i].Status)
		}
	}

	// Only one actual processing should have occurred
	if atomic.LoadInt32(&processCount) != 1 {
		t.Errorf("Expected 1 processing, got %d", atomic.LoadInt32(&processCount))
	}
}
