// Package llm provides a thin client that talks to either a local Ollama
// instance or any OpenAI-compatible endpoint (e.g. https://llm.eidonix.com/v1).
// The provider is chosen by inspecting the configured URL + API key:
//
//   - When an API key is present, the OpenAI chat-completions API is used.
//   - Otherwise the Ollama /api/generate API is used (no auth).
//
// Both paths return a plain-text completion so callers don't care which backend
// answered.
package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Provider is auto-detected from the presence of an API key.
type Provider int

const (
	ProviderOllama Provider = iota
	ProviderOpenAI
)

// Config holds the endpoint, model and (for OpenAI-compatible backends) key.
type Config struct {
	URL    string // base URL, e.g. http://localhost:11434 or https://llm.eidonix.com/v1
	Model  string // model id, e.g. llama3 or qwen3.5-9b
	APIKey string // non-empty selects the OpenAI-compatible path
}

func (c Config) provider() Provider {
	if strings.TrimSpace(c.APIKey) != "" {
		return ProviderOpenAI
	}
	return ProviderOllama
}

// ConfigFromEnv builds a Config from the standard env variables with sensible
// defaults so callers don't repeat themselves.
func ConfigFromEnv(urlKey, modelKey, defaultURL, defaultModel string, getenv func(string) string) Config {
	v := getenv(urlKey)
	if v == "" {
		v = defaultURL
	}
	m := getenv(modelKey)
	if m == "" {
		m = defaultModel
	}
	return Config{URL: v, Model: m, APIKey: getenv("LLM_API_KEY")}
}

// Complete sends prompt to the configured backend and returns the text result.
// timeout caps the whole request. On any failure it returns a non-nil error.
func Complete(cfg Config, prompt string, timeout time.Duration) (string, error) {
	if cfg.provider() == ProviderOpenAI {
		return completeOpenAI(cfg, prompt, timeout)
	}
	return completeOllama(cfg, prompt, timeout)
}

// completeOllama calls POST {url}/api/generate with the legacy Ollama envelope.
func completeOllama(cfg Config, prompt string, timeout time.Duration) (string, error) {
	endpoint := strings.TrimRight(cfg.URL, "/")
	if endpoint == "" {
		endpoint = "http://localhost:11434"
	}
	model := cfg.Model
	if model == "" {
		model = "llama3"
	}
	body, err := json.Marshal(map[string]any{"model": model, "prompt": prompt, "stream": false})
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Post(endpoint+"/api/generate", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read ollama response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("ollama status %d: %s", resp.StatusCode, trunc(string(raw), 200))
	}
	var env struct {
		Response string `json:"response"`
		Error    string `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", fmt.Errorf("parse ollama envelope: %w", err)
	}
	if env.Error != "" {
		return "", fmt.Errorf("ollama: %s", env.Error)
	}
	out := strings.TrimSpace(env.Response)
	if out == "" {
		return "", fmt.Errorf("ollama returned empty response")
	}
	return out, nil
}

// OpenAI chat-completions request/response shapes.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type chatChoice struct {
	Message chatMessage `json:"message"`
}
type chatResponse struct {
	Choices []chatChoice `json:"choices"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// completeOpenAI calls POST {url}/chat/completions with Bearer auth.
func completeOpenAI(cfg Config, prompt string, timeout time.Duration) (string, error) {
	endpoint := strings.TrimRight(cfg.URL, "/")
	// A bare host (no /v1) is treated as {host}/v1.
	if !strings.HasSuffix(endpoint, "/v1") {
		endpoint += "/v1"
	}
	model := cfg.Model
	if model == "" {
		model = "gpt-3.5-turbo"
	}
	body, err := json.Marshal(chatRequest{
		Model:    model,
		Messages: []chatMessage{{Role: "user", Content: prompt}},
		Stream:   false,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.APIKey))
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai-compatible request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("openai-compatible status %d: %s", resp.StatusCode, trunc(string(raw), 300))
	}
	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if cr.Error != nil && cr.Error.Message != "" {
		return "", fmt.Errorf("api error: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("api returned no choices")
	}
	out := strings.TrimSpace(cr.Choices[0].Message.Content)
	if out == "" {
		return "", fmt.Errorf("api returned empty content")
	}
	return out, nil
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
