# ADR-018 — Sentinel Phase 2: AI Reasoning Layer

**Status:** Accepted
**Date:** 2026-03-17
**Author:** Harsh Maury
**Scope:** Sentinel service — Phase 2 addition
**Depends on:** ADR-017 (Sentinel Phase 1), Anthropic API access

---

## Context

Sentinel Phase 1 produces structured insights via deterministic
correlation rules (S-001 to S-005). These insights are accurate but
terse — a developer reading them must still mentally interpret what
the findings mean together and what action to take.

The Phase 1 output is ideal LLM input: it is already structured,
correlated, and filtered. An AI layer can read this structured report
and produce human-readable narrative reasoning — explaining what is
happening, why it matters, and what to investigate — without needing
to query raw events or make platform decisions.

---

## Decision

### 1. AI layer is strictly on-demand

The LLM is called only when the developer explicitly requests it via
GET /insights/explain. It is never called on background polling cycles.
This controls cost and latency.

### 2. Input to LLM is Phase 1 structured output only

The LLM never receives raw Nexus events, raw Forge history, or raw
Atlas graph data. It receives only the structured SystemReport from
Phase 1. This limits token usage and keeps the context focused.

### 3. Model: claude-sonnet-4-6 via Anthropic API

Uses the standard Anthropic /v1/messages endpoint with a structured
system prompt that defines the AI's role as a platform diagnostician.
The Phase 1 SystemReport is injected as user content.

### 4. Graceful degradation

If the Anthropic API is unavailable or returns an error, GET /insights/explain
falls back to returning the Phase 1 structured report with a note that
AI reasoning is unavailable. The service never fails hard on LLM errors.

### 5. New endpoint: GET /insights/explain

```
GET /insights/explain
```

Returns:
```json
{
  "health": "degraded",
  "ai_reasoning": "The platform shows two dependency risk warnings...",
  "structured_insights": [...],
  "ai_available": true,
  "collected_at": "..."
}
```

If AI unavailable:
```json
{
  "health": "healthy",
  "ai_reasoning": "",
  "structured_insights": [...],
  "ai_available": false,
  "collected_at": "..."
}
```

### 6. System prompt principles

- Role: senior platform diagnostician with full knowledge of ADRs
- Task: interpret the structured findings and explain in plain English
- Constraint: never suggest control actions (start/stop services)
- Constraint: responses under 300 words
- Format: plain prose, no markdown headers

### 7. ANTHROPIC_API_KEY via environment variable

Never hardcoded. If not set, AI layer is silently disabled and
/insights/explain returns Phase 1 output with `ai_available: false`.

---

## Implementation scope

### New files in Sentinel
- `internal/ai/reasoner.go` — LLM client, prompt construction, response parsing
- `internal/api/handler/explain.go` — GET /insights/explain handler

### Modified files in Sentinel
- `internal/api/server.go` — register GET /insights/explain
- `cmd/sentinel/main.go` — wire Reasoner with API key from env

---

## Compliance

| ADR | Status |
|-----|--------|
| ADR-003 | ✅ HTTP/JSON — Anthropic API is external HTTP |
| ADR-005 | ✅ AI layer never suggests start/stop |
| ADR-006 | ✅ AI reads structured output only — never raw Atlas state |

---

## Cost awareness

Each /insights/explain call uses approximately 500–800 input tokens
(system prompt + Phase 1 report) and produces ~200 output tokens.
At claude-sonnet pricing this is negligible per call.
The on-demand model ensures zero cost when not actively used.
