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

	accord "github.com/Harshmaury/Accord/api"
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
// ADR-039/F-3: now uses Accord DTOs directly — no local shadow types.
type PlatformState struct {
	Projects    []accord.AtlasProjectDTO
	Events      []accord.EventDTO
	Metrics     accord.NexusMetricsDTO
	Executions  []accord.ForgeExecutionDTO
	Findings    []accord.GuardianFindingDTO
	Services    []accord.ServiceDTO
	Agents      []accord.AgentDTO
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
		lastEventID: loadCursor("sentinel"),}
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
func (c *Collector) fetchProjects(ctx context.Context) []accord.AtlasProjectDTO {
	projs, err := c.atlas.Atlas().Projects(ctx)
	if err != nil {
		return nil
	}
	return projs
}

// fetchEvents uses Herald Nexus events client.
// mu serialises access to lastEventID — safe for concurrent callers.
func (c *Collector) fetchEvents(ctx context.Context) []accord.EventDTO {
	c.mu.Lock()
	sinceID := c.lastEventID
	c.mu.Unlock()

	evts, err := c.nexus.Events().Since(ctx, sinceID, 200)
	if err != nil {
		return nil
	}

	var maxID int64
	result := make([]accord.EventDTO, 0, len(evts))
	for _, e := range evts {
		if e.ID > maxID {
			maxID = e.ID
		}
		result = append(result, e)
	}

	if maxID > sinceID {
		c.mu.Lock()
		if maxID > c.lastEventID {
			c.lastEventID = maxID
		saveCursor("sentinel", c.lastEventID)
		}
		c.mu.Unlock()
	}
	return result
}

// fetchMetrics uses Herald NexusMetrics client.
func (c *Collector) fetchMetrics(ctx context.Context) accord.NexusMetricsDTO {
	m, err := c.nexus.NexusMetrics().Get(ctx)
	if err != nil {
		return accord.NexusMetricsDTO{}
	}
	return *m
}

// fetchHistory uses Herald Forge client.
func (c *Collector) fetchHistory(ctx context.Context) []accord.ForgeExecutionDTO {
	records, err := c.forge.Forge().History(ctx, 200)
	if err != nil {
		return nil
	}
	return records
}

// fetchFindings uses Herald Guardian client.
func (c *Collector) fetchFindings(ctx context.Context) []accord.GuardianFindingDTO {
	report, err := c.guardian.Guardian().Findings(ctx)
	if err != nil {
		return nil
	}
	return report.Findings
}

// fetchServices uses Herald Nexus services client.
func (c *Collector) fetchServices(ctx context.Context) []accord.ServiceDTO {
	svcs, err := c.nexus.Services().List(ctx)
	if err != nil {
		return nil
	}
	return svcs
}

// fetchAgents uses Herald Nexus agents client.
func (c *Collector) fetchAgents(ctx context.Context) []accord.AgentDTO {
	agts, err := c.nexus.Agents().List(ctx)
	if err != nil {
		return nil
	}
	return agts
}
