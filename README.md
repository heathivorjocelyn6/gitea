# Gitea Webhook Server

A Go implementation of idempotent webhook handling for Gitea, preventing duplicate check runs from concurrent webhook deliveries.

## Features

- **Idempotent Delivery Processing**: Prevents duplicate processing of the same webhook delivery
- **Concurrent Delivery Handling**: Safely processes multiple concurrent webhook deliveries
- **Signature Verification**: HMAC-SHA256 signature verification for webhook authenticity
- **Automatic Retries**: Configurable retry mechanism for failed deliveries
- **Metrics Tracking**: Monitor delivery success rates and duplicate attempt counts
- **Memory Management**: Automatic cleanup of old deliveries to prevent memory leaks

## Installation

```bash
go mod init gitea-fork
go mod tidy
```

## Usage

### Basic Server

```go
package main

import (
    "log"
    "net/http"
    "time"
    
    "gitea-fork/webhook"
)

func main() {
    // Create webhook handler
    handler := func(payload []byte) error {
        // Process your webhook payload here
        return nil
    }
    
    // Configure webhook server
    config := webhook.DefaultConfig()
    config.Secret = "your-webhook-secret" // For signature verification
    
    // Create and start server
    server := webhook.NewWebhookServer(config, handler)
    mux := http.NewServeMux()
    server.RegisterRoutes(mux)
    
    log.Fatal(http.ListenAndServe(":8080", mux))
}
```

### How It Works

1. **Delivery ID Extraction**: The server extracts the delivery ID from the `X-Gitea-Delivery` header
2. **Idempotency Check**: Before processing, it checks if this delivery ID has been seen before
3. **Concurrent Safety**: Uses sync primitives to safely handle concurrent access
4. **Signature Verification**: Validates webhook signatures using HMAC-SHA256 if configured
5. **Processing**: Executes the webhook handler function
6. **Status Tracking**: Updates delivery status and metrics

### API Endpoints

- `POST /webhook` - Process incoming webhooks
- `GET /health` - Health check endpoint
- `GET /metrics` - Processing metrics

## Configuration

```go
config := webhook.WebhookConfig{
    Secret:          "your-secret",        // For signature verification
    MaxRetries:      3,                     // Maximum retry attempts
    RetryDelay:      5 * time.Second,      // Delay between retries
    DeliveryTimeout: 30 * time.Second,     // Timeout per delivery
}
```

## Testing

```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./...

# Run specific test
go test -v -run TestDeliveryTracker_Idempotency ./webhook/
```

## Architecture

### Delivery Tracker

The core component that ensures idempotent delivery processing:

- Maintains a map of delivery IDs to their status
- Uses sync.RWMutex for concurrent access safety
- Handles retry logic for failed deliveries
- Tracks metrics for monitoring

### Webhook Server

HTTP server that wraps the delivery tracker:

- Extracts delivery IDs from Gitea headers
- Verifies webhook signatures
- Provides health and metrics endpoints
- Handles HTTP request/response cycle

## Benefits

1. **Prevents Duplicate Check Runs**: Idempotency ensures each webhook is processed exactly once
2. **Handles Concurrent Deliveries**: Safe for high-throughput webhook scenarios
3. **Automatic Recovery**: Retry mechanism handles temporary failures
4. **Observability**: Metrics provide insight into delivery success rates
5. **Memory Safe**: Automatic cleanup prevents memory leaks from accumulated deliveries

## License

MIT
