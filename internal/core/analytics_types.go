package core

// --- Analytics Types ---

type AIClassification string

const (
	AIClassHighProbability AIClassification = "high_probability_ai"
	AIClassLikelyAI       AIClassification = "likely_ai"
	AIClassMixedVerified  AIClassification = "mixed_verified"
	AIClassHumanAuthored  AIClassification = "human_authored"
)

type AIConfidenceScore struct {
	Score          float64          `json:"score"`
	Classification AIClassification `json:"classification"`
}

type AnalyticsEvent struct {
	SessionID       string  `json:"sessionId"`
	EventType       string  `json:"eventType"`
	ToolName        string  `json:"toolName,omitempty"`
	Timestamp       string  `json:"timestamp"`
	FilePath        string  `json:"filePath,omitempty"`
	LinesAdded      int     `json:"linesAdded,omitempty"`
	LinesRemoved    int     `json:"linesRemoved,omitempty"`
	AIConfidenceScore *float64 `json:"aiConfidenceScore,omitempty"`
}

type SessionSummary struct {
	TotalToolCalls     int     `json:"totalToolCalls"`
	FileEditsCount     int     `json:"fileEditsCount"`
	AIAuthoredLines    int     `json:"aiAuthoredLines"`
	HumanVerifiedLines int     `json:"humanVerifiedLines"`
	Classification     string  `json:"classification,omitempty"`
	EstimatedCostUSD   float64 `json:"estimatedCostUsd,omitempty"`
}

type AuthorshipSummary struct {
	TotalEntries            int                        `json:"totalEntries"`
	TotalLinesChanged       int                        `json:"totalLinesChanged"`
	WeightedAIScore         float64                    `json:"weightedAIScore"`
	ClassificationBreakdown map[AIClassification]int   `json:"classificationBreakdown"`
}

type StatsOptions struct {
	SessionID string `json:"sessionId,omitempty"`
	Days      int    `json:"days,omitempty"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
}
