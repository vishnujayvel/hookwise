package core

// --- Guard Types ---

type GuardRuleConfig struct {
	Match  string `yaml:"match" json:"match"`
	Action string `yaml:"action" json:"action"` // "block", "warn", "confirm"
	Reason string `yaml:"reason" json:"reason"`
	When   string `yaml:"when,omitempty" json:"when,omitempty"`
	Unless string `yaml:"unless,omitempty" json:"unless,omitempty"`
}

type GuardResult struct {
	Action      string           `json:"action"` // "allow", "block", "warn", "confirm"
	Reason      string           `json:"reason,omitempty"`
	MatchedRule *GuardRuleConfig `json:"matchedRule,omitempty"`
}

type ParsedCondition struct {
	FieldPath string `json:"fieldPath"`
	Operator  string `json:"operator"` // "contains", "starts_with", "ends_with", "==", "equals", "matches"
	Value     string `json:"value"`
}
