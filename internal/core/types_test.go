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
