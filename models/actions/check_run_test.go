package actions

import (
	"sync"
	"testing"
)

func TestHandleWebhook_ConcurrentIdempotency(t *testing.T) {
	db := NewMockDB()
	var wg sync.WaitGroup
	results := make(chan string, 100)

	externalID := "ext-12345"
	repoID := int64(1)

	// Spawning 100 concurrent goroutines
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := HandleWebhook(db, externalID, repoID)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			results <- res
		}()
	}

	wg.Wait()
	close(results)

	createdCount := 0
	ignoredCount := 0

	for res := range results {
		if res == "Created" {
			createdCount++
		} else if res == "Already processing" {
			ignoredCount++
		}
	}

	if createdCount != 1 {
		t.Errorf("expected exactly 1 'Created' response, got %d", createdCount)
	}

	if ignoredCount != 99 {
		t.Errorf("expected exactly 99 'Already processing' responses, got %d", ignoredCount)
	}

	if len(db.checkRuns) != 1 {
		t.Errorf("expected exactly 1 database record, got %d", len(db.checkRuns))
	}
}
