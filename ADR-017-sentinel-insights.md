# ADR-017 — Sentinel Insights Service

**Status:** Accepted
**Date:** 2026-03-17
**Author:** Harsh Maury
**Scope:** New analytical observer — Sentinel
**Port:** 8087
**Depends on:** Nexus Phase 16, Atlas Phase 3, Forge Phase 4, Guardian Phase 1

---

## Context

The platform now has seven services generating rich operational data:
- Atlas: verified project graph with capabilities and dependencies
- Nexus: structured events with component/outcome/trace_id fields
- Forge: execution history with status and duration
- Guardian: policy findings (G-001 to G-005)
- Metrics, Navigator, Observer: operational views

No component synthesizes these signals into cross-service insights.
A developer debugging an incident must manually query four services
and mentally correlate the results. Sentinel fills this gap by acting
as a read-only reasoning engine that correlates platform telemetry
into structured diagnostic insights.

Sentinel is NOT a control plane component. It never starts/stops
services, never triggers workflows, and never modifies state.
It is strictly observational — the diagnostic intelligence layer.

---

## Decision

### 1. Sentinel is a read-only analytical observer

Only reads from:
- Atlas  GET /workspace/projects      (every 30s)
- Atlas  GET /graph/services           (every 30s)
- Nexus  GET /events?since=<id>        (every 10s)
- Nexus  GET /metrics                  (every 15s)
- Forge  GET /history?limit=200        (every 30s)
- Guardian GET /guardian/findings      (every 30s)

Never writes. Never calls start/stop. Never triggers workflows.

### 2. Phase 1 correlation rules (deterministic only — no AI/LLM)

| Rule ID | Name | Signals | Output |
|---------|------|---------|--------|
| S-001 | Cascade detection | Nexus crashes + Atlas depends_on graph | "X crash may cascade to dependents Y, Z" |
| S-002 | Deploy correlation | Forge history timestamps + Nexus crash cluster timing | "Crash cluster started N min after deploy of T" |
| S-003 | Dependency risk | Atlas unverified projects + graph edges | "Unverified project in critical dependency path" |
| S-004 | Stale project | Atlas projects not seen in Nexus events >24h | "Project registered but no platform activity" |
| S-005 | High denial rate | Forge denied executions + Guardian G-001 findings | "Repeated denials suggest missing nexus.yaml" |

### 3. Endpoints

**GET /insights/system** — overall platform health synthesis:
```json
{
  "health": "degraded",
  "summary": "2 incidents detected, 1 dependency risk",
  "insights": [...],
  "collected_at": "..."
}
```

**GET /insights/incidents** — active incident clusters:
```json
{"incidents": [{"id": "...", "severity": "error", "title": "...", "evidence": [...]}]}
```

**GET /insights/deploy-risk** — deployment risk based on current state:
```json
{"risk": "medium", "factors": [...]}
```

### 4. Health classification

- `healthy` — no S-001/S-002 findings, Guardian clean
- `degraded` — S-003/S-004/S-005 findings present
- `incident` — S-001 or S-002 findings present

### 5. No persistence, no AI

Phase 1: all insights computed on demand from live upstream queries.
No SQLite. No LLM calls. Fully deterministic and testable.
Phase 2 (ADR-018): AI reasoning layer on top of Phase 1 structured output.

### 6. Authentication

X-Service-Token on all outbound calls.
GET /insights/* requires no inbound auth — read-only.

---

## Implementation scope

```
sentinel/
├── cmd/sentinel/main.go
├── internal/
│   ├── config/env.go
│   ├── insight/
│   │   ├── model.go      — Insight, Incident, SystemReport, DeployRisk types
│   │   └── engine.go     — evaluates S-001 to S-005, builds reports
│   ├── collector/
│   │   ├── platform.go   — fetches all upstream data, assembles PlatformState
│   └── api/
│       ├── handler/insights.go
│       └── server.go
├── go.mod
└── nexus.yaml
```

---

## Compliance

| ADR | Status |
|-----|--------|
| ADR-001 | ✅ Never maintains its own project list |
| ADR-003 | ✅ HTTP/JSON only |
| ADR-005 | ✅ Never calls start/stop |
| ADR-008 | ✅ X-Service-Token on all outbound calls |

---

## Next ADR

ADR-018 — Sentinel Phase 2: AI reasoning layer (LLM inference on top
of Phase 1 structured output). Separate evaluation required.
ADR-016 — Platform shared types module (still pending).
