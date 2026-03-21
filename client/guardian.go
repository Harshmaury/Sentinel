// @herald-project: Herald
// @herald-path: client/guardian.go
// GuardianClient provides typed access to the Guardian findings API.
// ADR-039: all Guardian calls go through Herald — no raw http.Get in observers.
package client

import (
	"context"
	"fmt"

	accord "github.com/Harshmaury/Accord/api"
)

// GuardianClient provides typed access to the Guardian /guardian API.
type GuardianClient struct{ c *Client }

// Findings returns the current findings report from Guardian GET /guardian/findings.
func (g *GuardianClient) Findings(ctx context.Context) (*accord.GuardianReportDTO, error) {
	var out accord.GuardianReportDTO
	if err := g.c.get(ctx, "/guardian/findings", &out); err != nil {
		return nil, fmt.Errorf("guardian.findings: %w", err)
	}
	return &out, nil
}
