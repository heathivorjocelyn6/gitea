# Gitea Webhook Idempotency — Prevent Duplicate Check Runs

Standalone implementation of concurrency-safe, idempotent webhook delivery handling for [Gitea](https://github.com/go-gitea/gitea).

Fixes [#1](https://github.com/heathivorjocelyn6/gitea/issues/1): duplicate Check Runs from concurrent webhook deliveries.

## Problem

When Gitea receives concurrent webhook deliveries for the same Check Run (e.g., GitHub retries or network duplicates), multiple goroutines process them simultaneously. Without idempotency control, this creates duplicate database records.

## Solution

`webhook.DedupHandler` — a concurrency-safe handler that guarantees at-most-once processing per external ID:

- **Per-ID mutex locking**: Only one goroutine processes a given external ID at a time.
- **Completion tracking**: Once processed, future deliveries for the same ID are rejected gracefully.
- **Failure recovery**: If processing fails (transient DB error), state resets for retry.
- **Memory-bounded**: Periodic cleanup of stale entries via configurable TTL.
- **Database constraint**: Migration SQL for a unique index on `(repo_id, external_id)` as defense-in-depth.

## Files

| File | Purpose |
|------|---------|
| `webhook/dedup.go` | Core deduplication handler |
| `webhook/dedup_test.go` | Concurrent safety tests (50 goroutines, same ID → 1 record) |
| `migrations/001_add_check_run_unique_constraint.sql` | DB migration for all backends |
| `main.go` | Standalone demo (10 concurrent deliveries → 1 processed) |

## Quick Demo

```bash
go run main.go
# Concurrent deliveries: 10
# Actually processed:    1   (should be 1)
# Idempotent skips:      9   (should be 9)
# ✅ Idempotency verified
```

## Tests

```bash
go test ./webhook/ -v -race
# TestTryClaim_FirstCallSucceeds          PASS
# TestTryClaim_DuplicateRejected          PASS
# TestConcurrentDeliveries_NoDuplicates   PASS  (50 goroutines, 1 winner)
# TestConcurrentDifferentIDs_AllSucceed   PASS
# TestWrap_EndToEnd                       PASS
# TestWrap_ErrorResetsState               PASS
```

## Upstream Gitea Integration

### 1. Webhook Handler (`services/webhook/`)

Wrap the existing check_run webhook handler:

```go
import "code.gitea.io/gitea/modules/webhook"

var dedupHandler = webhook.NewDedupHandler(30 * time.Minute)

func handleCheckRunWebhook(ctx context.Context, payload []byte) error {
    externalID := extractCheckRunID(payload) // GitHub's check_run.id

    return dedupHandler.Wrap(func(ctx context.Context, id string) error {
        // Existing insertion logic from models/actions/check_run.go
        return models.InsertCheckRun(ctx, payload)
    })(ctx, externalID)
}
```

### 2. Database Migration (`models/migrations/`)

Apply the unique constraint from `migrations/001_add_check_run_unique_constraint.sql`:

```go
// In models/actions/check_run.go, add XORM unique index:
type CheckRun struct {
    ID         int64  `xorm:"pk autoincr"`
    RepoID     int64  `xorm:"UNIQUE(repo_external)"`
    ExternalID string `xorm:"UNIQUE(repo_external)"`
    // ... other fields
}
```

### 3. Graceful Duplicate Handling

When the unique constraint catches a late duplicate (defense-in-depth):

```go
// In models/actions/check_run.go InsertCheckRun:
func InsertCheckRun(ctx context.Context, cr *CheckRun) error {
    _, err := x.Insert(cr)
    if err != nil {
        if isUniqueConstraintError(err) {
            // Duplicate — already processed, return success
            return nil
        }
        return err
    }
    return nil
}
```

## Why This Approach

- **Compared to PR #2** (full webhook server): Lighter — integrates directly with Gitea's existing handler, no new HTTP server needed.
- **Compared to PR #4** (model-only): Complete — includes in-memory dedup for the race window before the DB constraint takes effect, plus failure recovery.
- **Defense in depth**: In-memory mutex prevents concurrent goroutine races; DB unique constraint prevents any remaining edge cases.

## Verified By

- `-race` flag tests: zero data races detected
- 50 concurrent goroutines with same external ID → exactly 1 processed
- 20 concurrent goroutines with different IDs → all 20 processed independently
- Failed processing → state reset → retry succeeds
