// @sentinel-project: sentinel
// @sentinel-path: SERVICE-CONTRACT.md
# SERVICE-CONTRACT.md — Sentinel
# @version: 0.3.0-phase3
# @updated: 2026-03-25

**Port:** 8087 · **Domain:** Observer (read-only)

---

## Code

```
internal/collector/platform.go   polls Atlas/Nexus/Forge/Guardian on 10-30s intervals
internal/insight/engine.go       deterministic Phase 1 rules S-001..S-005
internal/insight/model.go        SystemReport, Insight structs
internal/ai/reasoner.go          Anthropic API call -- on-demand only
internal/actuator/               policy enforcement (ADR-024 write authority)
internal/api/handler/insights.go GET /insights/*
internal/api/handler/explain.go  GET /insights/explain -- AI on-demand
```

---

## Contract

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/health` | none | Liveness |
| GET | `/insights/system` | token | Full platform health report |
| GET | `/insights/incidents` | token | Error-severity findings only |
| GET | `/insights/deploy-risk` | token | Deployment risk assessment |
| GET | `/insights/explain` | token | AI narrative -- calls Anthropic on-demand |

**Phase 1 rules:**

| Rule | Name | Severity |
|------|------|----------|
| S-001 | Cascade detection | error |
| S-002 | Deploy correlation | error |
| S-003 | Dependency risk | warning |
| S-004 | Stale project | info |
| S-005 | High denial rate | warning |

Health states: `incident` (S-001/S-002 active) then `degraded` (S-003/004/005) then `healthy`.

`/insights/explain` returns Phase 1 output with `ai_available: false` if `ANTHROPIC_API_KEY` absent or API fails. Never crashes.

---

## Control

**Collection intervals:** Atlas projects/services 30s · Nexus events 10s · Nexus metrics 15s · Forge history 30s · Guardian findings 30s.

**AI constraint:** Anthropic API called ONLY on explicit `GET /insights/explain`. Never during background collection (ADR-018).

**Snapshot isolation:** `ExplainHandler` snapshots `report.Health`, `report.Insights`, `report.CollectedAt` into local variables before the Anthropic call. Polling loop `Set()` during the 25s API call does not affect the in-flight request.

**`lastEventID`:** protected by `sync.Mutex` -- written only by polling goroutine (T2-A race fix).

Per-cycle trace ID: `st-<hex>`. One full pass before HTTP server starts.

---

## Context

Derived, non-authoritative. Never calls write endpoints (ADR-018/ADR-020). AI layer never suggests control actions -- system prompt explicitly prohibits start/stop recommendations.
