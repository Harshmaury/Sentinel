// @accord-project: Accord
// @accord-path: api/upstream.go
// Upstream DTOs for Atlas, Forge, Guardian, and Nexus /metrics.
// These types are the compile-time contract between Herald clients and
// the services they call. Verified against each service's handler output.
//
// Verified against:
//   Atlas   internal/api/handler/workspace.go  projectResponse shape
//   Atlas   internal/api/handler/graph.go       GraphEdge shape
//   Forge   internal/store/storer.go            ExecutionRecord shape
//   Guardian internal/policy/model.go           Finding + Report shapes
//   Nexus   internal/telemetry/metrics.go       Snapshot shape
//
// Last verified: 2026-03-21
package api

import "time"

// ── ATLAS TYPES ───────────────────────────────────────────────────────────────

// AtlasProjectDTO is the API shape returned by Atlas GET /workspace/projects.
// Matches atlas/internal/api/handler/workspace.go projectResponse exactly.
type AtlasProjectDTO struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Path         string   `json:"path"`
	Language     string   `json:"language"`
	Type         string   `json:"type"`
	Source       string   `json:"source"`
	Status       string   `json:"status"` // "verified" | "unverified"
	Capabilities []string `json:"capabilities"`
	DependsOn    []string `json:"depends_on"`
	IndexedAt    string   `json:"indexed_at"`
}

// AtlasEdgeDTO is one graph edge from Atlas GET /workspace/graph.
// Matches the edge element shape in the handler response.
type AtlasEdgeDTO struct {
	FromID   string `json:"from_id"`
	ToID     string `json:"to_id"`
	EdgeType string `json:"edge_type"`
	Source   string `json:"source"`
}

// AtlasGraphDTO is the full response body of Atlas GET /workspace/graph.
type AtlasGraphDTO struct {
	Total  int            `json:"total"`
	Edges  []AtlasEdgeDTO `json:"edges"`
}

// ── FORGE TYPES ───────────────────────────────────────────────────────────────

// ForgeExecutionDTO is one record from Forge GET /history or GET /history/:trace_id.
// Matches forge/internal/store/storer.go ExecutionRecord json tags exactly.
type ForgeExecutionDTO struct {
	ID         string    `json:"id"`
	CommandID  string    `json:"command_id"`
	Intent     string    `json:"intent"`
	Target     string    `json:"target"`
	TraceID    string    `json:"trace_id"`
	Status     string    `json:"status"`
	Output     string    `json:"output,omitempty"`
	Error      string    `json:"error,omitempty"`
	DurationMS int64     `json:"duration_ms"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

// ── GUARDIAN TYPES ────────────────────────────────────────────────────────────

// GuardianFindingDTO is one finding from Guardian GET /guardian/findings.
// Matches guardian/internal/policy/model.go Finding json tags exactly.
type GuardianFindingDTO struct {
	RuleID    string    `json:"rule_id"`
	Severity  string    `json:"severity"`
	Target    string    `json:"target"`
	Message   string    `json:"message"`
	Count     int       `json:"count"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}

// GuardianSummaryDTO is the findings summary from GET /guardian/findings.
type GuardianSummaryDTO struct {
	Total    int `json:"total"`
	Warnings int `json:"warnings"`
	Errors   int `json:"errors"`
}

// GuardianReportDTO is the full response body of GET /guardian/findings.
// Matches guardian/internal/policy/model.go Report json tags exactly.
type GuardianReportDTO struct {
	Findings    []GuardianFindingDTO `json:"findings"`
	Summary     GuardianSummaryDTO   `json:"summary"`
	EvaluatedAt time.Time            `json:"evaluated_at"`
}

// ── NEXUS METRICS TYPES ───────────────────────────────────────────────────────

// NexusMetricsDTO is the response body of Nexus GET /metrics.
// Matches nexus/internal/telemetry/metrics.go Snapshot json tags exactly.
type NexusMetricsDTO struct {
	UptimeSeconds         float64 `json:"uptime_seconds"`
	ReconcileCyclesTotal  int64   `json:"reconcile_cycles_total"`
	ServicesStartedTotal  int64   `json:"services_started_total"`
	ServicesStoppedTotal  int64   `json:"services_stopped_total"`
	ServicesCrashedTotal  int64   `json:"services_crashed_total"`
	ServicesDeferredTotal int64   `json:"services_deferred_total"`
	ReconcileErrorsTotal  int64   `json:"reconcile_errors_total"`
	ServicesRunning       int64   `json:"services_running"`
	ServicesInMaintenance int64   `json:"services_in_maintenance"`
}
