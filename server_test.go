package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	
	"gitea-fork/webhook"
)

func TestWebhookServer_ConcurrentDeliveries(t *testing.T) {
	var processCount int32
	
	handler := func(payload []byte) error {
		processCount++
		return nil
	}
	
	config := webhook.DefaultConfig()
	server := webhook.NewWebhookServer(config, handler)
	
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	
	// Send the same webhook multiple times concurrently
	payload := []byte(`{"action":"push","ref":"refs/heads/main"}`)
	deliveryID := "test-delivery-123"
	
	var wg sync.WaitGroup
	const numRequests = 10
	
	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			
			req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(payload))
			req.Header.Set("X-Gitea-Delivery", deliveryID)
			req.Header.Set("Content-Type", "application/json")
			
			w := httptest.NewRecorder()
			server.HandleWebhook(w, req)
			
			if w.Code != http.StatusOK {
				t.Errorf("Expected 200 OK, got %d", w.Code)
			}
		}()
	}
	
	wg.Wait()
	
	// Verify only one actual processing occurred
	if processCount != 1 {
		t.Errorf("Expected 1 processing, got %d", processCount)
	}
	
	// Check metrics
	metrics := server.GetMetrics()
	if metrics.TotalDeliveries != 1 {
		t.Errorf("Expected 1 total delivery, got %d", metrics.TotalDeliveries)
	}
	if metrics.DuplicateAttempts != numRequests-1 {
		t.Errorf("Expected %d duplicate attempts, got %d", numRequests-1, metrics.DuplicateAttempts)
	}
}

func TestWebhookServer_DifferentDeliveries(t *testing.T) {
	var processCount int32
	
	handler := func(payload []byte) error {
		processCount++
		return nil
	}
	
	config := webhook.DefaultConfig()
	server := webhook.NewWebhookServer(config, handler)
	
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	
	// Send different webhooks
	const numDeliveries = 5
	
	var wg sync.WaitGroup
	for i := 0; i < numDeliveries; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			
			payload := []byte(fmt.Sprintf(`{"delivery":%d}`, index))
			deliveryID := fmt.Sprintf("delivery-%d", index)
			
			req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(payload))
			req.Header.Set("X-Gitea-Delivery", deliveryID)
			req.Header.Set("Content-Type", "application/json")
			
			w := httptest.NewRecorder()
			server.HandleWebhook(w, req)
			
			if w.Code != http.StatusOK {
				t.Errorf("Delivery %d: Expected 200 OK, got %d", index, w.Code)
			}
		}(i)
	}
	
	wg.Wait()
	
	// Verify all deliveries were processed
	if processCount != numDeliveries {
		t.Errorf("Expected %d processings, got %d", numDeliveries, processCount)
	}
}

func TestWebhookServer_HealthCheck(t *testing.T) {
	handler := func(payload []byte) error {
		return nil
	}
	
	config := webhook.DefaultConfig()
	server := webhook.NewWebhookServer(config, handler)
	
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	server.HandleHealth(w, req)
	
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 OK, got %d", w.Code)
	}
	
	body := w.Body.String()
	if body == "" {
		t.Error("Expected non-empty response body")
	}
}

func TestWebhookServer_MetricsEndpoint(t *testing.T) {
	handler := func(payload []byte) error {
		return nil
	}
	
	config := webhook.DefaultConfig()
	server := webhook.NewWebhookServer(config, handler)
	
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	
	// Process a few deliveries first
	for i := 0; i < 3; i++ {
		payload := []byte(fmt.Sprintf(`{"metrics":%d}`, i))
		deliveryID := fmt.Sprintf("metrics-%d", i)
		
		req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(payload))
		req.Header.Set("X-Gitea-Delivery", deliveryID)
		req.Header.Set("Content-Type", "application/json")
		
		w := httptest.NewRecorder()
		server.HandleWebhook(w, req)
	}
	
	// Check metrics endpoint
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	server.HandleMetrics(w, req)
	
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 OK, got %d", w.Code)
	}
	
	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected application/json, got %s", contentType)
	}
}
