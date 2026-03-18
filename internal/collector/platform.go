// @sentinel-project: sentinel
// @sentinel-path: internal/collector/platform.go
// Package collector fetches and assembles PlatformState from all upstream services.
// PlatformState is the single input to the Sentinel insight engine.
//
// Concurrency safety (ISSUE-001 fix):
//   lastEventID is protected by mu. Two goroutines call Collect():
//   1. 30s polling loop (analyze)
//   2. HTTP handler (GET /insights/deploy-risk)
//   mu.Lock() wraps the lastEventID read and write in fetchEvents().
package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Harshmaury/Canon/identity"
)

// ── UPSTREAM DATA TYPES ───────────────────────────────────────────────────────

// Project is a project from Atlas /workspace/projects.
type Project struct {
	ID           string   `json:"id"`
	Status       string   `json:"status"`
	Language     string   `json:"language"`
	Capabilities []string `json:"capabilities"`
	DependsOn    []string `json:"depends_on"`
}

// NexusEvent is an event from Nexus /events.
type NexusEvent struct {
	ID        int64     `json:"id"`
	Type      string    `json:"type"`
	Component string    `json:"component"`
	Outcome   string    `json:"outcome"`
	ServiceID string    `json:"service_id"`
	TraceID   string    `json:"trace_id"`
	CreatedAt time.Time `json:"created_at"`
}

// NexusMetrics is the runtime counter snapshot from Nexus /metrics.
type NexusMetrics struct {
	ServicesCrashedTotal int64   `json:"services_crashed_total"`
	ServicesRunning      int64   `json:"services_running"`
	UptimeSeconds        float64 `json:"uptime_seconds"`
}

// ForgeExecution is one record from Forge /history.
type ForgeExecution struct {
	Intent     string    `json:"intent"`
	Target     string    `json:"target"`
	Status     string    `json:"status"`
	DurationMS int64     `json:"duration_ms"`
	TraceID    string    `json:"trace_id"`
	StartedAt  time.Time `json:"started_at"`
}

// GuardianFinding is one finding from Guardian /guardian/findings.
type GuardianFinding struct {
	RuleID   string    `json:"rule_id"`
	Severity string    `json:"severity"`
	Target   string    `json:"target"`
	Message  string    `json:"message"`
	Count    int       `json:"count"`
	LastSeen time.Time `json:"last_seen"`
}

// PlatformState is the assembled snapshot of all upstream data.
type PlatformState struct {
	Projects    []*Project
	Events      []*NexusEvent
	Metrics     NexusMetrics
	Executions  []*ForgeExecution
	Findings    []*GuardianFinding
	CollectedAt time.Time
}

// ── COLLECTOR ─────────────────────────────────────────────────────────────────

// Collector fetches data from all platform services.
// mu protects lastEventID — Collect() is called from both the polling
// goroutine (analyze) and the HTTP handler (DeployRisk). (ISSUE-001)
type Collector struct {
	atlasAddr    string
	nexusAddr    string
	forgeAddr    string
	guardianAddr string
	serviceToken string
	httpClient   *http.Client
	mu           sync.Mutex // guards lastEventID
	lastEventID  int64
}

// NewCollector creates a Collector.
func NewCollector(atlasAddr, nexusAddr, forgeAddr, guardianAddr, serviceToken string) *Collector {
	return &Collector{
		atlasAddr:    atlasAddr,
		nexusAddr:    nexusAddr,
		forgeAddr:    forgeAddr,
		guardianAddr: guardianAddr,
		serviceToken: serviceToken,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}
}

// Collect fetches all upstream data and returns a PlatformState.
// traceID is the collection-cycle trace ID for X-Trace-ID propagation (FEAT-002).
func (c *Collector) Collect(ctx context.Context, traceID string) *PlatformState {
	state := &PlatformState{CollectedAt: time.Now().UTC()}
	state.Projects  = c.fetchProjects(ctx, traceID)
	state.Events    = c.fetchEvents(ctx, traceID)
	state.Metrics   = c.fetchMetrics(ctx, traceID)
	state.Executions = c.fetchHistory(ctx, traceID)
	state.Findings  = c.fetchFindings(ctx, traceID)
	return state
}

func (c *Collector) fetchProjects(ctx context.Context, traceID string) []*Project {
	resp, err := c.get(ctx, c.atlasAddr, "/workspace/projects", traceID)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var env struct {
		OK   bool       `json:"ok"`
		Data []*Project `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil
	}
	return env.Data
}

// fetchEvents reads and advances lastEventID under mu to prevent the
// data race between the polling goroutine and DeployRisk handler. (ISSUE-001)
func (c *Collector) fetchEvents(ctx context.Context, traceID string) []*NexusEvent {
	c.mu.Lock()
	path := fmt.Sprintf("/events?since=%d&limit=200", c.lastEventID)
	c.mu.Unlock()

	resp, err := c.get(ctx, c.nexusAddr, path, traceID)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var env struct {
		OK   bool          `json:"ok"`
		Data []*NexusEvent `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil
	}

	c.mu.Lock()
	for _, e := range env.Data {
		if e.ID > c.lastEventID {
			c.lastEventID = e.ID
		}
	}
	c.mu.Unlock()

	return env.Data
}

func (c *Collector) fetchMetrics(ctx context.Context, traceID string) NexusMetrics {
	resp, err := c.get(ctx, c.nexusAddr, "/metrics", traceID)
	if err != nil {
		return NexusMetrics{}
	}
	defer resp.Body.Close()
	var m NexusMetrics
	json.NewDecoder(resp.Body).Decode(&m) //nolint:errcheck
	return m
}

func (c *Collector) fetchHistory(ctx context.Context, traceID string) []*ForgeExecution {
	resp, err := c.get(ctx, c.forgeAddr, "/history?limit=200", traceID)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var env struct {
		OK   bool              `json:"ok"`
		Data []*ForgeExecution `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil
	}
	return env.Data
}

func (c *Collector) fetchFindings(ctx context.Context, traceID string) []*GuardianFinding {
	resp, err := c.get(ctx, c.guardianAddr, "/guardian/findings", traceID)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Findings []*GuardianFinding `json:"findings"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil
	}
	return env.Data.Findings
}

// get performs an authenticated GET against a platform service.
// Uses Canon identity constants for headers (ISSUE-003).
// Passes traceID as X-Trace-ID on every outbound call (FEAT-002).
func (c *Collector) get(ctx context.Context, base, path, traceID string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return nil, err
	}
	if c.serviceToken != "" && path != "/health" {
		req.Header.Set(identity.ServiceTokenHeader, c.serviceToken)
	}
	if traceID != "" {
		req.Header.Set(identity.TraceIDHeader, traceID)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d for %s%s", resp.StatusCode, base, path)
	}
	return resp, nil
}
