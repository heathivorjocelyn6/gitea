# Bounty Fix

**Reward:** $10.00
**Issue:** https://github.com/heathivorjocelyn6/gitea/issues/1
**Repo:** https://github.com/heathivorjocelyn6/gitea

---

## Issue

🎯 Prevent Duplicate Check Runs by Handling Concurrent Webhook Deliveries Idempotently

## 📝 Description
When Gitea receives duplicate or rapidly retried GitHub webhook deliveries (e.g., for `check_run` or `check_suite` events), they are processed concurrently in separate goroutines. Because there is currently no concurrency control or strict idempotency check during the ingestion phase, this race condition results in the creation of duplicate Check Runs in the database for the same external execution.

This issue requires implementing a mechanism to ensure that concurrent webhook deliveries for the same external Check Run ID are handled idempotently, preventing duplicate database records and ensuring data consistency.

## 🎯 Acceptance Criteria
- [ ] **Idempotency Guarantee**: Multiple concurrent webhook requests for the same Check Run (identified by the external provider's ID, e.g., GitHub's `check_run.id`) must not result in duplicate records in the database.
- [ ] **Concurrency Control**: Implement a locking mechanism (e.g., database-level unique constraints, transaction-level locks, or a distributed/memory lock based on the delivery/external ID) to serialize or safely discard duplicate concurrent writes.
- [ ] **Graceful Handling**: If a duplicate delivery is detected or fails a unique constraint, the application should handle the error gracefully (e.g., returning a `200 OK` or `202 Accepted` with a message indicating it is already processed/processing, rather than a `500 Internal Server Error`).
- [ ] **Backward Compatibility**: Ensure database migrations are provided if a new unique index/constraint is introduced to the schema.

## 🛠️ Technical Specifications & Context
- **Technology Stack**: Go, XORM (Go's ORM used in Gitea), SQL (PostgreSQL, MySQL, SQLite, MSSQL).
- **Key Files & Locations**:
  - Webhook routing and payload handling: `services/webhook/` and `routers/api/v1/repo/` (specifically where incoming GitHub/Gitea actions webhooks are received).
  - Check Run models and database operations: `models/actions/check_run.go` (or the corresponding model handling check runs/status checks).
- **Proposed Implementation Paths**:
  - **Database Constraint (Recommended)**: Add a unique index on the `check_run` table combining `repo_id` and `external_id` (or `uuid`). Update the insertion logic in XORM to handle conflict resolution (e.g., `INSERT ... ON CONFLICT DO NOTHING` or catching the unique key violation error and returning a clean response).
  - **Distributed/Mutex Lock**: Introduce a short-lived lock (using Gitea's internal cache/lock system or a sync map for in-memory single-instance setups) keyed by the webhook delivery ID or the external check run ID to prevent concurrent execution of the creation logic.

## 🧪 Verification & Testing
- **Unit/Integration Tests**:
  - Add a test case in `services/webhook/` or `models/actions/check_run_test.go` that spawns multiple concurrent goroutines attempting to insert/process the same webhook payload simultaneously.
  - Verify that only one database record is created and the other concurrent requests complete without causing database corruption or returning `500` errors.
- **Manual Verification**:
  - Simulate duplicate webhook deliveries using `curl` or a script sending identical payloads to the Gitea webhook endpoint concurrently.
  - Inspect the database (`check_run` table) to confirm no duplicate rows exist for the same external ID.

---

[![Opire Bounty](https://img.shields.io/badge/bounty-Opire-emerald?style=for-the-badge)](https://github.com/heathivorjocelyn6/gitea)
<br/>
<hr/>

<details><summary>This repo is using Opire - what does it mean? 👇</summary><br/>💵 Everyone can add rewards for this issue commenting <code>/reward 100</code> (replace <code>100</code> with the amount).<br/>🕵️‍♂️ If someone starts working on this issue to earn the rewards, they can comment <code>/try</code> to let everyone know!<br/>🙌 And when they open the PR, they can comment <code>/claim #1</code> either in the PR description or in a PR's comment.<br/><br/>🪙 Also, everyone can tip any user commenting <code>/tip 20 @heathivorjocelyn6</code> (replace <code>20</code> with the amount, and <code>@heathivorjocelyn6</code> with the user to tip).<br/><br/>📖 If you want to learn more, check out our <a href="https://docs.opire.dev">documentation</a>.</details>

---

## Process

### 1. Orient yourself first
Before touching any code:
- Read the README and any relevant docs
- Run: find . -type f | grep -v node_modules | grep -v .git | grep -v dist | head -60
- Grep for the key terms from the issue title to find the relevant files
- Read the files around the bug — understand what's currently happening

### 2. Find and read the tests
- Look for test files near the relevant code (*.test.ts, *.spec.js, __tests__, etc.)
- Read them — they tell you what behavior is expected
- Understand what a passing fix looks like before writing a line

### 3. Implement the fix
- Make the MINIMAL change that fixes the issue — nothing more
- Do not refactor, rename, reorganize, or clean up unrelated code
- Do not add dependencies unless the issue explicitly requires it
- Do not modify config files (package.json, tsconfig.json, etc.) unless that IS the fix
- One logical change, one commit

### 4. Verify
- Run the test suite: check package.json scripts for "test", "lint", "typecheck"
  Common commands: npm test, yarn test, pnpm test, pytest, go test ./..., cargo test
- If TypeScript is present: npx tsc --noEmit
- Fix any test failures your change caused — if a test was wrong before, update it

### 5. Commit and document
- git add -A
- git commit -m "fix: 🎯 Prevent Duplicate Check Runs by Handling Concurrent Webhook Deliverie"
- Write AGENT_SUMMARY.md with:
  - Root cause: what was wrong and why
  - Fix: what you changed and the reasoning
  - Verification: how to confirm it works
  - Concerns: anything a reviewer should know

---

## Stop conditions

If ANY of these are true, write "UNCERTAIN: [reason]" at the top of AGENT_SUMMARY.md, make NO code changes, and stop:
- The fix requires changing more than ~5 files
- You cannot determine what the correct behavior should be
- The issue requires a database migration, API key, or external service
- You find 3+ open PRs already attempting this fix (high competition)
- The repo is not in a language/framework you can reason about confidently

A clean UNCERTAIN is better than a bad PR.
