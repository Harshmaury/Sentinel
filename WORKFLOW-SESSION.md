# WORKFLOW-SESSION.md
# Session: ST-fix-canon-version
# Date: 2026-03-19

## What changed

Two fixes in one delivery:
1. Canon migration (ADR-016): replaced raw "X-Service-Token" in platform
   collector with canon.ServiceTokenHeader.
2. Version drift fix: sentinelVersion and nexus.yaml now match
   SERVICE-CONTRACT.md (0.2.0-phase2).

## Modified files
- internal/collector/platform.go  — canon import + ServiceTokenHeader
- cmd/sentinel/main.go             — sentinelVersion = "0.2.0"
- nexus.yaml                       — version: 0.2.0

## Apply

cd ~/workspace/projects/apps/sentinel && \
unzip -o /mnt/c/Users/harsh/Downloads/engx-drop/sentinel-fix-canon-version-20260319.zip -d . && \
go build ./...

## Verify

grep 'canon.ServiceTokenHeader' internal/collector/platform.go
grep '"X-Service-Token"' internal/collector/platform.go   # expected: no output
grep 'sentinelVersion' cmd/sentinel/main.go                # expected: "0.2.0"
grep 'version' nexus.yaml                                  # expected: 0.2.0

## Commit

git add \
  internal/collector/platform.go \
  cmd/sentinel/main.go \
  nexus.yaml \
  WORKFLOW-SESSION.md && \
git commit -m "fix: Canon migration + version sync 0.2.0 (audit #2, #6)" && \
git push origin main
