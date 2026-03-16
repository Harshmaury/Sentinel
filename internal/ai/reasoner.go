// @sentinel-project: sentinel
// @sentinel-path: internal/ai/reasoner.go
// Package ai provides the Sentinel AI reasoning layer (ADR-018).
//
// The Reasoner takes a Phase 1 SystemReport and calls the Anthropic
// API to produce human-readable narrative reasoning. It never queries
// raw platform data — it only reads the structured Phase 1 output.
//
// Graceful degradation: if the API key is absent or the call fails,
// the Reasoner returns an empty string and ai_available=false.
// The service never fails hard on LLM errors.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Harshmaury/Sentinel/internal/insight"
)

const (
	anthropicAPI    = "https://api.anthropic.com/v1/messages"
	anthropicModel  = "claude-sonnet-4-6"
	anthropicVersion = "2023-06-01"
	maxTokens       = 400
	requestTimeout  = 20 * time.Second
)

// systemPrompt defines the AI's role as a platform diagnostician.
const systemPrompt = `You are a senior developer platform diagnostician.
You are given a structured JSON report of findings from an automated
platform monitoring system. The platform consists of services built
in Go running on a local developer workstation: Nexus (control plane),
Atlas (workspace knowledge), and Forge (execution engine).

Your task is to interpret these findings and explain in plain English:
1. What is happening on the platform right now
2. Why it matters
3. What the developer should investigate first

Constraints:
- Never suggest starting or stopping services directly
- Keep your response under 300 words
- Write in plain prose — no markdown headers or bullet lists
- If the platform is healthy with no findings, say so briefly
- Focus on the most important finding if multiple exist`

// Reasoner calls the Anthropic API to produce narrative reasoning.
type Reasoner struct {
	apiKey     string
	httpClient *http.Client
}

// NewReasoner creates a Reasoner. If apiKey is empty, AI is disabled.
func NewReasoner(apiKey string) *Reasoner {
	return &Reasoner{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: requestTimeout},
	}
}

// Available returns true if the AI layer is configured.
func (r *Reasoner) Available() bool {
	return r.apiKey != ""
}

// ExplainReport calls the LLM with the Phase 1 report and returns narrative reasoning.
// Returns ("", false, nil) if AI is disabled. Returns ("", false, err) on API failure.
func (r *Reasoner) ExplainReport(ctx context.Context, report *insight.SystemReport) (string, bool, error) {
	if !r.Available() {
		return "", false, nil
	}

	reportJSON, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", false, fmt.Errorf("marshal report: %w", err)
	}

	userContent := fmt.Sprintf("Platform monitoring report:\n\n```json\n%s\n```\n\nPlease explain what this means and what to investigate.", string(reportJSON))

	reqBody, err := json.Marshal(map[string]any{
		"model":      anthropicModel,
		"max_tokens": maxTokens,
		"system":     systemPrompt,
		"messages": []map[string]string{
			{"role": "user", "content": userContent},
		},
	})
	if err != nil {
		return "", false, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicAPI, bytes.NewReader(reqBody))
	if err != nil {
		return "", false, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", r.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("anthropic API call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("anthropic API: HTTP %d", resp.StatusCode)
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", false, fmt.Errorf("decode response: %w", err)
	}

	for _, block := range result.Content {
		if block.Type == "text" && block.Text != "" {
			return block.Text, true, nil
		}
	}
	return "", false, fmt.Errorf("no text content in response")
}
