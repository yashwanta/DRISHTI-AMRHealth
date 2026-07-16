package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"drishti-amr-health/internal/llm"
)

// llmFindingShape mirrors the brief Â§6 required JSON schema.
type llmFindingShape struct {
	RootCause  string          `json:"root_cause"`
	Confidence string          `json:"confidence"`
	Factors    []string        `json:"factors"`
	Timeline   []TimelineEvent `json:"timeline"`
	Prevention string          `json:"prevention"`
}

// AnalyzeWithLLM calls the configured LLM (Ollama or OpenAI-compatible) to
// analyze the collected log bundle and returns a structured finding. On any
// error (network, non-JSON, schema invalid) it returns (nil, error) so the
// caller can fall back to rules.
func AnalyzeWithLLM(endpoint, model, apiKey string, job *AgentJob) (*AgentFinding, error) {
	cfg := llm.Config{URL: endpoint, Model: model, APIKey: apiKey}
	prompt := buildPrompt(job)
	text, err := llm.Complete(cfg, prompt, 240*time.Second)
	if err != nil {
		return nil, fmt.Errorf("llm complete: %w", err)
	}
	shape, err := parseFindingJSON(text)
	if err != nil {
		return nil, fmt.Errorf("parse finding json: %w", err)
	}
	return &AgentFinding{
		RootCause:  strings.TrimSpace(shape.RootCause),
		Confidence: normalizeConfidence(shape.Confidence),
		Factors:    cleanStrings(shape.Factors),
		Timeline:   shape.Timeline,
		Prevention: strings.TrimSpace(shape.Prevention),
		RawLogs:    sampleRawLogs(job.LogBundle, 40),
		Via:        model,
	}, nil
}

// buildPrompt constructs the analysis prompt enforcing the exact JSON schema.
func buildPrompt(job *AgentJob) string {
	var sb strings.Builder
	sb.WriteString("You are an AMR robot diagnostician. Analyze the following robot logs ")
	sb.WriteString("for investigation type: " + job.InvestigationType + ".\n")
	sb.WriteString("Robot: " + job.RobotID + " | Plant: " + job.PlantID + " | Window: " +
		job.WindowStart.Format(time.RFC3339) + " to " + job.WindowEnd.Format(time.RFC3339) + "\n")
	if f := strings.TrimSpace(job.Focus); f != "" {
		sb.WriteString("OPERATOR FOCUS â€” prioritize this: " + f + "\n")
	}
	sb.WriteString("\nLogs (timestamp | source | level | message):\n")
	limit := 80
	for i, e := range job.LogBundle {
		if i >= limit {
			sb.WriteString(fmt.Sprintf("... (%d more entries omitted)\n", len(job.LogBundle)-limit))
			break
		}
		sb.WriteString(fmt.Sprintf("%s | %s | %s | %s\n",
			e.Timestamp.Format(time.RFC3339), e.Source, e.Level, e.Message))
	}
	sb.WriteString("\nRespond with ONLY valid JSON, no markdown, no commentary, exactly this shape:\n")
	sb.WriteString(`{
  "root_cause": "one clear sentence",
  "confidence": "high|medium|low",
  "factors": ["contributing factor", "contributing factor"],
  "timeline": [{"timestamp": "ISO8601", "source": "string", "event": "string"}],
  "prevention": "specific actionable steps"
}`)
	return sb.String()
}

// parseFindingJSON extracts and parses the JSON finding from an LLM response,
// tolerating leading/trailing prose and ```json fences.
func parseFindingJSON(s string) (llmFindingShape, error) {
	var shape llmFindingShape
	cleaned := stripCodeFences(s)
	// Find the outermost {...} object.
	start := strings.Index(cleaned, "{")
	end := strings.LastIndex(cleaned, "}")
	if start < 0 || end < 0 || end <= start {
		return shape, fmt.Errorf("no JSON object found in response")
	}
	obj := cleaned[start : end+1]
	if err := json.Unmarshal([]byte(obj), &shape); err != nil {
		return shape, fmt.Errorf("invalid JSON: %w", err)
	}
	if strings.TrimSpace(shape.RootCause) == "" {
		return shape, fmt.Errorf("empty root_cause")
	}
	return shape, nil
}

var fenceRe = regexp.MustCompile("(?s)```(?:json)?")

func stripCodeFence(s string) string  { return fenceRe.ReplaceAllString(s, "") }
func stripCodeFences(s string) string { return strings.Trim(stripCodeFence(s), " \t\r\n`") }

func normalizeConfidence(c string) string {
	switch strings.ToLower(strings.TrimSpace(c)) {
	case "high":
		return "high"
	case "medium":
		return "medium"
	default:
		return "low"
	}
}

func cleanStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func sampleRawLogs(bundle []LogEntry, n int) []LogEntry {
	if len(bundle) <= n {
		return bundle
	}
	// Bias the sample toward error/warn lines so raw logs show the signal.
	var prioritized, rest []LogEntry
	for _, e := range bundle {
		if e.Level == "error" || e.Level == "warn" {
			prioritized = append(prioritized, e)
		} else {
			rest = append(rest, e)
		}
	}
	out := prioritized
	if len(out) < n {
		need := n - len(out)
		if need > len(rest) {
			need = len(rest)
		}
		out = append(out, rest[:need]...)
	}
	if len(out) > n {
		out = out[:n]
	}
	return out
}
