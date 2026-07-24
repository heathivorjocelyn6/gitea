package actions

import (
	"context"
	"errors"
	"sync"
)

var ErrDuplicateCheckRun = errors.New("actions: duplicate check run delivery")

type CheckRun struct {
	ID         int64  `xorm:"pk autoincr"`
	RepoID     int64  `xorm:"INDEX UNIQUE(repo_external)"`
	ExternalID string `xorm:"INDEX UNIQUE(repo_external)"`
	Status     string
	HeadSHA    string
}

type CheckRunStore struct {
	mu     sync.Mutex
	runs   map[string]*CheckRun
}

func NewCheckRunStore() *CheckRunStore {
	return &CheckRunStore{
		runs: make(map[string]*CheckRun),
	}
}

func (s *CheckRunStore) CreateCheckRunIdempotent(ctx context.Context, repoID int64, externalID, headSHA string) (*CheckRun, bool, error) {
	key := fmt.Sprintf("%d:%s", repoID, externalID)
	
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, exists := s.runs[key]; exists {
		return existing, false, nil
	}

	run := &CheckRun{
		ID:         int64(len(s.runs) + 1),
		RepoID:     repoID,
		ExternalID: externalID,
		Status:     "queued",
		HeadSHA:    headSHA,
	}
	s.runs[key] = run
	return run, true, nil
}
