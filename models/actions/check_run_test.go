package actions

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func TestCreateCheckRunIdempotent_ConcurrentWebhooks(t *testing.T) {
	store := NewCheckRunStore()
	ctx := context.Background()
	var wg sync.WaitGroup

	repoID := int64(100)
	externalID := "delivery-12345"
	headSHA := "abc1234"

	createdCount := int32(0)
	var mu sync.Mutex

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, created, err := store.CreateCheckRunIdempotent(ctx, repoID, externalID, headSHA)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if created {
				mu.Lock()
				createdCount++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if createdCount != 1 {
		t.Fatalf("Expected exactly 1 check run created, got %d", createdCount)
	}
}
