package cortex

import "time"

type AdvisorEffort string

const (
	AdvisorEffortAuto   AdvisorEffort = "auto"
	AdvisorEffortLow    AdvisorEffort = "low"
	AdvisorEffortMedium AdvisorEffort = "medium"
	AdvisorEffortHigh   AdvisorEffort = "high"
)

type AdvisorSettings struct {
	Enabled           bool          `json:"enabled"`
	Endpoint          string        `json:"endpoint"`
	Model             string        `json:"model"`
	InputTokenBudget  int           `json:"input_token_budget"`
	OutputTokenBudget int           `json:"output_token_budget"`
	Effort            AdvisorEffort `json:"effort"`
	UpdatedBy         string        `json:"updated_by"`
	UpdatedAt         time.Time     `json:"updated_at"`
}

type UpdateAdvisorSettingsCommand struct {
	ActorID           string        `json:"actor_id"`
	Enabled           bool          `json:"enabled"`
	Endpoint          string        `json:"endpoint"`
	Model             string        `json:"model"`
	InputTokenBudget  int           `json:"input_token_budget"`
	OutputTokenBudget int           `json:"output_token_budget"`
	Effort            AdvisorEffort `json:"effort"`
}

type AdvisorRunRecord struct {
	ActorID      string `json:"actor_id"`
	Model        string `json:"model"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	Status       string `json:"status"`
	Summary      string `json:"summary,omitempty"`
	ResponseJSON string `json:"response_json,omitempty"`
	Error        string `json:"error,omitempty"`
}

type AdvisorRun struct {
	ID           string    `json:"id"`
	Model        string    `json:"model"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	Status       string    `json:"status"`
	Summary      string    `json:"summary,omitempty"`
	ResponseJSON string    `json:"response_json,omitempty"`
	Error        string    `json:"error,omitempty"`
	ActorID      string    `json:"actor_id"`
	CreatedAt    time.Time `json:"created_at"`
}

type AdvisorStatus struct {
	Settings AdvisorSettings `json:"settings"`
	LastRun  *AdvisorRun     `json:"last_run,omitempty"`
}
