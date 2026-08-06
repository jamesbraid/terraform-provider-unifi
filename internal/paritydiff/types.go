package paritydiff

import (
	"encoding/json"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

type Dimension string

const (
	RequestDimension          Dimension = "request"
	ControllerResultDimension Dimension = "controller_result"
	StateDimension            Dimension = "state"
	DiagnosticsDimension      Dimension = "diagnostics"
	PlanDimension             Dimension = "plan"
)

type Observation struct {
	AdapterID        string          `json:"adapter_id"`
	Request          json.RawMessage `json:"request,omitempty"`
	ControllerResult json.RawMessage `json:"controller_result,omitempty"`
	State            json.RawMessage `json:"state,omitempty"`
	Diagnostics      json.RawMessage `json:"diagnostics,omitempty"`
	Plan             json.RawMessage `json:"plan,omitempty"`
	ExecutionError   string          `json:"execution_error,omitempty"`
}

type AttemptResult string

const (
	Pass         AttemptResult = "pass"
	Uncovered    AttemptResult = "uncovered"
	Divergent    AttemptResult = "divergent"
	Inconclusive AttemptResult = "inconclusive"
	Invalid      AttemptResult = "invalid"
)

type Scenario struct {
	ID                 string                   `json:"id"`
	Surface            catalogparity.SurfaceKey `json:"surface"`
	RequiredDimensions []Dimension              `json:"required_dimensions"`
}

type Difference struct {
	Dimension Dimension `json:"dimension"`
	Pointer   string    `json:"pointer"`
	Baseline  string    `json:"baseline,omitempty"`
	Candidate string    `json:"candidate,omitempty"`
}

type Attempt struct {
	FormatVersion int           `json:"format_version"`
	Scenario      Scenario      `json:"scenario"`
	BaselineID    string        `json:"baseline_adapter_id"`
	CandidateID   string        `json:"candidate_adapter_id"`
	Result        AttemptResult `json:"result"`
	Differences   []Difference  `json:"differences"`
}

type History struct {
	FormatVersion int       `json:"format_version"`
	Attempts      []Attempt `json:"attempts"`
}

func (h *History) Append(attempt Attempt) {
	h.Attempts = append(h.Attempts, attempt)
}
