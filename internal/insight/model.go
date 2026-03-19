// @sentinel-project: sentinel
// @sentinel-path: internal/insight/model.go
// Package insight defines Sentinel's analytical output types.
package insight

import "time"

// Rule IDs for Sentinel correlation rules.
const (
	RuleCascadeDetection  = "S-001"
	RuleDeployCorrelation = "S-002"
	RuleDependencyRisk    = "S-003"
	RuleStaleProject      = "S-004"
	RuleHighDenialRate    = "S-005"
	RuleServiceMaintenance = "S-006" // desired=running but actual=maintenance
	RuleBuildFailureRate   = "S-007" // sustained high build failure rate from G-003
	RuleAgentDisconnected  = "S-008" // engxa offline while services desired-running
)

// Health levels for system report.
const (
	HealthHealthy  = "healthy"
	HealthDegraded = "degraded"
	HealthIncident = "incident"
)

// Severity levels.
const (
	SeverityInfo    = "info"
	SeverityWarning = "warning"
	SeverityError   = "error"
)

// Insight is a single correlated finding from Sentinel.
type Insight struct {
	RuleID   string    `json:"rule_id"`
	Severity string    `json:"severity"`
	Title    string    `json:"title"`
	Detail   string    `json:"detail"`
	Subjects []string  `json:"subjects"` // affected project IDs
	At       time.Time `json:"at"`
}

// Incident is a cluster of related insights forming an operational event.
type Incident struct {
	ID       string     `json:"id"`
	Severity string     `json:"severity"`
	Title    string     `json:"title"`
	Evidence []*Insight `json:"evidence"`
}

// SystemReport is the full platform health synthesis.
type SystemReport struct {
	Health      string     `json:"health"`
	Summary     string     `json:"summary"`
	Insights    []*Insight `json:"insights"`
	CollectedAt time.Time  `json:"collected_at"`
}

// DeployRisk is the deployment risk assessment.
type DeployRisk struct {
	Risk    string     `json:"risk"` // "low" | "medium" | "high"
	Factors []*Insight `json:"factors"`
}
