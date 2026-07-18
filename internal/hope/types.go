package hope

import "time"

type Skill struct {
	ID           string
	Name         string
	Description  string
	Path         string
	Source       string
	SourceURL    string
	Keywords     []string
	Role         string
	Project      string
	Enabled      bool
	UseCount     int
	SuccessCount int
	FailureCount int
	UpdatedAt    time.Time
}

type RouteRequest struct {
	Query     string
	AgentID   string
	ProjectID string
	Limit     int
}

type SkillMatch struct {
	Skill  Skill
	Score  float64
	Reason string
}

type ContextPack struct {
	ID             string
	IdempotencyKey string
	AgentID        string
	SessionID      string
	Query          string
	ProjectID      string
	Router         string
	InputTokens    int
	OutputTokens   int
	Skills         []SkillMatch
}

type SkillFeedback struct {
	IdempotencyKey string
	PackID         string
	SkillID        string
	AgentID        string
	Outcome        string
}
