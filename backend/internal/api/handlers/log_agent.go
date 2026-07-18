package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"drishti-amr-health/internal/llm"
)

const maxAgentExplainEvents = 100

type agentExplainEvent struct {
	Timestamp         string `json:"timestamp"`
	ServerName        string `json:"server_name"`
	EventType         string `json:"event_type"`
	Severity          string `json:"severity"`
	Source            string `json:"source"`
	Message           string `json:"message"`
	PlainEnglish      string `json:"plain_english"`
	RecommendedAction string `json:"recommended_action"`
}

type agentExplainRequest struct {
	Context string              `json:"context"`
	Events  []agentExplainEvent `json:"events"`
}

type AgentLogExplanation struct {
	PlainEnglish     string   `json:"plain_english"`
	LikelyCause      string   `json:"likely_cause"`
	Evidence         []string `json:"evidence"`
	RemediationSteps []string `json:"remediation_steps"`
	Confidence       string   `json:"confidence"`
	Caveats          []string `json:"caveats"`
	Via              string   `json:"via"`
}

// AgentExplain translates the currently visible log evidence and recommends
// read-only remediation guidance. It never executes a command or changes a host.
func (h *LogHandler) AgentExplain(w http.ResponseWriter, r *http.Request) {
	var req agentExplainRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.Events) == 0 {
		jsonError(w, "at least one visible log event is required", http.StatusBadRequest)
		return
	}
	if len(req.Events) > maxAgentExplainEvents {
		req.Events = req.Events[:maxAgentExplainEvents]
	}

	prompt := buildAgentExplainPrompt(req)
	text, err := llm.Complete(llm.Config{URL: h.llmURL, Model: h.llmModel, APIKey: h.llmAPIKey}, prompt, 240*time.Second)
	if err == nil {
		if result, parseErr := parseAgentExplanation(text); parseErr == nil {
			result.Via = h.llmModel
			jsonOK(w, result)
			return
		}
	}

	// Existing deterministic explanations remain useful if the external model
	// is unavailable. Be explicit that this is not an LLM conclusion.
	first := req.Events[0]
	plain := strings.TrimSpace(first.PlainEnglish)
	if plain == "" {
		plain = strings.TrimSpace(first.Message)
	}
	action := strings.TrimSpace(first.RecommendedAction)
	if action == "" {
		action = "Review the raw evidence, confirm the affected component and timestamp, then inspect the service, network path, dependencies, and recent changes before restarting anything."
	}
	jsonOK(w, AgentLogExplanation{
		PlainEnglish:     plain,
		LikelyCause:      "The available evidence is insufficient for a verified root cause.",
		Evidence:         []string{fmt.Sprintf("%s [%s] %s", first.Timestamp, first.Source, first.Message)},
		RemediationSteps: []string{action},
		Confidence:       "low",
		Caveats:          []string{"The LLM was unavailable or returned an invalid response; this is a rule-based fallback."},
		Via:              "rules",
	})
}

func buildAgentExplainPrompt(req agentExplainRequest) string {
	var b strings.Builder
	b.WriteString("You are a cautious AMR and industrial IT incident analyst. Explain only what the evidence supports. ")
	b.WriteString("Translate technical errors into plain English and provide safe, ordered remediation checks. ")
	b.WriteString("Never claim a cause as fact when it is only inferred. Never recommend destructive commands, credential disclosure, disabling security controls, or automatic changes.\n")
	if context := strings.TrimSpace(req.Context); context != "" {
		b.WriteString("Current filters/context: " + context + "\n")
	}
	b.WriteString("Visible evidence (newest first):\n")
	for i, event := range req.Events {
		fmt.Fprintf(&b, "%d. %s | server=%s | source=%s | type=%s | severity=%s | %s\n",
			i+1, event.Timestamp, event.ServerName, event.Source, event.EventType, event.Severity, event.Message)
	}
	b.WriteString(`Respond with ONLY JSON in this exact shape:
{"plain_english":"short explanation of what is happening and impact","likely_cause":"most likely cause, clearly marked as inference when not proven","evidence":["specific supporting log fact"],"remediation_steps":["safe ordered action"],"confidence":"high|medium|low","caveats":["missing evidence or alternative explanation"]}`)
	return b.String()
}

func parseAgentExplanation(text string) (AgentLogExplanation, error) {
	var result AgentLogExplanation
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return result, fmt.Errorf("no JSON object")
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &result); err != nil {
		return result, err
	}
	if strings.TrimSpace(result.PlainEnglish) == "" || len(result.RemediationSteps) == 0 {
		return result, fmt.Errorf("incomplete explanation")
	}
	switch strings.ToLower(strings.TrimSpace(result.Confidence)) {
	case "high", "medium", "low":
		result.Confidence = strings.ToLower(strings.TrimSpace(result.Confidence))
	default:
		result.Confidence = "low"
	}
	return result, nil
}
