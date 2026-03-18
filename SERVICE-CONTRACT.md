# SERVICE-CONTRACT.md — Sentinel

**Service:** sentinel
**Domain:** Observer
**Port:** 8087
**ADRs:** ADR-017 (Phase 1 insights), ADR-018 (Phase 2 AI reasoning), ADR-020 (governance)
**Version:** 0.2.0-phase2
**Updated:** 2026-03-18

---

## Role

Read-only analytical observer. Correlates platform telemetry into structured
diagnostic insights (Phase 1) and generates AI narrative reasoning on demand
(Phase 2). Sentinel has no execution authority of any kind.

---

## Inputs

**Background collection (every 30s):**
- `Atlas GET /workspace/projects` — project graph
- `Atlas GET /graph/services` — verified services
- `Nexus GET /events?since=<id>` — platform events (every 10s)
- `Nexus GET /metrics` — runtime counters (every 15s)
- `Forge GET /history?limit=200` — execution history
- `Guardian GET /guardian/findings` — policy findings

**On-demand (only when `GET /insights/explain` is called):**
- Anthropic API `/v1/messages` — `claude-sonnet-4-6` model

All background inputs are read-only HTTP GET calls.

---

## Outputs

- `GET /health` — `{"ok":true,"status":"healthy","service":"sentinel"}`
- `GET /insights/system` — full platform health report (Phase 1)
- `GET /insights/incidents` — error-severity findings only (Phase 1)
- `GET /insights/deploy-risk` — deployment risk assessment (Phase 1)
- `GET /insights/explain` — AI narrative reasoning (Phase 2, on-demand)

---

## Dependencies

| Service    | Used for                    | Auth required   |
|------------|-----------------------------|-----------------|
| Atlas      | Project graph + services    | X-Service-Token |
| Nexus      | Events + runtime metrics    | X-Service-Token |
| Forge      | Execution history           | X-Service-Token |
| Guardian   | Policy findings             | None (ADR-020)  |
| Anthropic  | AI narrative (on-demand)    | ANTHROPIC_API_KEY |

---

## Phase 1 correlation rules

| Rule  | Name               | Severity |
|-------|--------------------|----------|
| S-001 | Cascade detection  | error    |
| S-002 | Deploy correlation | error    |
| S-003 | Dependency risk    | warning  |
| S-004 | Stale project      | info     |
| S-005 | High denial rate   | warning  |

Health: `incident` (S-001/S-002 present), `degraded` (S-003/S-004/S-005), `healthy`.

---

## Guarantees

- Phase 1 rules are fully deterministic — no AI involved in background polling.
- AI (Anthropic) is called ONLY when the developer explicitly requests
  `GET /insights/explain` — never on background collection cycles.
- AI receives only the Phase 1 structured `SystemReport` — never raw events
  or raw graph data.
- If `ANTHROPIC_API_KEY` is absent or the API call fails, `/insights/explain`
  returns Phase 1 output with `ai_available: false`. Service never crashes.
- One full collection pass completes before the HTTP server starts (ADR-020 Rule 6).
- Each collection cycle carries a unique `st-<hex>` trace ID on all outbound calls.

---

## Non-Responsibilities

- **Sentinel never calls** `POST /projects/:id/start` or `/stop` on Nexus.
- **Sentinel never calls** `POST /commands` on Forge.
- **Sentinel never writes** to any platform database.
- **Sentinel never triggers** workflows or automation.
- **The AI layer never suggests control actions** — system prompt explicitly
  prohibits start/stop recommendations.
- **Sentinel is not a decision system.** It suggests. The developer decides.
- Sentinel does not own project state, service state, or execution authority.

---

## Data Authority

**Derived, non-authoritative.**

All insights are computed from data owned by other services.
Sentinel does not persist findings — the in-memory `StateStore` reflects
only the most recent collection cycle.

---

## Concurrency Model

- `StateStore` protected by `sync.RWMutex`. `Set()` takes write lock,
  `Get()` takes read lock.
- `ExplainHandler` snapshots `report.Health`, `report.Insights`, and
  `report.CollectedAt` into local variables immediately after `Get()` returns —
  before the 25-second Anthropic call begins. The Reasoner works on an
  immutable snapshot even if the polling loop calls `Set()` mid-call.
- Single polling goroutine owns all collection and store writes.
- HTTP handlers are read-only — they call `Get()` only, then snapshot.
