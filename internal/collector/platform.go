// @sentinel-project: sentinel
// @sentinel-path: internal/collector/platform.go
// Package collector fetches and assembles PlatformState from all upstream services.
// PlatformState is the single input to the Sentinel insight engine.
package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	canon "github.com/Harshmaury/Canon/identity"
)

// ── UPSTREAM DATA TYPES ───────────────────────────────────────────────────────

// Service is a managed process from Nexus /services.
type Service struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Project      string `json:"project"`
	DesiredState string `json:"desired_state"`
	ActualState  string `json:"actual_state"`
	FailCount    int    `json:"fail_count"`
}

// Agent is a registered engxa agent from Nexus /agents.
type Agent struct {
	ID     string `json:"id"`
	Online bool   `json:"online"`
}

// Project is a project from Atlas /workspace/projects.
type Project struct {
	ID          string   `json:"id"`
	Status      string   `json:"status"`
	Language    string   `json:"language"`
	Capabilities []string `json:"capabilities"`
	DependsOn   []string `json:"depends_on"`
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
	ServicesCrashedTotal  int64   `json:"services_crashed_total"`
	ServicesRunning       int64   `json:"services_running"`
	UptimeSeconds         float64 `json:"uptime_seconds"`
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
	Services    []*Service  // S-006, S-008: service lifecycle state
	Agents      []*Agent    // S-008: agent connectivity
	CollectedAt time.Time
}

// ── COLLECTOR ─────────────────────────────────────────────────────────────────

// Collector fetches data from all platform services.
type Collector struct {
	atlasAddr    string
	nexusAddr    string
	forgeAddr    string
	guardianAddr string
	serviceToken string
	httpClient   *http.Client
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
func (c *Collector) Collect(ctx context.Context) *PlatformState {
	state := &PlatformState{CollectedAt: time.Now().UTC()}
	state.Projects   = c.fetchProjects(ctx)
	state.Events     = c.fetchEvents(ctx)
	state.Metrics    = c.fetchMetrics(ctx)
	state.Executions = c.fetchHistory(ctx)
	state.Findings   = c.fetchFindings(ctx)
	state.Services   = c.fetchServices(ctx)
	state.Agents     = c.fetchAgents(ctx)
	return state
}

func (c *Collector) fetchProjects(ctx context.Context) []*Project {
	resp, err := c.get(ctx, c.atlasAddr, "/workspace/projects")
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

func (c *Collector) fetchEvents(ctx context.Context) []*NexusEvent {
	path := fmt.Sprintf("/events?since=%d&limit=200", c.lastEventID)
	resp, err := c.get(ctx, c.nexusAddr, path)
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
	for _, e := range env.Data {
		if e.ID > c.lastEventID {
			c.lastEventID = e.ID
		}
	}
	return env.Data
}

func (c *Collector) fetchMetrics(ctx context.Context) NexusMetrics {
	resp, err := c.get(ctx, c.nexusAddr, "/metrics")
	if err != nil {
		return NexusMetrics{}
	}
	defer resp.Body.Close()
	var m NexusMetrics
	json.NewDecoder(resp.Body).Decode(&m)
	return m
}

func (c *Collector) fetchHistory(ctx context.Context) []*ForgeExecution {
	resp, err := c.get(ctx, c.forgeAddr, "/history?limit=200")
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

func (c *Collector) fetchFindings(ctx context.Context) []*GuardianFinding {
	resp, err := c.get(ctx, c.guardianAddr, "/guardian/findings")
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

func (c *Collector) fetchServices(ctx context.Context) []*Service {
	resp, err := c.get(ctx, c.nexusAddr, "/services")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var env struct {
		OK   bool       `json:"ok"`
		Data []*Service `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil
	}
	return env.Data
}

func (c *Collector) fetchAgents(ctx context.Context) []*Agent {
	resp, err := c.get(ctx, c.nexusAddr, "/agents")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var env struct {
		OK   bool     `json:"ok"`
		Data []*Agent `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil
	}
	return env.Data
}

func (c *Collector) get(ctx context.Context, base, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return nil, err
	}
	if c.serviceToken != "" && path != "/health" {
		req.Header.Set(canon.ServiceTokenHeader, c.serviceToken)
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
