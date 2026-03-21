# WORKFLOW SESSION — FIX T2-A: Sentinel Race Condition

**Fix ID:** UMS-SENTINEL-P1-001  (engx platform convention: SVC-LAYER-PRIORITY-SEQ)
**Date:** 2026-03-21  
**Status:** Ready to apply  
**Blocks:** v2.0.0 stability gate (Tier 2 stability risk, go race detector would catch this)

## What was wrong

`sentinel/internal/collector/platform.go` — `Collector.lastEventID` was an
unprotected `int64` mutated in `fetchEvents()`.

`fetchEvents()` was called from two concurrent goroutines:
1. Polling goroutine: `analyze()` → `coll.Collect()` every 30s
2. HTTP handler: `GET /insights/deploy-risk` → `h.coll.Collect()`

Both goroutines read and wrote `lastEventID` simultaneously with no lock.
This is a data race: the Go race detector (`go test -race`) catches it.
Symptom: cursor corruption, skipped events, non-deterministic insight analysis.

## What changed

**File 1: `sentinel/internal/collector/platform.go`**
- Added `mu sync.Mutex` field to `Collector` struct
- `fetchEvents()` now locks mu to read `sinceID`, releases, makes the network call,
  then re-locks to advance `lastEventID` only if the new max exceeds the stored value
- Lock-free network I/O — the mutex is not held during the Herald call

**File 2: `sentinel/internal/api/handler/insights.go`**
- `DeployRisk` handler changed from `h.coll.Collect(ctx)` → `h.store.Get()`
- The polling goroutine is now the **sole writer** to the Collector
- HTTP handlers are **read-only consumers** of StateStore (already mutex-protected)
- Cached state is at most 30s old — correct for deploy risk assessment
- Removed unused `deployRiskTraceID()` helper (was only used by the old live path)

## Apply

```powershell
# From your drop folder
cd ~/workspace/projects/engx/services/sentinel
unzip -o /path/to/UMS-SENTINEL-P1-001.zip -d .
go build ./...
```

## Verify

```bash
go test -race ./internal/collector/... ./internal/api/handler/...
# Must exit 0 with no race detector output

# Confirm DeployRisk endpoint still responds
engx platform start
curl -s http://127.0.0.1:8087/insights/deploy-risk | jq .ok
# → true
```

## What does NOT change

- `Collector` public interface is unchanged — `NewCollector` and `Collect` signatures identical
- `InsightsHandler` public interface is unchanged — `NewInsightsHandler` signature identical
- Server wiring in `api/server.go` unchanged — no changes needed there
- `StateStore.Set/Get` unchanged
- All other handlers (`System`, `Incidents`) already used `h.store.Get()` — correct
