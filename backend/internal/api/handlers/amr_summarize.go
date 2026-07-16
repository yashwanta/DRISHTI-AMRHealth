package handlers

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"drishti-amr-health/internal/llm"
)

// SummarizeResponse is the result of the Summarize action.
type SummarizeResponse struct {
	Summary string `json:"summary"`
	Via     string `json:"via"` // "llm" | "rules"
	Model   string `json:"model,omitempty"`
	LLMNote string `json:"llm_note,omitempty"`
}

// Summarize produces a plain-English narrative over a robot's drop timeline (or,
// when no robot is given, a plant-wide connectivity summary). It prefers an LLM
// (Ollama) for natural prose and falls back to a deterministic rule-based
// paragraph if the model is unavailable - so the button always returns something
// useful. Query: plant (optional), robot (optional - omit for a plant overview).
func (h *AMRHandler) Summarize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	plant := r.URL.Query().Get("plant")
	robot := normaliseAMRName(r.URL.Query().Get("robot"))
	win := parseWindow(r)

	locs := h.loadLocationEvents(ctx, plant, win)
	drops := h.dropsFor(ctx, plant, robot, win)
	attachLocations(drops, locs)

	facts := summarizeFacts{Plant: plant, Robot: robot}
	facts.buildFrom(drops)

	// Try the LLM first; on any failure or factual contradiction, fall back to
	// rules. Timeline numbers are authoritative; prose must never invert them.
	if para, model, ok := h.summarizeWithLLM(facts); ok {
		if !summaryNeedsFallback(para, facts) {
			jsonOK(w, SummarizeResponse{Summary: para, Via: "llm", Model: model})
			return
		}
		jsonOK(w, SummarizeResponse{
			Summary: summarizeWithRules(facts),
			Via:     "rules",
			Model:   model,
			LLMNote: "LLM summary was not actionable enough for the timeline facts - showing a verified recommendation.",
		})
		return
	}
	jsonOK(w, SummarizeResponse{
		Summary: summarizeWithRules(facts),
		Via:     "rules",
		LLMNote: "LLM unavailable - showing a rule-based summary.",
	})
}

// summarizeFacts holds the distilled numbers the narrative is built from. Keeping
// the extraction separate from the wording means the LLM and the rule fallback
// consume identical facts.
type summarizeFacts struct {
	Plant      string
	Robot      string
	DropCount  int
	Resolved   int
	Unresolved int
	WorstSec   int
	TotalSec   int
	AvgSec     int
	FirstStart *time.Time
	LastStart  *time.Time
	Flapping   bool
	// Locations observed across drops, with per-location drop counts.
	Locations map[string]int
}

func (f *summarizeFacts) buildFrom(drops []dropEvent) {
	f.DropCount = len(drops)
	f.Locations = map[string]int{}
	for _, d := range drops {
		if d.Resolved {
			f.Resolved++
			f.TotalSec += d.DurationSec
		} else {
			f.Unresolved++
		}
		if d.DurationSec > f.WorstSec {
			f.WorstSec = d.DurationSec
		}
		if f.FirstStart == nil || d.Start.Before(*f.FirstStart) {
			t := d.Start
			f.FirstStart = &t
		}
		if f.LastStart == nil || d.Start.After(*f.LastStart) {
			t := d.Start
			f.LastStart = &t
		}
		if d.Location != "" {
			f.Locations[d.Location]++
		}
	}
	if f.Resolved > 0 {
		f.AvgSec = f.TotalSec / f.Resolved
	}
	f.Flapping = detectBurstGo(drops)
}

// summaryNeedsFallback catches common LLM failures before they reach the UI. The
// timeline is the source of truth, and operators need a fix path, not just prose.
func summaryNeedsFallback(summary string, f summarizeFacts) bool {
	if f.DropCount == 0 {
		return false
	}
	lower := strings.ToLower(summary)
	contradictions := []string{
		"no disconnect",
		"no reconnect",
		"no drop",
		"zero disconnect",
		"zero reconnect",
		"0 disconnect",
		"0 reconnect",
		"without any disconnect",
		"without disconnect",
		"demonstrated exceptional reliability",
		"exceptional reliability",
		"consistently connected",
		"appears stable",
		"stable connection",
		"no outages",
		"no outage",
	}
	for _, phrase := range contradictions {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	actionWords := []string{"recommended action", "check", "verify", "inspect", "confirm", "test", "ping", "review"}
	for _, word := range actionWords {
		if strings.Contains(lower, word) {
			return false
		}
	}
	return true
}

// summarizeWithLLM asks the configured LLM (Ollama or OpenAI-compatible) for a
// plain-prose paragraph from the facts. Returns ok=false on any error so the
// caller can fall back to rules.
func (h *AMRHandler) summarizeWithLLM(f summarizeFacts) (string, string, bool) {
	cfg := llm.Config{URL: h.ollamaURL, Model: h.ollamaModel, APIKey: h.llmAPIKey}
	model := cfg.Model
	if model == "" {
		model = "llama3"
	}
	para, err := llm.Complete(cfg, buildSummaryPrompt(f), 120*time.Second)
	if err != nil {
		log.Printf("summarizeWithLLM error (via %s): %v", cfg.URL, err)
		return "", model, false
	}
	return para, model, true
}

// buildSummaryPrompt asks for a concise, operator-facing paragraph. It supplies
// the distilled facts so the model doesn't have to reason over raw logs.
func buildSummaryPrompt(f summarizeFacts) string {
	var sb strings.Builder
	sb.WriteString("You are an AMR (autonomous mobile robot) fleet reliability analyst. ")
	sb.WriteString("Write ONE short plain-English paragraph (3-5 sentences) that gives an IT operator a fix path, not just a history summary. ")
	sb.WriteString("Do not use markdown, bullets, or headers. Base your text ONLY on the facts below. If the facts show any disconnect/reconnect events, do not say the robot had no disconnects, no outages, was stable, or showed exceptional reliability. Include the phrase Recommended action and give concrete checks. Do not end with cause is unclear; if proof is missing, say evidence is not enough to prove root cause, then give the next checks.\n\n")
	if f.Robot != "" {
		sb.WriteString("Robot: " + f.Robot + "\n")
	} else {
		sb.WriteString("Scope: " + scopeLabel(f.Plant) + "\n")
	}
	sb.WriteString(fmt.Sprintf("Disconnect/reconnect events: %d (%d recovered, %d still offline)\n", f.DropCount, f.Resolved, f.Unresolved))
	sb.WriteString(fmt.Sprintf("Worst single outage: %s\n", humanDuration(f.WorstSec)))
	sb.WriteString(fmt.Sprintf("Total offline time: %s\n", humanDuration(f.TotalSec)))
	sb.WriteString(fmt.Sprintf("Average outage length: %s\n", humanDuration(f.AvgSec)))
	if f.Flapping {
		sb.WriteString("Pattern: flapping (many short reconnects in a short window) - suggests Wi-Fi roaming or an unstable link rather than one outage.\n")
	}
	if f.FirstStart != nil && f.LastStart != nil {
		sb.WriteString(fmt.Sprintf("Events spanned %s to %s.\n", f.FirstStart.Format("Jan 2 15:04"), f.LastStart.Format("Jan 2 15:04")))
	}
	if len(f.Locations) > 0 {
		type loc struct {
			name string
			n    int
		}
		ls := make([]loc, 0, len(f.Locations))
		for n, c := range f.Locations {
			ls = append(ls, loc{n, c})
		}
		sort.Slice(ls, func(i, j int) bool { return ls[i].n > ls[j].n })
		var names []string
		for i := 0; i < len(ls) && i < 4; i++ {
			names = append(names, fmt.Sprintf("%s (%d drops)", ls[i].name, ls[i].n))
		}
		sb.WriteString("Map points where drops happened: " + strings.Join(names, ", ") + "\n")
	}
	sb.WriteString("Recommended action hints: " + recommendationForFacts(f) + "\n")
	sb.WriteString("\nAnswer format: start with the outage severity, then Recommended action, then the next 2-3 checks in order. Prefer robot-specific power/controller/service checks for long continuous outages; prefer Wi-Fi/AP/coverage checks for many short flapping outages. Mention worst outage duration and location only when useful.")
	return sb.String()
}

// summarizeWithRules is the deterministic fallback: assembles the same kind of
// paragraph from the facts without a model. Always returns a readable sentence.
func summarizeWithRules(f summarizeFacts) string {
	if f.DropCount == 0 {
		if f.Robot != "" {
			return fmt.Sprintf("%s showed no disconnect/reconnect events in the synced window - it appears stable.", f.Robot)
		}
		return "No AMR disconnect/reconnect events were found in the synced window for this scope."
	}

	subject := f.Robot
	if subject == "" {
		subject = "AMRs at " + scopeLabel(f.Plant)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%s had %d disconnect", subject, f.DropCount)
	if f.DropCount != 1 {
		sb.WriteString("s")
	}
	if f.FirstStart != nil && f.LastStart != nil {
		fmt.Fprintf(&sb, " between %s and %s", f.FirstStart.Format("15:04"), f.LastStart.Format("15:04 on Jan 2"))
	}
	sb.WriteString(".")
	fmt.Fprintf(&sb, " The worst single outage was %s", humanDuration(f.WorstSec))
	if f.TotalSec > 0 {
		fmt.Fprintf(&sb, " (total offline %s)", humanDuration(f.TotalSec))
	}
	sb.WriteString(".")

	if f.Unresolved > 0 {
		fmt.Fprintf(&sb, " %d disconnect%s had no matching reconnect in the logs - the robot may still be offline or the reconnect fell outside the synced window.", f.Unresolved, plural(f.Unresolved))
	}
	if f.Flapping {
		sb.WriteString(" The drops cluster tightly in time, indicating flapping - most consistent with Wi-Fi roaming or an unstable network link rather than a single outage.")
	}

	if len(f.Locations) > 0 {
		type loc struct {
			name string
			n    int
		}
		ls := make([]loc, 0, len(f.Locations))
		for n, c := range f.Locations {
			ls = append(ls, loc{n, c})
		}
		sort.Slice(ls, func(i, j int) bool { return ls[i].n > ls[j].n })
		top := ls[0]
		fmt.Fprintf(&sb, " The most affected map point was %s with %d drops.", top.name, top.n)
		if len(ls) > 1 {
			var names []string
			for i := 0; i < len(ls) && i < 3; i++ {
				names = append(names, ls[i].name)
			}
			fmt.Fprintf(&sb, " Multiple locations (%s) were involved, which can point to broader Wi-Fi/coverage issues.", strings.Join(names, ", "))
		}
	}
	fmt.Fprintf(&sb, " Recommended action: %s", recommendationForFacts(f))
	return sb.String()
}

func recommendationForFacts(f summarizeFacts) string {
	if f.DropCount == 0 {
		return "no action needed beyond normal monitoring."
	}
	if f.Unresolved > 0 {
		return "treat the robot as potentially still offline; ping it from FleetManager/RDS, confirm robot power and E-stop state on the floor, then restart or inspect the robot-side network/service before clearing the incident."
	}
	if f.Flapping || (f.DropCount >= 5 && f.WorstSec < 1800) {
		return "check Wi-Fi/AP roaming and coverage first; compare signal, AP handoff, DHCP lease changes, and switch logs around the drop times before replacing robot hardware."
	}
	if f.WorstSec >= 3600 {
		return "start with robot-specific checks: confirm the robot stayed powered, inspect battery/charger/E-stop/controller logs, verify it responds to ping from FleetManager, then restart the robot network/service if reachable."
	}
	if len(f.Locations) > 0 {
		return "inspect the repeated map/location area and nearby AP coverage, then verify the robot can keep a stable session while driving through that zone."
	}
	return "check robot power and network reachability first, then compare RDS/FleetManager logs with AP/DHCP/switch logs at the outage times to separate robot-specific failure from site network failure."
}

func scopeLabel(plant string) string {
	if plant == "" {
		return "all plants"
	}
	return plant
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// humanDuration renders seconds as a compact human string ("60.5 s", "~12 min",
// "~2 h 5 m"). It's deliberately concise for inline summary prose.
func humanDuration(secs int) string {
	if secs <= 0 {
		return "0 s"
	}
	if secs < 90 {
		return fmt.Sprintf("%d s", secs)
	}
	if secs < 3600 {
		return fmt.Sprintf("~%d min", (secs+30)/60)
	}
	h := secs / 3600
	m := (secs % 3600) / 60
	if m == 0 {
		return fmt.Sprintf("~%d h", h)
	}
	return fmt.Sprintf("~%d h %d m", h, m)
}
