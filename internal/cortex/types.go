package cortex

import "time"

type MemoryKind string

const (
	KindFact          MemoryKind = "fact"
	KindPreference    MemoryKind = "preference"
	KindDecision      MemoryKind = "decision"
	KindFailedAttempt MemoryKind = "failed_attempt"
	KindSolution      MemoryKind = "solution"
	KindProjectState  MemoryKind = "project_state"
)

type Scope string

const (
	ScopeGlobal  Scope = "global"
	ScopeProject Scope = "project"
	ScopeDomain  Scope = "domain"
	ScopePrivate Scope = "private"
)

type Lifecycle string

const (
	LifecycleCandidate  Lifecycle = "candidate"
	LifecycleActive     Lifecycle = "active"
	LifecycleCanonical  Lifecycle = "canonical"
	LifecycleSuperseded Lifecycle = "superseded"
	LifecycleRejected   Lifecycle = "rejected"
	LifecycleArchived   Lifecycle = "archived"
)

type ReviewDecision string

const (
	ReviewApprove   ReviewDecision = "approve"
	ReviewPromote   ReviewDecision = "promote"
	ReviewReject    ReviewDecision = "reject"
	ReviewSupersede ReviewDecision = "supersede"
	ReviewArchive   ReviewDecision = "archive"
)

type FeedbackOutcome string

const (
	FeedbackConfirmed    FeedbackOutcome = "confirmed"
	FeedbackContradicted FeedbackOutcome = "contradicted"
	FeedbackHelpful      FeedbackOutcome = "helpful"
	FeedbackUnhelpful    FeedbackOutcome = "unhelpful"
	FeedbackApplied      FeedbackOutcome = "applied"
)

type EventType string

const (
	EventCreated      EventType = "created"
	EventImported     EventType = "imported"
	EventObserved     EventType = "observed"
	EventRevised      EventType = "revised"
	EventApproved     EventType = "approved"
	EventPromoted     EventType = "promoted"
	EventRejected     EventType = "rejected"
	EventSuperseded   EventType = "superseded"
	EventArchived     EventType = "archived"
	EventRecalled     EventType = "recalled"
	EventConfirmed    EventType = "confirmed"
	EventContradicted EventType = "contradicted"
	EventHelpful      EventType = "helpful"
	EventUnhelpful    EventType = "unhelpful"
	EventApplied      EventType = "applied"
)

type Memory struct {
	ID           string     `json:"id"`
	Kind         MemoryKind `json:"kind"`
	Scope        Scope      `json:"scope"`
	ScopeKey     string     `json:"scope_key,omitempty"`
	MemoryKey    string     `json:"memory_key"`
	Lifecycle    Lifecycle  `json:"lifecycle"`
	Title        string     `json:"title"`
	Content      string     `json:"content"`
	Tags         []string   `json:"tags,omitempty"`
	TruthScore   float64    `json:"truth_score"`
	UtilityScore float64    `json:"utility_score"`
	CreatedBy    string     `json:"created_by"`
	SessionID    string     `json:"session_id,omitempty"`
	SourceRef    string     `json:"source_ref,omitempty"`
	Revision     int        `json:"revision"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	// Embedding is the offline semantic vector; omitted from JSON when empty
	// so the public API response shape is unchanged for callers that ignore it.
	Embedding []float64 `json:"embedding,omitempty"`
}

type RememberCommand struct {
	IdempotencyKey string     `json:"idempotency_key"`
	Kind           MemoryKind `json:"kind"`
	Scope          Scope      `json:"scope"`
	ScopeKey       string     `json:"scope_key,omitempty"`
	MemoryKey      string     `json:"memory_key"`
	Title          string     `json:"title"`
	Content        string     `json:"content"`
	Tags           []string   `json:"tags,omitempty"`
	AgentID        string     `json:"agent_id"`
	SessionID      string     `json:"session_id,omitempty"`
	SourceRef      string     `json:"source_ref,omitempty"`
}

type ImportCommand struct {
	IdempotencyKey string         `json:"idempotency_key"`
	Kind           MemoryKind     `json:"kind"`
	Scope          Scope          `json:"scope"`
	ScopeKey       string         `json:"scope_key,omitempty"`
	MemoryKey      string         `json:"memory_key"`
	Title          string         `json:"title"`
	Content        string         `json:"content"`
	Tags           []string       `json:"tags,omitempty"`
	AgentID        string         `json:"agent_id"`
	SessionID      string         `json:"session_id,omitempty"`
	SourceRef      string         `json:"source_ref"`
	TruthScore     float64        `json:"truth_score"`
	UtilityScore   float64        `json:"utility_score"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type RecallQuery struct {
	IdempotencyKey    string `json:"idempotency_key,omitempty"`
	AgentID           string `json:"agent_id"`
	SessionID         string `json:"session_id,omitempty"`
	Text              string `json:"text"`
	Project           string `json:"project,omitempty"`
	Domain            string `json:"domain,omitempty"`
	Limit             int    `json:"limit,omitempty"`
	TokenBudget       int    `json:"token_budget,omitempty"`
	IncludeCandidates bool   `json:"include_candidates,omitempty"`
}

type RecallItem struct {
	Memory Memory  `json:"memory"`
	Score  float64 `json:"score"`
}

type RecallResult struct {
	ID              string       `json:"id"`
	Items           []RecallItem `json:"items"`
	TokenBudget     int          `json:"token_budget,omitempty"`
	EstimatedTokens int          `json:"estimated_tokens,omitempty"`
	Truncated       bool         `json:"truncated,omitempty"`
}

type FeedbackCommand struct {
	IdempotencyKey string          `json:"idempotency_key"`
	MemoryID       string          `json:"memory_id"`
	AgentID        string          `json:"agent_id"`
	SessionID      string          `json:"session_id,omitempty"`
	Outcome        FeedbackOutcome `json:"outcome"`
	Reason         string          `json:"reason,omitempty"`
}

type ReviewCommand struct {
	IdempotencyKey string         `json:"idempotency_key"`
	MemoryID       string         `json:"memory_id"`
	ActorID        string         `json:"actor_id"`
	Decision       ReviewDecision `json:"decision"`
	Reason         string         `json:"reason,omitempty"`
}

type HistoryQuery struct {
	MemoryID string `json:"memory_id"`
	AgentID  string `json:"agent_id"`
}

type Overview struct {
	Counts   map[Lifecycle]int `json:"counts"`
	Memories []Memory          `json:"memories"`
}

type BrowseQuery struct {
	AgentID   string
	Text      string
	Lifecycle Lifecycle
	Kind      MemoryKind
	Scope     Scope
	ScopeKey  string
	CreatedBy string
	Limit     int
}

type BrowseResult struct {
	Total    int      `json:"total"`
	Memories []Memory `json:"memories"`
}

type Event struct {
	ID        string         `json:"id"`
	MemoryID  string         `json:"memory_id"`
	Type      EventType      `json:"type"`
	ActorID   string         `json:"actor_id"`
	SessionID string         `json:"session_id,omitempty"`
	Reason    string         `json:"reason,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}
