package feeds

import "time"

// NewEnvelope creates a feed envelope with the canonical three-key structure.
// Prevents Bug #29 class errors (field name drift) by centralizing construction.
func NewEnvelope(feedType string, data map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"type":      feedType,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data":      data,
	}
}

// NewEnvelopeAt creates a feed envelope with a specific timestamp (for testing).
func NewEnvelopeAt(feedType string, data map[string]interface{}, ts time.Time) map[string]interface{} {
	return map[string]interface{}{
		"type":      feedType,
		"timestamp": ts.UTC().Format(time.RFC3339),
		"data":      data,
	}
}
