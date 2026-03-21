// @sentinel-project: sentinel
// @sentinel-path: internal/collector/platform.go
// ADR-039: complete Herald migration — all upstream calls now use typed clients.
// FIX T2-A (retained): mu protects lastEventID for concurrent access safety.
//
// Migration summary:
//   fetchProjects  → atlas Herald client (was raw HTTP to Atlas)
//   fetchEvents    → nexus Herald client (already Herald, mutex retained)
//   fetchMetrics   → nexus Herald NexusMetrics client (was raw HTTP)
//   fetchHistory   → forge Herald client (was raw HTTP to Forge)
//   fetchFindings  → guardian Herald client (was raw HTTP to Guardian)
//   fetchServices  → nexus Herald client (already Herald)
//   fetchAgents    → nexus Herald client (already Herald)
//
// Raw httpClient removed entirely — no net/http dependency remains.
package collector

import (
	"context"
	"sync"
	"time"

	herald "github.com/Harshmaury/Herald/client"
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
	Services    []*Service
	Agents      []*Agent
	CollectedAt time.Time
}

// ── COLLECTOR ─────────────────────────────────────────────────────────────────

// Collector fetches data from all platform services via Herald.
// ADR-039: all upstream calls use typed Herald clients — no raw HTTP remains.
// Concurrency: Collect() is safe for concurrent callers — mu protects lastEventID.
type Collector struct {
	nexus    *herald.Client // Nexus: events, services, agents, metrics
	atlas    *herald.Client // Atlas: workspace projects
	forge    *herald.Client // Forge: execution history
	guardian *herald.Client // Guardian: findings

	mu          sync.Mutex // protects lastEventID
	lastEventID int64
}

// NewCollector creates a Collector with Herald clients for all upstream services.
func NewCollector(atlasAddr, nexusAddr, forgeAddr, guardianAddr, serviceToken string) *Collector {
	return &Collector{
		nexus:    herald.New(nexusAddr, herald.WithToken(serviceToken)),
		atlas:    herald.NewForService(atlasAddr, serviceToken),
		forge:    herald.NewForService(forgeAddr, serviceToken),
		guardian: herald.NewForService(guardianAddr, serviceToken),
	}
}

// Collect fetches all upstream data and returns a PlatformState.
// Safe for concurrent callers.
func (c *Collector) Collect(ctx context.Context) *PlatformState {
	state := &PlatformState{CollectedAt: time.Now().UTC()}
	state.Projects = c.fetchProjects(ctx)
	state.Events = c.fetchEvents(ctx)
	state.Metrics = c.fetchMetrics(ctx)
	state.Executions = c.fetchHistory(ctx)
	state.Findings = c.fetchFindings(ctx)
	state.Services = c.fetchServices(ctx)
	state.Agents = c.fetchAgents(ctx)
	return state
}

// fetchProjects uses Herald Atlas client.
func (c *Collector) fetchProjects(ctx context.Context) []*Project {
	projs, err := c.atlas.Atlas().Projects(ctx)
	if err != nil {
		return nil
	}
	out := make([]*Project, 0, len(projs))
	for _, p := range projs {
		caps := p.Capabilities
		if caps == nil {
			caps = []string{}
		}
		deps := p.DependsOn
		if deps == nil {
			deps = []string{}
		}
		out = append(out, &Project{
			ID:           p.ID,
			Status:       p.Status,
			Language:     p.Language,
			Capabilities: caps,
			DependsOn:    deps,
		})
	}
	return out
}

// fetchEvents uses Herald Nexus events client.
// mu serialises access to lastEventID — safe for concurrent callers.
func (c *Collector) fetchEvents(ctx context.Context) []*NexusEvent {
	c.mu.Lock()
	sinceID := c.lastEventID
	c.mu.Unlock()

	evts, err := c.nexus.Events().Since(ctx, sinceID, 200)
	if err != nil {
		return nil
	}

	var maxID int64
	result := make([]*NexusEvent, 0, len(evts))
	for _, e := range evts {
		if e.ID > maxID {
			maxID = e.ID
		}
		var ts time.Time
		ts, _ = time.Parse(time.RFC3339Nano, e.CreatedAt)
		if ts.IsZero() {
			ts, _ = time.Parse(time.RFC3339, e.CreatedAt)
		}
		result = append(result, &NexusEvent{
			ID: e.ID, Type: e.Type, Component: e.Component,
			Outcome: e.Outcome, ServiceID: e.ServiceID,
			TraceID: e.TraceID, CreatedAt: ts,
		})
	}

	if maxID > sinceID {
		c.mu.Lock()
		if maxID > c.lastEventID {
			c.lastEventID = maxID
		}
		c.mu.Unlock()
	}
	return result
}

// fetchMetrics uses Herald NexusMetrics client.
func (c *Collector) fetchMetrics(ctx context.Context) NexusMetrics {
	m, err := c.nexus.NexusMetrics().Get(ctx)
	if err != nil {
		return NexusMetrics{}
	}
	return NexusMetrics{
		ServicesCrashedTotal: m.ServicesCrashedTotal,
		ServicesRunning:      m.ServicesRunning,
		UptimeSeconds:        m.UptimeSeconds,
	}
}

// fetchHistory uses Herald Forge client.
func (c *Collector) fetchHistory(ctx context.Context) []*ForgeExecution {
	records, err := c.forge.Forge().History(ctx, 200)
	if err != nil {
		return nil
	}
	out := make([]*ForgeExecution, 0, len(records))
	for _, r := range records {
		out = append(out, &ForgeExecution{
			Intent:     r.Intent,
			Target:     r.Target,
			Status:     r.Status,
			DurationMS: r.DurationMS,
			TraceID:    r.TraceID,
			StartedAt:  r.StartedAt,
		})
	}
	return out
}

// fetchFindings uses Herald Guardian client.
func (c *Collector) fetchFindings(ctx context.Context) []*GuardianFinding {
	report, err := c.guardian.Guardian().Findings(ctx)
	if err != nil {
		return nil
	}
	out := make([]*GuardianFinding, 0, len(report.Findings))
	for _, f := range report.Findings {
		out = append(out, &GuardianFinding{
			RuleID:   f.RuleID,
			Severity: f.Severity,
			Target:   f.Target,
			Message:  f.Message,
			Count:    f.Count,
			LastSeen: f.LastSeen,
		})
	}
	return out
}

// fetchServices uses Herald Nexus services client.
func (c *Collector) fetchServices(ctx context.Context) []*Service {
	svcs, err := c.nexus.Services().List(ctx)
	if err != nil {
		return nil
	}
	out := make([]*Service, 0, len(svcs))
	for _, s := range svcs {
		out = append(out, &Service{
			ID: s.ID, Name: s.Name, Project: s.Project,
			DesiredState: s.DesiredState, ActualState: s.ActualState,
			FailCount: s.FailCount,
		})
	}
	return out
}

// fetchAgents uses Herald Nexus agents client.
func (c *Collector) fetchAgents(ctx context.Context) []*Agent {
	agts, err := c.nexus.Agents().List(ctx)
	if err != nil {
		return nil
	}
	out := make([]*Agent, 0, len(agts))
	for _, a := range agts {
		out = append(out, &Agent{ID: a.ID, Online: a.Online})
	}
	return out
}
