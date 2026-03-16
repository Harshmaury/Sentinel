# WORKFLOW-SESSION.md
# Session: ST-phase1-sentinel-insights
# Date: 2026-03-17

## What changed — Sentinel Phase 1 (ADR-017)

New analytical insights service. Correlates data from Atlas, Nexus,
Forge, and Guardian to produce structured platform insights via
GET /insights/system, /incidents, /deploy-risk.

## Setup and run

mkdir -p ~/workspace/projects/apps/sentinel
cd ~/workspace/projects/apps/sentinel
unzip -o /mnt/c/Users/harsh/Downloads/engx-drop/sentinel-phase1-insights-20260317.zip -d .
go mod tidy && go build ./...
go install ./cmd/sentinel/ && cp ~/go/bin/sentinel ~/bin/sentinel
SENTINEL_SERVICE_TOKEN=7d5fcbe4-44b9-4a8f-8b79-f80925c1330e sentinel &

## Verify

curl -s http://127.0.0.1:8087/health
curl -s http://127.0.0.1:8087/insights/system | jq '.data | {health, summary}'
curl -s http://127.0.0.1:8087/insights/system | jq '.data.insights[] | {rule:.rule_id, title:.title}'
curl -s http://127.0.0.1:8087/insights/incidents | jq '.data.incidents'
curl -s http://127.0.0.1:8087/insights/deploy-risk | jq '.data'

## Commit

git init && git add . && \
git commit -m "feat: sentinel insights phase 1 (ADR-017)" && \
git tag v0.1.0-phase1
