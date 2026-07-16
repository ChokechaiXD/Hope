package intelligence

import (
	"context"
	"time"
)

type Model struct {
	ID      string `json:"id"`
	OwnedBy string `json:"owned_by"`
}

type Suggestion struct {
	MemoryID string `json:"memory_id"`
	Title    string `json:"title"`
	Content  string `json:"content"`
	Scope    string `json:"scope"`
	Kind     string `json:"kind"`
	Category string `json:"category"`
	Reason   string `json:"reason"`
	Evidence string `json:"evidence"`
}

type AdviceRequest struct {
	Endpoint          string
	Model             string
	Effort            string
	InputTokenBudget  int
	OutputTokenBudget int
	Suggestions       []Suggestion
}

type Assessment struct {
	MemoryID string `json:"memory_id"`
	Verdict  string `json:"verdict"`
	Reason   string `json:"reason"`
}

type Advice struct {
	Summary      string       `json:"summary"`
	Assessments  []Assessment `json:"items"`
	InputTokens  int          `json:"input_tokens"`
	OutputTokens int          `json:"output_tokens"`
	RawJSON      string       `json:"-"`
}

type Advisor interface {
	Models(context.Context, string) ([]Model, error)
	Advise(context.Context, AdviceRequest) (Advice, error)
}

type modelCache struct {
	Endpoint string
	Models   []Model
	Expires  time.Time
}
