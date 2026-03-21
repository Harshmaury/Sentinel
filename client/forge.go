// @herald-project: Herald
// @herald-path: client/forge.go
// ForgeClient provides typed access to the Forge execution history API.
// ADR-039: all Forge calls go through Herald — no raw http.Get in observers.
package client

import (
	"context"
	"fmt"
	"strconv"

	accord "github.com/Harshmaury/Accord/api"
)

// ForgeClient provides typed access to the Forge /history API.
type ForgeClient struct{ c *Client }

// History returns recent execution records from Forge GET /history.
func (f *ForgeClient) History(ctx context.Context, limit int) ([]accord.ForgeExecutionDTO, error) {
	var out []accord.ForgeExecutionDTO
	path := "/history?limit=" + strconv.Itoa(limit)
	if err := f.c.get(ctx, path, &out); err != nil {
		return nil, fmt.Errorf("forge.history: %w", err)
	}
	return out, nil
}

// ByTrace returns all execution records for a trace ID from Forge GET /history/:trace_id.
func (f *ForgeClient) ByTrace(ctx context.Context, traceID string) ([]accord.ForgeExecutionDTO, error) {
	var out []accord.ForgeExecutionDTO
	if err := f.c.get(ctx, "/history/"+traceID, &out); err != nil {
		return nil, fmt.Errorf("forge.history.bytrace %s: %w", traceID, err)
	}
	return out, nil
}
