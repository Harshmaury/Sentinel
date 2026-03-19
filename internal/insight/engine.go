// @sentinel-project: sentinel
// @sentinel-path: internal/insight/engine.go
// Engine evaluates Sentinel correlation rules against PlatformState.
// All rules are deterministic — no LLM calls in Phase 1 (ADR-017).
package insight

import (
	"fmt"
	"time"

	"github.com/Harshmaury/Sentinel/internal/collector"
)

// Engine evaluates correlation rules and produces insights.
type Engine struct{}

// NewEngine creates an Engine.
func NewEngine() *Engine { return &Engine{} }

// Analyze runs all rules and returns a SystemReport.
func (e *Engine) Analyze(state *collector.PlatformState) *SystemReport {
	var insights []*Insight

	insights = append(insights, e.ruleCascadeDetection(state)...)
	insights = append(insights, e.ruleDeployCorrelation(state)...)
	insights = append(insights, e.ruleDependencyRisk(state)...)
	insights = append(insights, e.ruleStaleProject(state)...)
	insights = append(insights, e.ruleHighDenialRate(state)...)
	insights = append(insights, e.ruleServiceMaintenance(state)...)
	insights = append(insights, e.ruleBuildFailureRate(state)...)
	insights = append(insights, e.ruleAgentDisconnected(state)...)

	health := classifyHealth(insights)
	summary := buildSummary(insights)

	if insights == nil {
		insights = []*Insight{}
	}
	return &SystemReport{
		Health:      health,
		Summary:     summary,
		Insights:    insights,
		CollectedAt: state.CollectedAt,
	}
}

// DeployRisk assesses deployment risk from current platform state.
func (e *Engine) DeployRisk(state *collector.PlatformState) *DeployRisk {
	var factors []*Insight

	// Inherit relevant insights as risk factors.
	all := e.Analyze(state)
	for _, ins := range all.Insights {
		switch ins.RuleID {
		case RuleCascadeDetection, RuleDeployCorrelation, RuleDependencyRisk:
			factors = append(factors, ins)
		}
	}

	risk := "low"
	if len(factors) > 0 {
		risk = "medium"
	}
	for _, f := range factors {
		if f.Severity == SeverityError {
			risk = "high"
			break
		}
	}

	if factors == nil {
		factors = []*Insight{}
	}
	return &DeployRisk{Risk: risk, Factors: factors}
}

// ── RULE IMPLEMENTATIONS ──────────────────────────────────────────────────────

// S-001: detect crash events and identify dependents at risk.
func (e *Engine) ruleCascadeDetection(state *collector.PlatformState) []*Insight {
	cutoff := time.Now().UTC().Add(-15 * time.Minute)
	crashed := map[string]bool{}

	for _, ev := range state.Events {
		if ev.Type == "SERVICE_CRASHED" && ev.CreatedAt.After(cutoff) {
			crashed[ev.ServiceID] = true
		}
	}
	if len(crashed) == 0 {
		return nil
	}

	// Build reverse dependency map from Atlas projects.
	dependents := map[string][]string{}
	for _, p := range state.Projects {
		for _, dep := range p.DependsOn {
			dependents[dep] = append(dependents[dep], p.ID)
		}
	}

	var insights []*Insight
	for svcID := range crashed {
		deps := dependents[svcID]
		if len(deps) == 0 {
			continue
		}
		insights = append(insights, &Insight{
			RuleID:   RuleCascadeDetection,
			Severity: SeverityError,
			Title:    fmt.Sprintf("Crash cascade risk: %s", svcID),
			Detail:   fmt.Sprintf("%s crashed in the last 15 min — dependents %v may be affected", svcID, deps),
			Subjects: append([]string{svcID}, deps...),
			At:       time.Now().UTC(),
		})
	}
	return insights
}

// S-002: correlate Forge deployments with Nexus crash clusters.
func (e *Engine) ruleDeployCorrelation(state *collector.PlatformState) []*Insight {
	const windowMins = 20
	now := time.Now().UTC()
	cutoff := now.Add(-time.Duration(windowMins) * time.Minute)

	// Find recent crash events.
	var firstCrash time.Time
	crashCount := 0
	for _, ev := range state.Events {
		if ev.Type == "SERVICE_CRASHED" && ev.CreatedAt.After(cutoff) {
			crashCount++
			if firstCrash.IsZero() || ev.CreatedAt.Before(firstCrash) {
				firstCrash = ev.CreatedAt
			}
		}
	}
	if crashCount == 0 {
		return nil
	}

	// Find a deploy execution that preceded the first crash.
	for _, ex := range state.Executions {
		if ex.Intent != "deploy" {
			continue
		}
		if ex.StartedAt.After(cutoff) && ex.StartedAt.Before(firstCrash) {
			elapsed := firstCrash.Sub(ex.StartedAt).Round(time.Second)
			return []*Insight{{
				RuleID:   RuleDeployCorrelation,
				Severity: SeverityError,
				Title:    fmt.Sprintf("Deploy of %q may have caused crash cluster", ex.Target),
				Detail:   fmt.Sprintf("Deploy of %s ran at %s — %d crash(es) began %s later", ex.Target, ex.StartedAt.Format(time.RFC3339), crashCount, elapsed),
				Subjects: []string{ex.Target},
				At:       now,
			}}
		}
	}
	return nil
}

// S-003: flag unverified projects in critical dependency paths.
func (e *Engine) ruleDependencyRisk(state *collector.PlatformState) []*Insight {
	unverified := map[string]bool{}
	for _, p := range state.Projects {
		if p.Status == "unverified" {
			unverified[p.ID] = true
		}
	}

	var insights []*Insight
	seen := map[string]bool{}
	for _, p := range state.Projects {
		for _, dep := range p.DependsOn {
			if unverified[dep] && !seen[dep] {
				seen[dep] = true
				insights = append(insights, &Insight{
					RuleID:   RuleDependencyRisk,
					Severity: SeverityWarning,
					Title:    fmt.Sprintf("Unverified dependency: %s", dep),
					Detail:   fmt.Sprintf("%s depends on %s which has no nexus.yaml — add descriptor", p.ID, dep),
					Subjects: []string{p.ID, dep},
					At:       time.Now().UTC(),
				})
			}
		}
	}
	return insights
}

// S-004: flag projects with no recent Nexus event activity.
func (e *Engine) ruleStaleProject(state *collector.PlatformState) []*Insight {
	const staleHours = 24
	active := map[string]bool{}
	for _, ev := range state.Events {
		if ev.ServiceID != "" && ev.ServiceID != "system" && ev.ServiceID != "drop" {
			active[ev.ServiceID] = true
		}
	}

	var insights []*Insight
	for _, p := range state.Projects {
		if p.Status != "verified" {
			continue
		}
		if !active[p.ID] && len(state.Events) > 0 {
			insights = append(insights, &Insight{
				RuleID:   RuleStaleProject,
				Severity: SeverityInfo,
				Title:    fmt.Sprintf("No platform activity: %s", p.ID),
				Detail:   fmt.Sprintf("Verified project %s has no Nexus events in the observed window (%d h+)", p.ID, staleHours),
				Subjects: []string{p.ID},
				At:       time.Now().UTC(),
			})
		}
	}
	return insights
}

// S-005: correlate Guardian denial findings with Forge execution patterns.
func (e *Engine) ruleHighDenialRate(state *collector.PlatformState) []*Insight {
	denialTargets := map[string]int{}
	for _, f := range state.Findings {
		if f.RuleID == "G-001" {
			denialTargets[f.Target] += f.Count
		}
	}

	var insights []*Insight
	for target, count := range denialTargets {
		insights = append(insights, &Insight{
			RuleID:   RuleHighDenialRate,
			Severity: SeverityWarning,
			Title:    fmt.Sprintf("Repeated denials for %s", target),
			Detail:   fmt.Sprintf("%s has been denied %d time(s) — project likely missing nexus.yaml or unregistered", target, count),
			Subjects: []string{target},
			At:       time.Now().UTC(),
		})
	}
	return insights
}

// S-006: detect services stuck in maintenance (desired=running, actual=maintenance).
func (e *Engine) ruleServiceMaintenance(state *collector.PlatformState) []*Insight {
	var insights []*Insight
	for _, svc := range state.Services {
		if svc.DesiredState == "running" && svc.ActualState == "maintenance" {
			insights = append(insights, &Insight{
				RuleID:   RuleServiceMaintenance,
				Severity: SeverityWarning,
				Title:    fmt.Sprintf("Service in maintenance: %s", svc.Name),
				Detail:   fmt.Sprintf("%s is desired=running but stuck in maintenance after %d failure(s) — run: engx services reset %s", svc.Name, svc.FailCount, svc.Name),
				Subjects: []string{svc.Project},
				At:       time.Now().UTC(),
			})
		}
	}
	return insights
}

// S-007: sustained build failure rate from Guardian G-003 findings.
func (e *Engine) ruleBuildFailureRate(state *collector.PlatformState) []*Insight {
	var insights []*Insight
	for _, f := range state.Findings {
		if f.RuleID == "G-003" {
			insights = append(insights, &Insight{
				RuleID:   RuleBuildFailureRate,
				Severity: SeverityError,
				Title:    fmt.Sprintf("High build failure rate: %s", f.Target),
				Detail:   fmt.Sprintf("Guardian G-003: %s — check build logs: engx logs %s-daemon", f.Message, f.Target),
				Subjects: []string{f.Target},
				At:       time.Now().UTC(),
			})
		}
	}
	return insights
}

// S-008: engxa offline while services are desired-running.
func (e *Engine) ruleAgentDisconnected(state *collector.PlatformState) []*Insight {
	if len(state.Agents) == 0 {
		return nil
	}
	anyOnline := false
	for _, a := range state.Agents {
		if a.Online {
			anyOnline = true
		}
	}
	if anyOnline {
		return nil
	}
	desiredRunning := 0
	for _, svc := range state.Services {
		if svc.DesiredState == "running" {
			desiredRunning++
		}
	}
	if desiredRunning == 0 {
		return nil
	}
	return []*Insight{{
		RuleID:   RuleAgentDisconnected,
		Severity: SeverityError,
		Title:    "engxa agent offline",
		Detail:   fmt.Sprintf("All agents offline — %d service(s) desired=running will not start until engxa reconnects", desiredRunning),
		Subjects: []string{"engxa"},
		At:       time.Now().UTC(),
	}}
}

// ── HELPERS ───────────────────────────────────────────────────────────────────

func classifyHealth(insights []*Insight) string {
	for _, ins := range insights {
		switch ins.RuleID {
		case RuleCascadeDetection, RuleDeployCorrelation,
			RuleBuildFailureRate, RuleAgentDisconnected:
			if ins.Severity == SeverityError {
				return HealthIncident
			}
		}
	}
	for _, ins := range insights {
		if ins.Severity == SeverityWarning || ins.Severity == SeverityError {
			return HealthDegraded
		}
	}
	return HealthHealthy
}

func buildSummary(insights []*Insight) string {
	if len(insights) == 0 {
		return "all platform signals normal"
	}
	errors, warnings, infos := 0, 0, 0
	for _, ins := range insights {
		switch ins.Severity {
		case SeverityError:
			errors++
		case SeverityWarning:
			warnings++
		default:
			infos++
		}
	}
	return fmt.Sprintf("%d error(s), %d warning(s), %d info finding(s)", errors, warnings, infos)
}
