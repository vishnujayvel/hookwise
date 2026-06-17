package core

import "testing"

func TestIsEventType(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"PreToolUse", true},
		{"PostToolUse", true},
		{"SessionStart", true},
		{"SessionEnd", true},
		{"UserPromptSubmit", true},
		{"Setup", true},
		{"InvalidEvent", false},
		{"", false},
		{"pretooluse", false}, // case-sensitive
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := IsEventType(tt.input)
			if got != tt.want {
				t.Errorf("IsEventType(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestEventTypesCount(t *testing.T) {
	if len(EventTypes) != 13 {
		t.Errorf("expected 13 event types, got %d", len(EventTypes))
	}
}

// TestEventTypesRegistryComplete guards the EventTypes registry against a silent
// corruption that the count + partial-enumeration tests above miss: a *swap*
// (dropping one constant and padding length with a duplicate of another) keeps
// len==13 and leaves TestIsEventType green while making IsEventType reject a real
// event. Since IsEventType gates dispatch event validation, that regression must
// not pass silently. This asserts the registry is exactly the set of named
// Event* constants — no drop, no duplicate, no foreign entry.
func TestEventTypesRegistryComplete(t *testing.T) {
	allConsts := []string{
		EventUserPromptSubmit,
		EventPreToolUse,
		EventPostToolUse,
		EventPostToolUseFailure,
		EventNotification,
		EventStop,
		EventSubagentStart,
		EventSubagentStop,
		EventPreCompact,
		EventSessionStart,
		EventSessionEnd,
		EventPermissionRequest,
		EventSetup,
	}

	// Every named constant must be recognized — catches a constant dropped from
	// the EventTypes slice (even if length is padded back with a duplicate).
	for _, c := range allConsts {
		if !IsEventType(c) {
			t.Errorf("named event constant %q is not in the EventTypes registry", c)
		}
	}

	// EventTypes must contain no duplicates and no entry outside the const set —
	// catches a duplicate-paste or a stray/foreign value.
	seen := make(map[string]struct{}, len(EventTypes))
	constSet := make(map[string]struct{}, len(allConsts))
	for _, c := range allConsts {
		constSet[c] = struct{}{}
	}
	for _, et := range EventTypes {
		if _, dup := seen[et]; dup {
			t.Errorf("EventTypes contains duplicate entry %q", et)
		}
		seen[et] = struct{}{}
		if _, ok := constSet[et]; !ok {
			t.Errorf("EventTypes contains %q which is not a named Event* constant", et)
		}
	}
}

func TestHookPayloadIsValid(t *testing.T) {
	valid := HookPayload{SessionID: "abc-123"}
	if !valid.IsValidPayload() {
		t.Error("expected valid payload")
	}

	empty := HookPayload{}
	if empty.IsValidPayload() {
		t.Error("expected invalid payload for empty session_id")
	}
}
