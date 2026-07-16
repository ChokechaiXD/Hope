package cortex

import "time"

type CuratorMode string

const (
	CuratorManual    CuratorMode = "manual"
	CuratorAssisted  CuratorMode = "assisted"
	CuratorAutomatic CuratorMode = "automatic"
)

type CuratorCategory string

const (
	CuratorReady     CuratorCategory = "ready"
	CuratorAttention CuratorCategory = "attention"
	CuratorWaiting   CuratorCategory = "waiting"
	CuratorProtected CuratorCategory = "protected"
)

type CuratorReason string

const (
	CuratorAgreement      CuratorReason = "independent_agreement"
	CuratorContradicted   CuratorReason = "contradicted"
	CuratorLowTruth       CuratorReason = "low_truth"
	CuratorLowUtility     CuratorReason = "low_utility"
	CuratorNeedsAgreement CuratorReason = "needs_agreement"
	CuratorNeedsSource    CuratorReason = "needs_source"
	CuratorProtectedScope CuratorReason = "protected_scope"
	CuratorProtectedKind  CuratorReason = "protected_kind"
	CuratorImported       CuratorReason = "imported"
)

type CuratorSettings struct {
	Mode               CuratorMode `json:"mode"`
	RunEveryCandidates int         `json:"run_every_candidates"`
	BatchLimit         int         `json:"batch_limit"`
	MinAgreement       int         `json:"min_agreement"`
	UpdatedBy          string      `json:"updated_by"`
	UpdatedAt          time.Time   `json:"updated_at"`
}

type UpdateCuratorSettingsCommand struct {
	ActorID            string      `json:"actor_id"`
	Mode               CuratorMode `json:"mode"`
	RunEveryCandidates int         `json:"run_every_candidates"`
	BatchLimit         int         `json:"batch_limit"`
	MinAgreement       int         `json:"min_agreement"`
}

type CuratorSuggestion struct {
	Memory         Memory          `json:"memory"`
	Category       CuratorCategory `json:"category"`
	Reason         CuratorReason   `json:"reason"`
	Recommendation ReviewDecision  `json:"recommendation,omitempty"`
	Supporters     []string        `json:"supporters,omitempty"`
	Confirmations  int             `json:"confirmations"`
	Contradictions int             `json:"contradictions"`
	HasSource      bool            `json:"has_source"`
	SafeToApply    bool            `json:"safe_to_apply"`
}

type CuratorReport struct {
	Analyzed    int                 `json:"analyzed"`
	Ready       int                 `json:"ready"`
	Attention   int                 `json:"attention"`
	Waiting     int                 `json:"waiting"`
	Protected   int                 `json:"protected"`
	Applied     int                 `json:"applied"`
	Suggestions []CuratorSuggestion `json:"suggestions"`
}

type CuratorRun struct {
	ID        string      `json:"id"`
	Mode      CuratorMode `json:"mode"`
	Trigger   string      `json:"trigger"`
	Analyzed  int         `json:"analyzed"`
	Ready     int         `json:"ready"`
	Attention int         `json:"attention"`
	Applied   int         `json:"applied"`
	ActorID   string      `json:"actor_id"`
	Error     string      `json:"error,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
}

type CurateCommand struct {
	ActorID   string `json:"actor_id"`
	Trigger   string `json:"trigger"`
	ApplySafe bool   `json:"apply_safe"`
}

type CuratorStatus struct {
	Settings CuratorSettings `json:"settings"`
	LastRun  *CuratorRun     `json:"last_run,omitempty"`
}
