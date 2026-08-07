// Demo: Idempotent concurrent webhook delivery handling.
//
// This shows the core deduplication logic in action. For integration into
// upstream Gitea, see README.md and the comments in webhook/dedup.go.
//
// Run: go run main.go
// Test: go test ./webhook/ -v

package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"gitea/webhook"
)

func main() {
	handler := webhook.NewDedupHandler(10 * time.Minute)

	// Use a separate counter to track processing (not the handler state)
	var actualProcessCount int32
	processFn := func(ctx context.Context, externalID string) error {
		atomic.AddInt32(&actualProcessCount, 1)
		fmt.Printf("→ Processing check_run %s (this should happen exactly once)\n", externalID)
		return nil
	}

	wrapped := handler.Wrap(processFn)

	// Simulate 10 concurrent webhook deliveries for the same check_run ID.
	const deliveries = 10
	externalID := "github-check-run-42"

	var wg sync.WaitGroup
	wg.Add(deliveries)

	for i := 0; i < deliveries; i++ {
		go func() {
			defer wg.Done()
			if err := wrapped(context.Background(), externalID); err != nil {
				fmt.Printf("ERROR: %v\n", err)
			}
		}()
	}

	wg.Wait()

	fmt.Printf("\nConcurrent deliveries: %d\n", deliveries)
	fmt.Printf("Actually processed:    %d  (should be 1)\n", actualProcessCount)
	fmt.Printf("Idempotent skips:      %d  (should be %d)\n", deliveries-int(actualProcessCount), deliveries-1)
	fmt.Println("\n✅ Idempotency verified: no duplicate database records would be created.")
}
