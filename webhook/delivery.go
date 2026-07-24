package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// DeliveryStatus represents the state of a webhook delivery
type DeliveryStatus int

const (
	// StatusPending indicates the delivery is being processed
	StatusPending DeliveryStatus = iota
	// StatusDelivered indicates the delivery was successfully processed
	StatusDelivered
	// StatusFailed indicates the delivery processing failed
	StatusFailed
)

// String returns the string representation of DeliveryStatus
func (s DeliveryStatus) String() string {
	switch s {
	case StatusPending:
		return "pending"
	case StatusDelivered:
		return "delivered"
	case StatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// Delivery represents a single webhook delivery attempt
type Delivery struct {
	ID            string
	Payload       []byte
	Signature     string
	Timestamp     time.Time
	Status        DeliveryStatus
	AttemptCount  int
	MaxRetries    int
	LastError     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	ProcessedAt   *time.Time
}

// WebhookConfig holds configuration for webhook processing
type WebhookConfig struct {
	// Secret is used to verify webhook signatures
	Secret string
	// MaxRetries is the maximum number of delivery attempts
	MaxRetries int
	// RetryDelay is the delay between retry attempts
	RetryDelay time.Duration
	// DeliveryTimeout is the timeout for individual deliveries
	DeliveryTimeout time.Duration
}

// DefaultConfig returns a default webhook configuration
func DefaultConfig() WebhookConfig {
	return WebhookConfig{
		MaxRetries:      3,
		RetryDelay:      time.Second * 5,
		DeliveryTimeout: time.Second * 30,
	}
}

// DeliveryTracker tracks webhook deliveries and prevents duplicate processing
type DeliveryTracker struct {
	mu          sync.RWMutex
	deliveries  map[string]*Delivery
	config      WebhookConfig
	handler     func(payload []byte) error
	metrics     Metrics
}

// Metrics holds webhook processing metrics
type Metrics struct {
	TotalDeliveries    int64
 SuccessfulDeliveries int64
	FailedDeliveries   int64
	DuplicateAttempts  int64
	AverageProcessTime time.Duration
	mu                 sync.RWMutex
}

// GetMetrics returns a copy of the current metrics
func (m *Metrics) GetMetrics() Metrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return Metrics{
		TotalDeliveries:     m.TotalDeliveries,
		SuccessfulDeliveries: m.SuccessfulDeliveries,
		FailedDeliveries:    m.FailedDeliveries,
		DuplicateAttempts:   m.DuplicateAttempts,
		AverageProcessTime:  m.AverageProcessTime,
	}
}

// NewDeliveryTracker creates a new DeliveryTracker with the given configuration
func NewDeliveryTracker(config WebhookConfig, handler func(payload []byte) error) *DeliveryTracker {
	return &DeliveryTracker{
		deliveries: make(map[string]*Delivery),
		config:     config,
		handler:    handler,
	}
}

// GenerateDeliveryID creates a unique delivery ID based on payload and timestamp
func GenerateDeliveryID(payload []byte, timestamp time.Time) string {
	h := sha256.New()
	h.Write(payload)
	h.Write([]byte(timestamp.Format(time.RFC3339Nano)))
	return hex.EncodeToString(h.Sum(nil))[:16] // Use first 16 chars for brevity
}

// VerifySignature verifies the webhook signature using HMAC-SHA256
func VerifySignature(payload []byte, signature string, secret string) bool {
	if secret == "" {
		return true // No secret configured, skip verification
	}
	
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expectedMAC := hex.EncodeToString(mac.Sum(nil))
	
	// Support both "sha256=..." and raw hex formats
	if len(signature) > 7 && signature[:7] == "sha256=" {
		signature = signature[7:]
	}
	
	return hmac.Equal([]byte(expectedMAC), []byte(signature))
}

// ProcessDelivery processes a webhook delivery with idempotency guarantees
func (dt *DeliveryTracker) ProcessDelivery(deliveryID string, payload []byte, signature string) (*Delivery, error) {
	startTime := time.Now()
	
	// Verify signature if configured
	if dt.config.Secret != "" && !VerifySignature(payload, signature, dt.config.Secret) {
		return nil, fmt.Errorf("invalid webhook signature")
	}
	
	// Check if delivery already exists (idempotency check)
	dt.mu.Lock()
	existing, exists := dt.deliveries[deliveryID]
	if exists {
		// If already delivered, return the existing delivery (idempotent behavior)
		if existing.Status == StatusDelivered {
			dt.mu.Unlock()
			dt.metrics.mu.Lock()
			dt.metrics.DuplicateAttempts++
			dt.metrics.mu.Unlock()
			return existing, nil
		}
		
		// If pending and not too old, wait for it to complete
		if existing.Status == StatusPending && time.Since(existing.CreatedAt) < dt.config.DeliveryTimeout {
			dt.mu.Unlock()
			return dt.waitForDelivery(deliveryID)
		}
		
		// If failed or timed out, allow retry
		existing.AttemptCount++
		existing.Status = StatusPending
		existing.UpdatedAt = time.Now()
		existing.LastError = ""
		dt.mu.Unlock()
	} else {
		// Create new delivery
		delivery := &Delivery{
			ID:           deliveryID,
			Payload:      payload,
			Signature:    signature,
			Timestamp:    time.Now(),
			Status:       StatusPending,
			AttemptCount: 1,
			MaxRetries:   dt.config.MaxRetries,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}
		dt.deliveries[deliveryID] = delivery
		dt.mu.Unlock()
		
		dt.metrics.mu.Lock()
		dt.metrics.TotalDeliveries++
		dt.metrics.mu.Unlock()
	}
	
	// Process the delivery
	err := dt.processWebhook(payload)
	processTime := time.Since(startTime)
	
	// Update delivery status
	dt.mu.Lock()
	delivery := dt.deliveries[deliveryID]
	if err != nil {
		delivery.Status = StatusFailed
		delivery.LastError = err.Error()
		delivery.UpdatedAt = time.Now()
		
		dt.mu.Unlock()
		
		dt.metrics.mu.Lock()
		dt.metrics.FailedDeliveries++
		dt.metrics.mu.Unlock()
		
		// Schedule retry if we haven't exceeded max attempts
		if delivery.AttemptCount < delivery.MaxRetries {
			go dt.retryDelivery(deliveryID)
		}
		
		return delivery, fmt.Errorf("delivery failed: %w", err)
	}
	
	now := time.Now()
	delivery.Status = StatusDelivered
	delivery.ProcessedAt = &now
	delivery.UpdatedAt = now
	dt.mu.Unlock()
	
	dt.metrics.mu.Lock()
	dt.metrics.SuccessfulDeliveries++
	// Update average process time using exponential moving average
	if dt.metrics.TotalDeliveries == 1 {
		dt.metrics.AverageProcessTime = processTime
	} else {
		dt.metrics.AverageProcessTime = (dt.metrics.AverageProcessTime + processTime) / 2
	}
	dt.metrics.mu.Unlock()
	
	return delivery, nil
}

// processWebhook calls the configured handler
func (dt *DeliveryTracker) processWebhook(payload []byte) error {
	if dt.handler == nil {
		return fmt.Errorf("no webhook handler configured")
	}
	return dt.handler(payload)
}

// retryDelivery handles retrying a failed delivery
func (dt *DeliveryTracker) retryDelivery(deliveryID string) {
	time.Sleep(dt.config.RetryDelay)

	dt.mu.Lock()
	delivery, exists := dt.deliveries[deliveryID]
	if !exists {
		dt.mu.Unlock()
		return
	}
	
	// Check if we should still retry (not already being processed or succeeded)
	if delivery.Status == StatusDelivered {
		dt.mu.Unlock()
		return
	}
	
	payload := delivery.Payload
	signature := delivery.Signature
	dt.mu.Unlock()
	
	// Process the delivery again
	_, err := dt.ProcessDelivery(deliveryID, payload, signature)
	if err != nil {
		// Log error but don't propagate - retries are best effort
		fmt.Printf("Retry failed for delivery %s: %v\n", deliveryID, err)
	}
}

// waitForDelivery waits for an in-progress delivery to complete
func (dt *DeliveryTracker) waitForDelivery(deliveryID string) (*Delivery, error) {
	timeout := time.After(dt.config.DeliveryTimeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	
	for {
		select {
		case <-timeout:
			return nil, fmt.Errorf("timeout waiting for delivery %s", deliveryID)
		case <-ticker.C:
			dt.mu.RLock()
			delivery, exists := dt.deliveries[deliveryID]
			if !exists {
				dt.mu.RUnlock()
				return nil, fmt.Errorf("delivery %s not found", deliveryID)
			}
			
			if delivery.Status != StatusPending {
				result := *delivery
				dt.mu.RUnlock()
				return &result, nil
			}
			dt.mu.RUnlock()
		}
	}
}

// GetDelivery retrieves a delivery by ID
func (dt *DeliveryTracker) GetDelivery(deliveryID string) (*Delivery, bool) {
	dt.mu.RLock()
	defer dt.mu.RUnlock()
	delivery, exists := dt.deliveries[deliveryID]
	if !exists {
		return nil, false
	}
	result := *delivery
	return &result, true
}

// GetMetrics returns the current processing metrics
func (dt *DeliveryTracker) GetMetrics() Metrics {
	return dt.metrics.GetMetrics()
}

// Cleanup removes old completed deliveries to prevent memory leaks
func (dt *DeliveryTracker) Cleanup(maxAge time.Duration) int {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	
	cutoff := time.Now().Add(-maxAge)
	removed := 0
	
	for id, delivery := range dt.deliveries {
		if delivery.Status != StatusPending && delivery.UpdatedAt.Before(cutoff) {
			delete(dt.deliveries, id)
			removed++
		}
	}
	
	return removed
}

// GetDeliveryCount returns the number of tracked deliveries
func (dt *DeliveryTracker) GetDeliveryCount() int {
	dt.mu.RLock()
	defer dt.mu.RUnlock()
	return len(dt.deliveries)
}
