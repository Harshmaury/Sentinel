// @sentinel-project: sentinel
// @sentinel-path: internal/ai/reasoner.go
// Reasoner calls the Anthropic API to generate plain-prose narrative
// reasoning on top of a Sentinel Phase 1 SystemReport (ADR-018).
//
// Rules (ADR-018):
//   - Called ONLY on explicit GET /insights/explain — never on polling cycles
//   - Input: Phase 1 SystemReport JSON only — never raw events or graph data
//   - Output: plain prose ≤ 300 words, no markdown headers
//   - Never suggests start/stop actions
//   - Degrades gracefully: empty key or API error → aiAvailable=false, no crash
//   - Timeout: 25s enforced by caller context (explain.go)
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Harshmaury/Sentinel/internal/insight"
)

const (
	anthropicEndpoint    = "https://api.anthropic.com/v1/messages"
	anthropicModel       = "claude-sonnet-4-6"
	anthropicVersionVal  = "2023-06-01"
	anthropicVersionKey  = "anthropic-version"
	anthropicAPIKeyHeader = "x-api-key"
	maxOutputTokens      = 512 // ≤300 words output; 512 tokens is a safe ceiling
)

// systemPrompt defines the AI's role per ADR-018 § 6.
const systemPrompt = `You are a senior platform diagnostician with full knowledge of this developer platform's architecture.

You will receive a structured JSON report of platform findings from an automated analysis system.
Your task is to interpret these findings and explain them in plain English.

Rules:
- Respond in plain prose only — no markdown headers, no bullet points, no lists
- Maximum 300 words
- Explain what is happening, why it matters, and what to investigate first
- Never suggest starting or stopping services — you are an observer, not a controller
- If there are no findings, say the platform looks healthy and briefly describe normal state
- Be direct and technical — the reader is a developer who knows this system`

// Reasoner calls the Anthropic API to produce narrative reasoning.
// Safe to create with an empty API key — ExplainReport returns aiAvailable=false immediately.
type Reasoner struct {
	apiKey     string
	httpClient *http.Client
}

// NewReasoner creates a Reasoner. If apiKey is empty, the Reasoner is disabled —
// ExplainReport returns ("", false, nil) without making any network call.
func NewReasoner(apiKey string) *Reasoner {
	return &Reasoner{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// ExplainReport calls the Anthropic API with the Phase 1 report and returns
// a plain-prose narrative explanation.
//
// Returns reasoning="", aiAvailable=false on empty key, unreachable API, or any
// error — the service never fails hard on LLM problems (ADR-018 § 4).
func (r *Reasoner) ExplainReport(ctx context.Context, report *insight.SystemReport) (string, bool, error) {
	if r.apiKey == "" {
		return "", false, nil
	}

	reportJSON, err := json.Marshal(report)
	if err != nil {
		return "", false, fmt.Errorf("marshal report: %w", err)
	}

	text, err := r.callAPI(ctx, string(reportJSON))
	if err != nil {
		// Degrade gracefully — API errors are not fatal per ADR-018 § 4.
		return "", false, nil //nolint:nilerr
	}

	return text, true, nil
}

// ── ANTHROPIC API ─────────────────────────────────────────────────────────────

// anthropicRequest is the POST /v1/messages request body.
type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system"`
	Messages  []anthropicMessage `json:"messages"`
}

// anthropicMessage is one conversation turn.
type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicResponse is the /v1/messages response envelope.
type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// callAPI sends one request to Anthropic and returns the first text block.
// Split into buildRequest + doRequest + parseResponse to stay under 40 lines.
func (r *Reasoner) callAPI(ctx context.Context, userContent string) (string, error) {
	req, err := r.buildRequest(ctx, userContent)
	if err != nil {
		return "", err
	}

	raw, err := r.doRequest(req)
	if err != nil {
		return "", err
	}

	return parseResponse(raw)
}

// buildRequest constructs the authenticated HTTP request.
func (r *Reasoner) buildRequest(ctx context.Context, userContent string) (*http.Request, error) {
	payload := anthropicRequest{
		Model:     anthropicModel,
		MaxTokens: maxOutputTokens,
		System:    systemPrompt,
		Messages:  []anthropicMessage{{Role: "user", Content: userContent}},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(anthropicAPIKeyHeader, r.apiKey)
	req.Header.Set(anthropicVersionKey, anthropicVersionVal)

	return req, nil
}

// doRequest executes the HTTP call and returns the raw response body.
func (r *Reasoner) doRequest(req *http.Request) ([]byte, error) {
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, truncate(raw, 200))
	}

	return raw, nil
}

// parseResponse extracts the first text block from the Anthropic response.
func parseResponse(raw []byte) (string, error) {
	var result anthropicResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}

	if result.Error != nil {
		return "", fmt.Errorf("api error %s: %s", result.Error.Type, result.Error.Message)
	}

	for _, block := range result.Content {
		if block.Type == "text" && block.Text != "" {
			return block.Text, nil
		}
	}

	return "", fmt.Errorf("no text in response")
}

// truncate returns at most n bytes of b as a string — for safe error messages.
func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
