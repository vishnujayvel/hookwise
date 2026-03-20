package core

// --- Handler Types ---

type ResolvedHandler struct {
	Name        string
	HandlerType string   // "builtin", "script", "inline"
	Events      []string // event types this handler listens for
	Module      string
	Command     string
	Action      map[string]interface{}
	Timeout     int    // milliseconds
	Phase       string // "guard", "context", "side_effect"
	ConfigRaw   map[string]interface{}
}

// HasEvent returns true if the handler listens for the given event type.
func (h *ResolvedHandler) HasEvent(eventType string) bool {
	for _, e := range h.Events {
		if e == eventType {
			return true
		}
	}
	return false
}

// --- Segment Config ---

type SegmentConfig struct {
	Builtin string              `yaml:"builtin,omitempty" json:"builtin,omitempty"`
	Custom  *CustomSegmentConfig `yaml:"custom,omitempty" json:"custom,omitempty"`
}

// UnmarshalYAML allows SegmentConfig to accept both a plain string
// (e.g., "- session") and a full struct (e.g., {builtin: "session"}).
func (s *SegmentConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Try plain string first.
	var str string
	if err := unmarshal(&str); err == nil {
		s.Builtin = str
		return nil
	}
	// Fall back to struct.
	type raw SegmentConfig
	return unmarshal((*raw)(s))
}

type CustomSegmentConfig struct {
	Command string `yaml:"command" json:"command"`
	Label   string `yaml:"label,omitempty" json:"label,omitempty"`
	Timeout int    `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}
