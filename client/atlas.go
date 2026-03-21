// @herald-project: Herald
// @herald-path: client/atlas.go
// AtlasClient provides typed access to the Atlas workspace API.
// ADR-039: all Atlas calls go through Herald — no raw http.Get in observers.
package client

import (
	"context"
	"fmt"

	accord "github.com/Harshmaury/Accord/api"
)

// AtlasClient provides typed access to the Atlas /workspace API.
type AtlasClient struct{ c *Client }

// Projects returns all projects from Atlas GET /workspace/projects.
func (a *AtlasClient) Projects(ctx context.Context) ([]accord.AtlasProjectDTO, error) {
	var out []accord.AtlasProjectDTO
	if err := a.c.get(ctx, "/workspace/projects", &out); err != nil {
		return nil, fmt.Errorf("atlas.projects: %w", err)
	}
	return out, nil
}

// Graph returns all workspace graph edges from Atlas GET /workspace/graph.
func (a *AtlasClient) Graph(ctx context.Context) (*accord.AtlasGraphDTO, error) {
	var out accord.AtlasGraphDTO
	if err := a.c.get(ctx, "/workspace/graph", &out); err != nil {
		return nil, fmt.Errorf("atlas.graph: %w", err)
	}
	return &out, nil
}
