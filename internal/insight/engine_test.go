// @sentinel-project: sentinel
// @sentinel-path: internal/insight/engine_test.go
// Tests for the Sentinel insight engine — all 8 correlation rules.
// All rules are pure functions: input is PlatformState, output is []*Insight.
// No mocks, no HTTP, no goroutines. Tests run in <1ms total.
package insight

import (
	"testing"
	"time"

	accord "github.com/Harshmaury/Accord/api"
	"github.com/Harshmaury/Sentinel/internal/collector"
)

// ── HELPERS ───────────────────────────────────────────────────────────────────

var engine = NewEngine()

// now is a fixed reference point so tests are time-independent.
var now = time.Now().UTC()

// crashEvent returns a SERVICE_CRASHED event for svcID at t.
func crashEvent(svcID string, t time.Time) accord.EventDTO {
	return accord.EventDTO{
		Type:      "SERVICE_CRASHED",
		ServiceID: svcID,
		CreatedAt: t.Format(time.RFC3339Nano),
	}
}

// emptyState returns a PlatformState with a non-nil CollectedAt.
func emptyState() *collector.PlatformState {
	return &collector.PlatformState{CollectedAt: now}
}

// assertRule checks that exactly wantCount insights with ruleID are present.
func assertRule(t *testing.T, insights []*Insight, ruleID string, wantCount int) {
	t.Helper()
	count := 0
	for _, ins := range insights {
		if ins.RuleID == ruleID {
			count++
		}
	}
	if count != wantCount {
		t.Errorf("rule %s: got %d insight(s), want %d", ruleID, count, wantCount)
	}
}

// assertNoInsights checks that Analyze produces zero insights.
func assertNoInsights(t *testing.T, report *SystemReport) {
	t.Helper()
	if len(report.Insights) != 0 {
		t.Errorf("expected 0 insights, got %d: %+v", len(report.Insights), report.Insights)
	}
}

// ── S-001: CASCADE DETECTION ─────────────────────────────────────────────────

func TestS001_CascadeDetection(t *testing.T) {
	tests := []struct {
		name       string
		state      *collector.PlatformState
		wantCount  int
	}{
		{
			name: "crash with dependents — fires",
			state: &collector.PlatformState{
				CollectedAt: now,
				Events: []accord.EventDTO{
					crashEvent("atlas", now.Add(-5*time.Minute)),
				},
				Projects: []accord.AtlasProjectDTO{
					{ID: "forge", DependsOn: []string{"atlas"}},
				},
			},
			wantCount: 1,
		},
		{
			name: "crash with no dependents — no insight",
			state: &collector.PlatformState{
				CollectedAt: now,
				Events: []accord.EventDTO{
					crashEvent("atlas", now.Add(-5*time.Minute)),
				},
				Projects: []accord.AtlasProjectDTO{
					{ID: "forge", DependsOn: []string{}},
				},
			},
			wantCount: 0,
		},
		{
			name: "crash outside 15min window — no insight",
			state: &collector.PlatformState{
				CollectedAt: now,
				Events: []accord.EventDTO{
					crashEvent("atlas", now.Add(-20*time.Minute)),
				},
				Projects: []accord.AtlasProjectDTO{
					{ID: "forge", DependsOn: []string{"atlas"}},
				},
			},
			wantCount: 0,
		},
		{
			name: "no events — no insight",
			state: &collector.PlatformState{
				CollectedAt: now,
				Projects: []accord.AtlasProjectDTO{
					{ID: "forge", DependsOn: []string{"atlas"}},
				},
			},
			wantCount: 0,
		},
		{
			name: "two crashed services each with dependents — two insights",
			state: &collector.PlatformState{
				CollectedAt: now,
				Events: []accord.EventDTO{
					crashEvent("atlas", now.Add(-2*time.Minute)),
					crashEvent("nexus", now.Add(-3*time.Minute)),
				},
				Projects: []accord.AtlasProjectDTO{
					{ID: "forge", DependsOn: []string{"atlas"}},
					{ID: "sentinel", DependsOn: []string{"nexus"}},
				},
			},
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := engine.Analyze(tt.state)
			assertRule(t, report.Insights, RuleCascadeDetection, tt.wantCount)
		})
	}
}

// ── S-002: DEPLOY CORRELATION ────────────────────────────────────────────────

func TestS002_DeployCorrelation(t *testing.T) {
	tests := []struct {
		name      string
		state     *collector.PlatformState
		wantCount int
	}{
		{
			name: "deploy before crash — fires",
			state: &collector.PlatformState{
				CollectedAt: now,
				Events: []accord.EventDTO{
					crashEvent("atlas", now.Add(-5*time.Minute)),
				},
				Executions: []accord.ForgeExecutionDTO{
					accord.ForgeExecutionDTO{Intent: "deploy", Target: "atlas", StartedAt: now.Add(-10*time.Minute)},
				},
			},
			wantCount: 1,
		},
		{
			name: "deploy after crash — no correlation",
			state: &collector.PlatformState{
				CollectedAt: now,
				Events: []accord.EventDTO{
					crashEvent("atlas", now.Add(-10*time.Minute)),
				},
				Executions: []accord.ForgeExecutionDTO{
					accord.ForgeExecutionDTO{Intent: "deploy", Target: "atlas", StartedAt: now.Add(-5*time.Minute)},
				},
			},
			wantCount: 0,
		},
		{
			name: "non-deploy execution before crash — no correlation",
			state: &collector.PlatformState{
				CollectedAt: now,
				Events: []accord.EventDTO{
					crashEvent("atlas", now.Add(-5*time.Minute)),
				},
				Executions: []accord.ForgeExecutionDTO{
					accord.ForgeExecutionDTO{Intent: "build", Target: "atlas", StartedAt: now.Add(-10*time.Minute)},
				},
			},
			wantCount: 0,
		},
		{
			name: "crashes but no executions — no insight",
			state: &collector.PlatformState{
				CollectedAt: now,
				Events: []accord.EventDTO{
					crashEvent("atlas", now.Add(-5*time.Minute)),
				},
			},
			wantCount: 0,
		},
		{
			name: "no crashes — no insight",
			state: &collector.PlatformState{
				CollectedAt: now,
				Executions: []accord.ForgeExecutionDTO{
					accord.ForgeExecutionDTO{Intent: "deploy", Target: "atlas", StartedAt: now.Add(-5*time.Minute)},
				},
			},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := engine.Analyze(tt.state)
			assertRule(t, report.Insights, RuleDeployCorrelation, tt.wantCount)
		})
	}
}

// ── S-003: DEPENDENCY RISK ────────────────────────────────────────────────────

func TestS003_DependencyRisk(t *testing.T) {
	tests := []struct {
		name      string
		state     *collector.PlatformState
		wantCount int
	}{
		{
			name: "verified project depends on unverified — fires",
			state: &collector.PlatformState{
				CollectedAt: now,
				Projects: []accord.AtlasProjectDTO{
					{ID: "forge", Status: "verified", DependsOn: []string{"atlas"}},
					{ID: "atlas", Status: "unverified"},
				},
			},
			wantCount: 1,
		},
		{
			name: "all dependencies verified — no insight",
			state: &collector.PlatformState{
				CollectedAt: now,
				Projects: []accord.AtlasProjectDTO{
					{ID: "forge", Status: "verified", DependsOn: []string{"atlas"}},
					{ID: "atlas", Status: "verified"},
				},
			},
			wantCount: 0,
		},
		{
			name: "same unverified dep referenced by two projects — one insight",
			state: &collector.PlatformState{
				CollectedAt: now,
				Projects: []accord.AtlasProjectDTO{
					{ID: "forge", Status: "verified", DependsOn: []string{"atlas"}},
					{ID: "sentinel", Status: "verified", DependsOn: []string{"atlas"}},
					{ID: "atlas", Status: "unverified"},
				},
			},
			wantCount: 1, // deduped by dep ID
		},
		{
			name: "no projects — no insight",
			state: emptyState(),
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := engine.Analyze(tt.state)
			assertRule(t, report.Insights, RuleDependencyRisk, tt.wantCount)
		})
	}
}

// ── S-004: STALE PROJECT ──────────────────────────────────────────────────────

func TestS004_StaleProject(t *testing.T) {
	tests := []struct {
		name      string
		state     *collector.PlatformState
		wantCount int
	}{
		{
			name: "verified project with no events — stale",
			state: &collector.PlatformState{
				CollectedAt: now,
				Projects: []accord.AtlasProjectDTO{
					{ID: "navigator", Status: "verified"},
				},
				Events: []accord.EventDTO{
					{Type: "SERVICE_STARTED", ServiceID: "nexus", CreatedAt: now.Add(-1 * time.Minute).Format(time.RFC3339Nano)},
				},
			},
			wantCount: 1,
		},
		{
			name: "verified project with matching event — not stale",
			state: &collector.PlatformState{
				CollectedAt: now,
				Projects: []accord.AtlasProjectDTO{
					{ID: "nexus", Status: "verified"},
				},
				Events: []accord.EventDTO{
					{Type: "SERVICE_STARTED", ServiceID: "nexus", CreatedAt: now.Add(-1 * time.Minute).Format(time.RFC3339Nano)},
				},
			},
			wantCount: 0,
		},
		{
			name: "unverified project — skipped by rule",
			state: &collector.PlatformState{
				CollectedAt: now,
				Projects: []accord.AtlasProjectDTO{
					{ID: "unknown", Status: "unverified"},
				},
				Events: []accord.EventDTO{
					{Type: "SERVICE_STARTED", ServiceID: "nexus", CreatedAt: now.Add(-1 * time.Minute).Format(time.RFC3339Nano)},
				},
			},
			wantCount: 0,
		},
		{
			name: "no events at all — rule skips (avoids false positives on fresh platform)",
			state: &collector.PlatformState{
				CollectedAt: now,
				Projects: []accord.AtlasProjectDTO{
					{ID: "navigator", Status: "verified"},
				},
			},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := engine.Analyze(tt.state)
			assertRule(t, report.Insights, RuleStaleProject, tt.wantCount)
		})
	}
}

// ── S-005: HIGH DENIAL RATE ───────────────────────────────────────────────────

func TestS005_HighDenialRate(t *testing.T) {
	tests := []struct {
		name      string
		state     *collector.PlatformState
		wantCount int
	}{
		{
			name: "G-001 finding present — fires",
			state: &collector.PlatformState{
				CollectedAt: now,
				Findings: []accord.GuardianFindingDTO{
					{RuleID: "G-001", Target: "mystery", Count: 5},
				},
			},
			wantCount: 1,
		},
		{
			name: "two different G-001 targets — two insights",
			state: &collector.PlatformState{
				CollectedAt: now,
				Findings: []accord.GuardianFindingDTO{
					{RuleID: "G-001", Target: "mystery", Count: 3},
					{RuleID: "G-001", Target: "unknown", Count: 2},
				},
			},
			wantCount: 2,
		},
		{
			name: "non-G-001 finding — no insight",
			state: &collector.PlatformState{
				CollectedAt: now,
				Findings: []accord.GuardianFindingDTO{
					{RuleID: "G-003", Target: "forge", Count: 5},
				},
			},
			wantCount: 0,
		},
		{
			name: "no findings — no insight",
			state: emptyState(),
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := engine.Analyze(tt.state)
			assertRule(t, report.Insights, RuleHighDenialRate, tt.wantCount)
		})
	}
}

// ── S-006: SERVICE MAINTENANCE ───────────────────────────────────────────────

func TestS006_ServiceMaintenance(t *testing.T) {
	tests := []struct {
		name      string
		state     *collector.PlatformState
		wantCount int
	}{
		{
			name: "service desired=running actual=maintenance — fires",
			state: &collector.PlatformState{
				CollectedAt: now,
				Services: []accord.ServiceDTO{
					{ID: "atlas-daemon", Name: "atlas", Project: "atlas",
						DesiredState: "running", ActualState: "maintenance", FailCount: 3},
				},
			},
			wantCount: 1,
		},
		{
			name: "service desired=running actual=running — no insight",
			state: &collector.PlatformState{
				CollectedAt: now,
				Services: []accord.ServiceDTO{
					{ID: "atlas-daemon", Name: "atlas", Project: "atlas",
						DesiredState: "running", ActualState: "running"},
				},
			},
			wantCount: 0,
		},
		{
			name: "service desired=stopped actual=maintenance — no insight",
			state: &collector.PlatformState{
				CollectedAt: now,
				Services: []accord.ServiceDTO{
					{ID: "atlas-daemon", Name: "atlas", Project: "atlas",
						DesiredState: "stopped", ActualState: "maintenance"},
				},
			},
			wantCount: 0,
		},
		{
			name: "two services in maintenance — two insights",
			state: &collector.PlatformState{
				CollectedAt: now,
				Services: []accord.ServiceDTO{
					{ID: "atlas-daemon", Name: "atlas", Project: "atlas",
						DesiredState: "running", ActualState: "maintenance"},
					{ID: "forge-daemon", Name: "forge", Project: "forge",
						DesiredState: "running", ActualState: "maintenance"},
				},
			},
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := engine.Analyze(tt.state)
			assertRule(t, report.Insights, RuleServiceMaintenance, tt.wantCount)
		})
	}
}

// ── S-007: BUILD FAILURE RATE ─────────────────────────────────────────────────

func TestS007_BuildFailureRate(t *testing.T) {
	tests := []struct {
		name      string
		state     *collector.PlatformState
		wantCount int
	}{
		{
			name: "G-003 finding present — fires",
			state: &collector.PlatformState{
				CollectedAt: now,
				Findings: []accord.GuardianFindingDTO{
					{RuleID: "G-003", Target: "forge", Message: ">50% failure rate", Count: 8},
				},
			},
			wantCount: 1,
		},
		{
			name: "G-001 finding only — no insight",
			state: &collector.PlatformState{
				CollectedAt: now,
				Findings: []accord.GuardianFindingDTO{
					{RuleID: "G-001", Target: "forge", Count: 4},
				},
			},
			wantCount: 0,
		},
		{
			name: "no findings — no insight",
			state: emptyState(),
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := engine.Analyze(tt.state)
			assertRule(t, report.Insights, RuleBuildFailureRate, tt.wantCount)
		})
	}
}

// ── S-008: AGENT DISCONNECTED ─────────────────────────────────────────────────

func TestS008_AgentDisconnected(t *testing.T) {
	tests := []struct {
		name      string
		state     *collector.PlatformState
		wantCount int
	}{
		{
			name: "all agents offline + services desired running — fires",
			state: &collector.PlatformState{
				CollectedAt: now,
				Agents: []accord.AgentDTO{
					{ID: "local", Online: false},
				},
				Services: []accord.ServiceDTO{
					{ID: "atlas-daemon", DesiredState: "running", ActualState: "stopped"},
				},
			},
			wantCount: 1,
		},
		{
			name: "one agent online — no insight",
			state: &collector.PlatformState{
				CollectedAt: now,
				Agents: []accord.AgentDTO{
					{ID: "local", Online: true},
					{ID: "remote", Online: false},
				},
				Services: []accord.ServiceDTO{
					{ID: "atlas-daemon", DesiredState: "running"},
				},
			},
			wantCount: 0,
		},
		{
			name: "all agents offline but no services desired running — no insight",
			state: &collector.PlatformState{
				CollectedAt: now,
				Agents: []accord.AgentDTO{
					{ID: "local", Online: false},
				},
				Services: []accord.ServiceDTO{
					{ID: "atlas-daemon", DesiredState: "stopped"},
				},
			},
			wantCount: 0,
		},
		{
			name: "no agents registered — no insight (rule skips)",
			state: &collector.PlatformState{
				CollectedAt: now,
				Services: []accord.ServiceDTO{
					{ID: "atlas-daemon", DesiredState: "running"},
				},
			},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := engine.Analyze(tt.state)
			assertRule(t, report.Insights, RuleAgentDisconnected, tt.wantCount)
		})
	}
}

// ── HEALTH CLASSIFICATION ─────────────────────────────────────────────────────

func TestClassifyHealth(t *testing.T) {
	tests := []struct {
		name       string
		state      *collector.PlatformState
		wantHealth string
	}{
		{
			name:       "no insights — healthy",
			state:      emptyState(),
			wantHealth: HealthHealthy,
		},
		{
			name: "S-004 info only — healthy",
			state: &collector.PlatformState{
				CollectedAt: now,
				Projects: []accord.AtlasProjectDTO{
					{ID: "navigator", Status: "verified"},
				},
				Events: []accord.EventDTO{
					{Type: "SERVICE_STARTED", ServiceID: "nexus", CreatedAt: now.Add(-1 * time.Minute).Format(time.RFC3339Nano)},
				},
			},
			wantHealth: HealthHealthy,
		},
		{
			name: "S-006 warning — degraded",
			state: &collector.PlatformState{
				CollectedAt: now,
				Services: []accord.ServiceDTO{
					{ID: "atlas-daemon", Name: "atlas", Project: "atlas",
						DesiredState: "running", ActualState: "maintenance"},
				},
			},
			wantHealth: HealthDegraded,
		},
		{
			name: "S-001 error — incident",
			state: &collector.PlatformState{
				CollectedAt: now,
				Events: []accord.EventDTO{
					crashEvent("atlas", now.Add(-2*time.Minute)),
				},
				Projects: []accord.AtlasProjectDTO{
					{ID: "forge", DependsOn: []string{"atlas"}},
				},
			},
			wantHealth: HealthIncident,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := engine.Analyze(tt.state)
			if report.Health != tt.wantHealth {
				t.Errorf("health = %q, want %q (insights: %d)", report.Health, tt.wantHealth, len(report.Insights))
			}
		})
	}
}

// ── DEPLOY RISK ───────────────────────────────────────────────────────────────

func TestDeployRisk(t *testing.T) {
	tests := []struct {
		name     string
		state    *collector.PlatformState
		wantRisk string
	}{
		{
			name:     "clean platform — low risk",
			state:    emptyState(),
			wantRisk: "low",
		},
		{
			name: "unverified dependency — medium risk",
			state: &collector.PlatformState{
				CollectedAt: now,
				Projects: []accord.AtlasProjectDTO{
					{ID: "forge", Status: "verified", DependsOn: []string{"atlas"}},
					{ID: "atlas", Status: "unverified"},
				},
			},
			wantRisk: "medium",
		},
		{
			name: "active crash cascade — high risk",
			state: &collector.PlatformState{
				CollectedAt: now,
				Events: []accord.EventDTO{
					crashEvent("atlas", now.Add(-2*time.Minute)),
				},
				Projects: []accord.AtlasProjectDTO{
					{ID: "forge", DependsOn: []string{"atlas"}},
				},
			},
			wantRisk: "high",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			risk := engine.DeployRisk(tt.state)
			if risk.Risk != tt.wantRisk {
				t.Errorf("deploy risk = %q, want %q", risk.Risk, tt.wantRisk)
			}
		})
	}
}

// ── CLEAN STATE ───────────────────────────────────────────────────────────────

func TestAnalyze_EmptyState_NoInsights(t *testing.T) {
	report := engine.Analyze(emptyState())
	assertNoInsights(t, report)
	if report.Health != HealthHealthy {
		t.Errorf("empty state health = %q, want %q", report.Health, HealthHealthy)
	}
	if report.Insights == nil {
		t.Error("Insights should be empty slice, not nil")
	}
}
