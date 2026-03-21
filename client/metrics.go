// @herald-project: Herald
// @herald-path: client/metrics.go
// NexusMetricsClient provides typed access to the Nexus /metrics endpoint.
// ADR-039: closes the last raw HTTP gap in metrics/collector/nexus.go.
package client

import (
	"context"
	"fmt"

	accord "github.com/Harshmaury/Accord/api"
)

// NexusMetricsClient provides typed access to the Nexus /metrics API.
type NexusMetricsClient struct{ c *Client }

// Get returns the current Nexus runtime counter snapshot.
func (m *NexusMetricsClient) Get(ctx context.Context) (*accord.NexusMetricsDTO, error) {
	var out accord.NexusMetricsDTO
	if err := m.c.get(ctx, "/metrics", &out); err != nil {
		return nil, fmt.Errorf("nexus.metrics: %w", err)
	}
	return &out, nil
}
