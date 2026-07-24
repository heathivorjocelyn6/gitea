package webhook

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

// WebhookServer provides HTTP endpoints for webhook handling
type WebhookServer struct {
	tracker *DeliveryTracker
	config  WebhookConfig
}

// NewWebhookServer creates a new WebhookServer
func NewWebhookServer(config WebhookConfig, handler func(payload []byte) error) *WebhookServer {
	return &WebhookServer{
		tracker: NewDeliveryTracker(config, handler),
		config:  config,
	}
}

// HandleWebhook processes incoming webhook requests
func (ws *WebhookServer) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	// Read the request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()
	
	// Extract delivery ID from header (Gitea uses X-Gitea-Delivery)
	deliveryID := r.Header.Get("X-Gitea-Delivery")
	if deliveryID == "" {
		// Fallback to generating one from content and timestamp
		deliveryID = GenerateDeliveryID(body, time.Now())
	}
	
	// Get signature from header
	signature := r.Header.Get("X-Gitea-Signature")
	if signature == "" {
		signature = r.Header.Get("X-Hub-Signature-256")
	}
	
	// Process the delivery
	delivery, err := ws.tracker.ProcessDelivery(deliveryID, body, signature)
	if err != nil {
		// Check if this is a duplicate attempt
		if delivery != nil && delivery.Status == StatusDelivered {
			// Return 200 for idempotent duplicates
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `{"status":"already_processed","delivery_id":"%s"}`, deliveryID)
			return
		}
		
		http.Error(w, fmt.Sprintf("Webhook processing failed: %v", err), http.StatusInternalServerError)
		return
	}
	
	// Return success response
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"processed","delivery_id":"%s"}`, deliveryID)
}

// HandleHealth returns health check endpoint
// GetMetrics returns the current processing metrics
func (ws *WebhookServer) GetMetrics() Metrics {
	return ws.tracker.GetMetrics()
}

// Cleanup removes old completed deliveries
func (ws *WebhookServer) Cleanup(maxAge time.Duration) int {
	return ws.tracker.Cleanup(maxAge)
}

// HandleHealth returns health check endpoint
func (ws *WebhookServer) HandleHealth(w http.ResponseWriter, r *http.Request) {
	metrics := ws.tracker.GetMetrics()
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"healthy","deliveries":%d,"successful":%d,"failed":%d}`,
		metrics.TotalDeliveries, metrics.SuccessfulDeliveries, metrics.FailedDeliveries)
}

// HandleMetrics returns processing metrics
func (ws *WebhookServer) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	metrics := ws.tracker.GetMetrics()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{
		"total_deliveries": %d,
		"successful_deliveries": %d,
		"failed_deliveries": %d,
		"duplicate_attempts": %d,
		"average_process_time": "%v"
	}`,
		metrics.TotalDeliveries,
		metrics.SuccessfulDeliveries,
		metrics.FailedDeliveries,
		metrics.DuplicateAttempts,
		metrics.AverageProcessTime,
	)
}

// RegisterRoutes registers the webhook handlers on the given ServeMux
func (ws *WebhookServer) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/webhook", ws.HandleWebhook)
	mux.HandleFunc("/health", ws.HandleHealth)
	mux.HandleFunc("/metrics", ws.HandleMetrics)
}
