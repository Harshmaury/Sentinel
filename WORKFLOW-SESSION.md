# WORKFLOW-SESSION.md
# Session: ST-phase2-ai-reasoning
# Date: 2026-03-17

## What changed — Sentinel Phase 2 (ADR-018)

AI reasoning layer on top of Phase 1 structured insights.
GET /insights/explain calls Anthropic API with Phase 1 report
and returns human-readable narrative. Degrades gracefully if
ANTHROPIC_API_KEY is not set.

## New files
- internal/ai/reasoner.go           — Anthropic API client, prompt construction
- internal/api/handler/explain.go   — GET /insights/explain handler

## Modified files
- internal/api/server.go            — Reasoner injected, /insights/explain route
- cmd/sentinel/main.go              — ANTHROPIC_API_KEY read, Reasoner wired

## Apply

cd ~/workspace/projects/apps/sentinel && \
unzip -o /mnt/c/Users/harsh/Downloads/engx-drop/sentinel-phase2-ai-reasoning-20260317.zip -d . && \
go mod tidy && go build ./...

## Run with AI enabled

pkill sentinel 2>/dev/null; sleep 1
SENTINEL_SERVICE_TOKEN=7d5fcbe4-44b9-4a8f-8b79-f80925c1330e \
ANTHROPIC_API_KEY=<your-key> \
sentinel &

## Run without AI (Phase 1 only)

SENTINEL_SERVICE_TOKEN=7d5fcbe4-44b9-4a8f-8b79-f80925c1330e sentinel &

## Verify

curl -s http://127.0.0.1:8087/insights/explain | jq '.data | {health, ai_available, ai_reasoning}'

## Commit

git add \
  internal/ai/reasoner.go \
  internal/api/handler/explain.go \
  internal/api/server.go \
  cmd/sentinel/main.go \
  WORKFLOW-SESSION.md && \
git commit -m "feat(phase2): AI reasoning layer GET /insights/explain (ADR-018)" && \
git tag v0.2.0-phase2 && \
git push origin main --tags
