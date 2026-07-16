package agent

import (
	"fmt"
	"strings"
	"time"
)

// FallbackFinding produces a deterministic, rule-based AgentFinding from the
// collected logs when the LLM is unavailable or returns invalid output. It is
// intentionally conservative: confidence is at most "medium" because no model
// reasoning was applied.
func FallbackFinding(job *AgentJob) *AgentFinding {
	bundle := job.LogBundle
	root, confidence, factors := classifyByType(job.InvestigationType, job.Focus, bundle)
	timeline := buildTimeline(bundle, 12)
	prevention := preventionFor(job.InvestigationType)

	return &AgentFinding{
		RootCause:  root,
		Confidence: confidence,
		Factors:    factors,
		Timeline:   timeline,
		Prevention:  prevention,
		RawLogs:    sampleRawLogs(bundle, 40),
		Via:        "rules",
	}
}

// classifyByType maps the investigation type + observed signals to a root cause.
// focus is the operator's free-text "what to look for"; its tokens add to the
// signal counts so a custom focus can surface matching events.
func classifyByType(invType, focus string, bundle []LogEntry) (root, confidence string, factors []string) {
	low := func(s string) string { return strings.ToLower(s) }

	// Operator focus tokens count toward the signal totals.
	focusTokens := []string{}
	for _, t := range strings.Fields(low(focus)) {
		t = strings.Trim(t, ".,;:!?\"'()[]")
		if len(t) >= 3 {
			focusTokens = append(focusTokens, t)
		}
	}
	focusHits := 0
	if len(focusTokens) > 0 {
		for _, e := range bundle {
			l := low(e.Message)
			for _, t := range focusTokens {
				if strings.Contains(l, t) {
					focusHits++
					break
				}
			}
		}
	}

	count := func(needles ...string) int {
		n := 0
		for _, e := range bundle {
			l := low(e.Message)
			for _, k := range needles {
				if strings.Contains(l, k) {
					n++
					break
				}
			}
		}
		return n
	}

	offline := count("unconnectedstate", "closingstate", "not connected", "add device failed", "remote host closed", "disconnect")
	reset := count("factory", "restore", "default", "reloadrobodmakeini", "active:false", "reset")
	battery := count("battery", "voltage", "power low", "soc", "charge")
	mapFail := count("map", "smap", "scene", "model_md5", "rds.scene", "checksum")
	oom := count("oom", "out of memory", "killed process", "killed")
	errs := count("error", "fail", "fatal", "exception")

	switch invType {
	case "Config Reset / Factory Default":
		if reset > 0 {
			return "Robot configuration was reset to factory defaults within the window.",
				"medium", []string{fmt.Sprintf("%d configuration-reset signals detected.", reset)}
		}
		return "No explicit config-reset event found in the window.", "low", nil

	case "Robot Offline":
		if offline > 0 {
			return "Robot lost its connection to the fleet manager during the window.",
				"medium", []string{fmt.Sprintf("%d disconnect/offline signals detected.", offline)}
		}
		if oom > 0 {
			return "Robot service may be offline due to a host memory (OOM) event.", "medium",
				[]string{fmt.Sprintf("%d OOM/killed signals detected.", oom)}
		}
		return "No robot-offline signal found in the window.", "low", nil

	case "Connectivity Loss":
		if offline > 0 {
			return "Network connectivity between the robot and RDS was lost during the window.",
				"medium", []string{fmt.Sprintf("%d connection-state transitions detected.", offline)}
		}
		return "No connectivity-loss signal found in the window.", "low", nil

	case "Battery Error":
		if battery > 0 {
			return "Robot reported battery or charging anomalies within the window.",
				"medium", []string{fmt.Sprintf("%d battery/charging signals detected.", battery)}
		}
		return "No battery-error signal found in the window.", "low", nil

	case "RDS Map Update Failure":
		if mapFail > 0 {
			return "An RDS map/scene update failed to apply or verify during the window.",
				"medium", []string{fmt.Sprintf("%d map/scene/checksum signals detected.", mapFail)}
		}
		return "No map-update failure found in the window.", "low", nil

	case "General Log Analysis":
		if oom > 0 {
			return "Host memory pressure (OOM) appears to be the dominant event in the window.",
				"medium", []string{fmt.Sprintf("%d OOM/killed signals detected.", oom)}
		}
		if focusHits > 0 {
			return fmt.Sprintf("%d entries matched your focus: %q.", focusHits, focus),
				"medium", []string{"Free-text focus matched events in the window."}
		}
		if errs > 0 {
			return fmt.Sprintf("Application logged %d error/failure events in the window.", errs),
				"medium", []string{"Multiple error-level events present."}
		}
		if len(bundle) == 0 {
			return "No events found in this time range for this robot.", "low", nil
		}
		return fmt.Sprintf("%d log entries collected; no single dominant failure pattern.", len(bundle)),
			"low", nil
	}

	// Unknown investigation type — best effort.
	if focusHits > 0 {
		return fmt.Sprintf("%d entries matched your focus: %q.", focusHits, focus),
			"medium", []string{"Free-text focus matched events in the window."}
	}

	if len(bundle) == 0 {
		return "No events found in this time range for this robot.", "low", nil
	}
	return fmt.Sprintf("%d log entries collected for analysis.", len(bundle)), "low", nil
}

func preventionFor(invType string) string {
	switch invType {
	case "Config Reset / Factory Default":
		return "Restore the last known-good configuration from a snapshot and restrict who can issue factory resets. Enable config-change alerting."
	case "Robot Offline":
		return "Check robot power, Wi-Fi signal, and the FleetManager-to-robot TCP path. Confirm the robot service restarted cleanly and review OOM history on the host."
	case "Connectivity Loss":
		return "Inspect switch port, cabling, DHCP lease history, and robot Wi-Fi. Verify the RDS socket reconnect logic and network MTU/stability."
	case "Battery Error":
		return "Inspect the charger, charging contacts/relay, battery BMS logs, and voltage curve. Replace the battery or charger if degraded."
	case "RDS Map Update Failure":
		return "Re-publish the map from a known-good source, verify model_md5/checksum, and confirm the operator has publish rights and adequate storage."
	default:
		return "Expand the time window or run a Deep Sync around the first event, then review the evidence list and raw logs below."
	}
}

// buildTimeline turns the most significant bundle entries into timeline events.
func buildTimeline(bundle []LogEntry, n int) []TimelineEvent {
	if len(bundle) == 0 {
		return nil
	}
	// Prefer error/warn, then newest-first, capped at n.
	var pri, rest []LogEntry
	for _, e := range bundle {
		if e.Level == "error" || e.Level == "warn" {
			pri = append(pri, e)
		} else {
			rest = append(rest, e)
		}
	}
	picked := append(pri, rest...)
	if len(picked) > n {
		picked = picked[len(picked)-n:]
	}
	out := make([]TimelineEvent, 0, len(picked))
	for _, e := range picked {
		out = append(out, TimelineEvent{
			Timestamp: e.Timestamp.Format(time.RFC3339),
			Source:    e.Source,
			Event:     e.Message,
		})
	}
	return out
}
