package actions

import (
	"errors"
	"sync"
)

type CheckRun struct {
	ID         int64
	ExternalID string
	RepoID     int64
}

// Mock database to simulate unique constraint
type MockDB struct {
	mu        sync.Mutex
	checkRuns map[string]*CheckRun
}

func NewMockDB() *MockDB {
	return &MockDB{
		checkRuns: make(map[string]*CheckRun),
	}
}

var ErrUniqueConstraint = errors.New("UNIQUE constraint failed: external_id")

// Insert simulates inserting a CheckRun with a unique constraint on ExternalID
func (db *MockDB) Insert(cr *CheckRun) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	// 1. Idempotency Guarantee: Database constraint enforces uniqueness
	if _, exists := db.checkRuns[cr.ExternalID]; exists {
		return ErrUniqueConstraint
	}

	db.checkRuns[cr.ExternalID] = cr
	return nil
}

// HandleWebhook delivery idempotently
func HandleWebhook(db *MockDB, externalID string, repoID int64) (string, error) {
	cr := &CheckRun{
		ExternalID: externalID,
		RepoID:     repoID,
	}

	err := db.Insert(cr)
	if err != nil {
		if errors.Is(err, ErrUniqueConstraint) {
			// 3. Graceful Handling: Ignore duplicate and return 202 Accepted equivalent message
			return "Already processing", nil
		}
		return "", err
	}

	return "Created", nil
}
