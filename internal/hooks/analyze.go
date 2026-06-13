package hooks

import (
	"fmt"
	"sort"
	"strings"
)

// Severity levels for doctor findings, rendered as the leading column.
const (
	LevelScan = "SCAN"
	LevelPass = "PASS"
	LevelInfo = "INFO"
	LevelWarn = "WARN"
	LevelFail = "FAIL"
)

// Finding is one doctor result line plus optional indented detail lines.
type Finding struct {
	Level   string   // LevelScan/Info/Warn/Fail
	Code    string   // short tag, e.g. "hook-sprawl"
	Message string   // the headline after "LEVEL  code: "
	Details []string // indented follow-up lines (remediation, breakdown)
}

// pluralHooks renders a hook count with correct singular/plural ("1 hook",
// "2 hooks").
func pluralHooks(n int) string {
	if n == 1 {
		return "1 hook"
	}
	return fmt.Sprintf("%d hooks", n)
}

// hotPathEvents are the high-frequency events where per-call cost matters most.
var hotPathEvents = map[string]bool{
	"PreToolUse":  true,
	"PostToolUse": true,
}

// ---------------------------------------------------------------------------
// #34 — inventory + sprawl
// ---------------------------------------------------------------------------

// eventCount pairs an event with its hook count for stable sorting.
type eventCount struct {
	Event string
	Count int
}

// sortedEventCounts returns per-event counts ordered by count desc, then name asc.
func sortedEventCounts(inv *Inventory) []eventCount {
	counts := inv.CountByEvent()
	out := make([]eventCount, 0, len(counts))
	for ev, c := range counts {
		out = append(out, eventCount{ev, c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Event < out[j].Event
	})
	return out
}

// InventoryFinding produces the SCAN line summarizing total hooks + per-event
// breakdown (issue #34 F1.1).
func InventoryFinding(inv *Inventory) Finding {
	ecs := sortedEventCounts(inv)
	var details []string
	for _, ec := range ecs {
		details = append(details, fmt.Sprintf("%-14s %s", ec.Event+":", pluralHooks(ec.Count)))
	}
	return Finding{
		Level:   LevelScan,
		Code:    "hooks",
		Message: fmt.Sprintf("%d hooks across %d events", len(inv.Entries), len(ecs)),
		Details: details,
	}
}

// sprawlThreshold returns (warn, fail) hook-count thresholds for an event.
func sprawlThreshold(event string) (warn, fail int) {
	if hotPathEvents[event] {
		return 5, 10
	}
	return 3, 8
}

// firesEveryCall reports whether a matcher causes its hook to run on every event
// of that type. An empty or "*" matcher is unconditional; a specific tool
// matcher (e.g. "mcp__cal__create") only fires when that tool is used.
func firesEveryCall(matcher string) bool {
	m := strings.TrimSpace(matcher)
	return m == "" || m == "*"
}

// alwaysFireByEvent counts, per event, the hooks that fire on every call
// (unconditional matcher). Matcher-scoped hooks are excluded — they are the
// real per-call process cost, so they drive the sprawl alarm.
func alwaysFireByEvent(inv *Inventory) map[string]int {
	counts := map[string]int{}
	for _, e := range inv.Entries {
		if firesEveryCall(e.Matcher) {
			counts[e.Event]++
		}
	}
	return counts
}

// SprawlFindings flags events with too many ALWAYS-FIRE hooks (issue #34 F1.2).
// Severity is driven by hooks that run on every call (unconditional matcher),
// not by matcher-scoped hooks: a hook bound to one specific tool does not fork a
// process on unrelated calls, so counting it toward per-call sprawl is wrong.
func SprawlFindings(inv *Inventory) []Finding {
	alwaysFire := alwaysFireByEvent(inv)

	// Deterministic order: by count desc, then event name.
	events := make([]string, 0, len(alwaysFire))
	for ev := range alwaysFire {
		events = append(events, ev)
	}
	sort.Slice(events, func(i, j int) bool {
		if alwaysFire[events[i]] != alwaysFire[events[j]] {
			return alwaysFire[events[i]] > alwaysFire[events[j]]
		}
		return events[i] < events[j]
	})

	var out []Finding
	for _, ev := range events {
		count := alwaysFire[ev]
		warn, fail := sprawlThreshold(ev)
		level := ""
		threshold := warn
		switch {
		case count > fail:
			level, threshold = LevelFail, fail
		case count > warn:
			level, threshold = LevelWarn, warn
		}
		if level == "" {
			continue
		}
		out = append(out, Finding{
			Level:   level,
			Code:    "hook-sprawl",
			Message: fmt.Sprintf("%s has %d always-on hooks (threshold: %d)", ev, count, threshold),
			Details: []string{"These hooks fork a process on every call. Consider consolidating."},
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// #33 — missing binary
// ---------------------------------------------------------------------------

// commandBinary returns the executable name from a hook command string (the
// first whitespace-delimited token). Returns "" for an empty command.
func commandBinary(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// MissingBinaryFindings flags hook binaries not resolvable on PATH (issue #33
// F1.3). lookPath is injected so tests are hermetic; production passes
// exec.LookPath. Each absent binary yields one FAIL with the count of hooks
// depending on it.
func MissingBinaryFindings(inv *Inventory, lookPath func(string) (string, error)) []Finding {
	type stat struct {
		count   int
		present bool
		checked bool
	}
	seen := map[string]*stat{}
	order := []string{}
	for _, e := range inv.Entries {
		bin := commandBinary(e.Command)
		if bin == "" {
			continue
		}
		s, ok := seen[bin]
		if !ok {
			s = &stat{}
			seen[bin] = s
			order = append(order, bin)
		}
		s.count++
		if !s.checked {
			_, err := lookPath(bin)
			s.present = err == nil
			s.checked = true
		}
	}

	var out []Finding
	for _, bin := range order {
		s := seen[bin]
		if s.present {
			continue
		}
		out = append(out, Finding{
			Level:   LevelFail,
			Code:    "hook-binary",
			Message: fmt.Sprintf("%q not found on PATH (used by %d hooks)", bin, s.count),
			Details: []string{
				"These hooks will fail silently on every matching event.",
				fmt.Sprintf("Fix: install %s and put it on PATH, or remove these hooks.", bin),
			},
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// #35 — network-dependent hooks on hot paths
// ---------------------------------------------------------------------------

// networkPattern is a substring whose presence in a command implies a
// network-dependent runner.
type networkPattern struct {
	match string
	why   string
}

var networkPatterns = []networkPattern{
	{"uvx ", "uvx resolves Python packages over the network on each invocation."},
	{"uv run ", "uv run may resolve packages over the network."},
	{"npx ", "npx resolves Node packages over the network on each invocation."},
	{"pip install", "pip install fetches packages over the network."},
	{"curl ", "curl makes a direct network call."},
	{"wget ", "wget makes a direct network call."},
	{"docker run ", "docker run may pull an image over the network."},
}

// NetworkHookFindings flags network-dependent runners used on hot-path events
// (issue #35 F1.4). Non-hot-path events are not flagged.
func NetworkHookFindings(inv *Inventory) []Finding {
	var out []Finding
	for _, e := range inv.Entries {
		if !hotPathEvents[e.Event] {
			continue
		}
		// "docker run --pull=never" never pulls — treat as safe.
		if strings.Contains(e.Command, "docker run") && strings.Contains(e.Command, "--pull=never") {
			continue
		}
		cmd := e.Command + " " // trailing space so a bare "uvx" at end still matches "uvx "
		for _, p := range networkPatterns {
			if strings.Contains(cmd, p.match) {
				out = append(out, Finding{
					Level:   LevelWarn,
					Code:    "hook-network",
					Message: fmt.Sprintf("%s hook uses %q", e.Event, e.Command),
					Details: []string{
						p.why,
						"On hot-path events, this adds latency to every tool call.",
						"Consider: install the package locally or replace with a compiled binary.",
					},
				})
				break
			}
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// #36 — duplicates + overlap
// ---------------------------------------------------------------------------

// knownGuards maps a command substring to a human label for overlap detection.
var knownGuards = []struct{ match, label string }{
	{"hookwise dispatch", "hookwise dispatch (hookwise guards)"},
	{"claude-code-guardian", "claude-code-guardian"},
	{"skill-routing-guard", "skill-routing-guard"},
}

// DuplicateFindings flags exact duplicate hooks (same command on the same event)
// and functional overlap of known guard systems on a single event (issue #36).
func DuplicateFindings(inv *Inventory) []Finding {
	// Group entries by event.
	byEvent := map[string][]HookEntry{}
	eventOrder := []string{}
	for _, e := range inv.Entries {
		if _, ok := byEvent[e.Event]; !ok {
			eventOrder = append(eventOrder, e.Event)
		}
		byEvent[e.Event] = append(byEvent[e.Event], e)
	}
	sort.Strings(eventOrder)

	var out []Finding
	for _, event := range eventOrder {
		entries := byEvent[event]

		// Exact duplicates: same (matcher, command) appearing >1 times. The same
		// command under DIFFERENT matchers is intentional per-tool protection,
		// not a duplicate, so the matcher is part of the dedup key.
		type key struct{ matcher, command string }
		cmdCount := map[key]int{}
		cmdOrder := []key{}
		for _, e := range entries {
			k := key{e.Matcher, e.Command}
			if _, ok := cmdCount[k]; !ok {
				cmdOrder = append(cmdOrder, k)
			}
			cmdCount[k]++
		}
		for _, k := range cmdOrder {
			cmd := k.command
			if cmdCount[k] > 1 {
				out = append(out, Finding{
					Level:   LevelWarn,
					Code:    "hook-duplicate",
					Message: fmt.Sprintf("%q appears %d times on %s", cmd, cmdCount[k], event),
					Details: []string{"Identical hooks waste resources. Remove the duplicates."},
				})
			}
		}

		// Functional overlap: distinct known guard systems on this event.
		var labels []string
		seenLabel := map[string]bool{}
		for _, e := range entries {
			for _, g := range knownGuards {
				if strings.Contains(e.Command, g.match) && !seenLabel[g.label] {
					seenLabel[g.label] = true
					labels = append(labels, g.label)
				}
			}
		}
		if len(labels) > 1 {
			details := make([]string, 0, len(labels)+1)
			for _, l := range labels {
				details = append(details, "• "+l)
			}
			details = append(details, "Consider consolidating into a single guard framework.")
			out = append(out, Finding{
				Level:   LevelInfo,
				Code:    "hook-overlap",
				Message: fmt.Sprintf("%d guard systems detected on %s", len(labels), event),
				Details: details,
			})
		}
	}
	return out
}
