package paritydiff

import (
	"encoding/json"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

func TestCompareClassifiesEquivalentObservations(t *testing.T) {
	attempt := Compare(
		Scenario{
			ID:      "dns-record-read",
			Surface: catalogparity.SurfaceKey{Kind: catalogparity.ManagedResource, Name: "unifi_dns_record"},
		},
		Observation{AdapterID: "released", State: json.RawMessage(`{"name":"entry","id":"1"}`)},
		Observation{AdapterID: "candidate", State: json.RawMessage(`{"id":"1","name":"entry"}`)},
	)
	if attempt.Result != Pass {
		t.Fatalf("result = %q, differences = %#v", attempt.Result, attempt.Differences)
	}
}

func TestCompareFailsClosed(t *testing.T) {
	scenario := Scenario{
		ID:                 "firewall-zone-read",
		Surface:            catalogparity.SurfaceKey{Kind: catalogparity.DataSource, Name: "unifi_firewall_zone"},
		RequiredDimensions: []Dimension{StateDimension, DiagnosticsDimension},
	}
	tests := map[string]struct {
		baseline  Observation
		candidate Observation
		want      AttemptResult
	}{
		"self comparison": {
			baseline:  Observation{AdapterID: "same", State: json.RawMessage(`{}`), Diagnostics: json.RawMessage(`[]`)},
			candidate: Observation{AdapterID: "same", State: json.RawMessage(`{}`), Diagnostics: json.RawMessage(`[]`)},
			want:      Invalid,
		},
		"malformed JSON": {
			baseline:  Observation{AdapterID: "released", State: json.RawMessage(`{"id":`), Diagnostics: json.RawMessage(`[]`)},
			candidate: Observation{AdapterID: "candidate", State: json.RawMessage(`{}`), Diagnostics: json.RawMessage(`[]`)},
			want:      Invalid,
		},
		"missing dimension": {
			baseline:  Observation{AdapterID: "released", State: json.RawMessage(`{}`), Diagnostics: json.RawMessage(`[]`)},
			candidate: Observation{AdapterID: "candidate", State: json.RawMessage(`{}`)},
			want:      Uncovered,
		},
		"execution error": {
			baseline:  Observation{AdapterID: "released", State: json.RawMessage(`{}`), Diagnostics: json.RawMessage(`[]`)},
			candidate: Observation{AdapterID: "candidate", ExecutionError: "target unavailable"},
			want:      Inconclusive,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			attempt := Compare(scenario, test.baseline, test.candidate)
			if attempt.Result != test.want {
				t.Fatalf("result = %q, want %q; differences = %#v", attempt.Result, test.want, attempt.Differences)
			}
		})
	}
}

func TestCompareReportsSemanticDifferenceByJSONPointer(t *testing.T) {
	attempt := Compare(
		Scenario{
			ID:                 "network-refresh",
			Surface:            catalogparity.SurfaceKey{Kind: catalogparity.ManagedResource, Name: "unifi_network"},
			RequiredDimensions: []Dimension{StateDimension},
		},
		Observation{AdapterID: "released", State: json.RawMessage(`{"nested":{"enabled":true},"members":["a","b"]}`)},
		Observation{AdapterID: "candidate", State: json.RawMessage(`{"nested":{"enabled":false},"members":["a","c"]}`)},
	)
	if attempt.Result != Divergent {
		t.Fatalf("result = %q, differences = %#v", attempt.Result, attempt.Differences)
	}
	want := []Difference{
		{Dimension: StateDimension, Pointer: "/members/1", Baseline: `"b"`, Candidate: `"c"`},
		{Dimension: StateDimension, Pointer: "/nested/enabled", Baseline: "true", Candidate: "false"},
	}
	if len(attempt.Differences) != len(want) {
		t.Fatalf("differences = %#v, want %#v", attempt.Differences, want)
	}
	for index := range want {
		if attempt.Differences[index] != want[index] {
			t.Fatalf("difference %d = %#v, want %#v", index, attempt.Differences[index], want[index])
		}
	}
}

// TestCompareReportsDivergenceEvenWhenAnotherDimensionIsExcused covers what
// TestCompareFailsClosed structurally cannot.
//
// That test's "missing dimension" case carries {} on BOTH sides, so there is no
// divergence present for the excusal to hide. It proves Uncovered is returned
// and is incapable of observing what returning it costs.
//
// The cost: a real difference found by diffValues was recorded in Differences
// and then dropped from the verdict, because one excused dimension short-
// circuited the result for every other dimension. Not wrong for admission --
// the promotion gate is `!= Pass`, so Uncovered still refuses -- but inverted
// for diagnosis. Uncovered tells an operator to go and collect more evidence;
// Divergent tells them the candidate behaves differently. Same observations,
// opposite next action, and the evidence to tell them apart had already been
// gathered and thrown away.
//
// The controls are load-bearing. Without them a mechanism that answered
// "divergent" unconditionally would pass every case above.
func TestCompareReportsDivergenceEvenWhenAnotherDimensionIsExcused(t *testing.T) {
	surface := catalogparity.SurfaceKey{Kind: catalogparity.ManagedResource, Name: "unifi_network"}
	diverging := json.RawMessage(`{"enabled":true}`)
	diverged := json.RawMessage(`{"enabled":false}`)

	tests := map[string]struct {
		dimensions []Dimension
		baseline   Observation
		candidate  Observation
		want       AttemptResult
	}{
		"a missing dimension does not mask a real divergence": {
			dimensions: []Dimension{StateDimension, DiagnosticsDimension},
			baseline:   Observation{AdapterID: "released", State: diverging, Diagnostics: json.RawMessage(`[]`)},
			candidate:  Observation{AdapterID: "candidate", State: diverged},
			want:       Divergent,
		},
		"nor does it when the excused dimension is compared first": {
			dimensions: []Dimension{DiagnosticsDimension, StateDimension},
			baseline:   Observation{AdapterID: "released", State: diverging, Diagnostics: json.RawMessage(`[]`)},
			candidate:  Observation{AdapterID: "candidate", State: diverged},
			want:       Divergent,
		},
		"a malformed dimension does not mask a real divergence": {
			dimensions: []Dimension{StateDimension, DiagnosticsDimension},
			baseline:   Observation{AdapterID: "released", State: diverging, Diagnostics: json.RawMessage(`[]`)},
			candidate:  Observation{AdapterID: "candidate", State: diverged, Diagnostics: json.RawMessage(`[`)},
			want:       Divergent,
		},
		// Severity, not declaration order: a malformed observation outranks an
		// absent one, whichever dimension happens to be compared last.
		"invalid outranks uncovered when neither hides a divergence": {
			dimensions: []Dimension{PlanDimension, DiagnosticsDimension},
			baseline:   Observation{AdapterID: "released", Plan: json.RawMessage(`{`), Diagnostics: json.RawMessage(`[]`)},
			candidate:  Observation{AdapterID: "candidate", Plan: json.RawMessage(`{}`)},
			want:       Invalid,
		},
		// Controls.
		"a real divergence alone is still divergent": {
			dimensions: []Dimension{StateDimension},
			baseline:   Observation{AdapterID: "released", State: diverging},
			candidate:  Observation{AdapterID: "candidate", State: diverged},
			want:       Divergent,
		},
		"an excused dimension with nothing to hide is still uncovered": {
			dimensions: []Dimension{StateDimension, DiagnosticsDimension},
			baseline:   Observation{AdapterID: "released", State: diverging, Diagnostics: json.RawMessage(`[]`)},
			candidate:  Observation{AdapterID: "candidate", State: diverging},
			want:       Uncovered,
		},
		"a malformed dimension with nothing to hide is still invalid": {
			dimensions: []Dimension{StateDimension, DiagnosticsDimension},
			baseline:   Observation{AdapterID: "released", State: diverging, Diagnostics: json.RawMessage(`[]`)},
			candidate:  Observation{AdapterID: "candidate", State: diverging, Diagnostics: json.RawMessage(`[`)},
			want:       Invalid,
		},
		"agreement across every dimension still passes": {
			dimensions: []Dimension{StateDimension, DiagnosticsDimension},
			baseline:   Observation{AdapterID: "released", State: diverging, Diagnostics: json.RawMessage(`[]`)},
			candidate:  Observation{AdapterID: "candidate", State: diverging, Diagnostics: json.RawMessage(`[]`)},
			want:       Pass,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			attempt := Compare(
				Scenario{ID: "excusal", Surface: surface, RequiredDimensions: test.dimensions},
				test.baseline, test.candidate,
			)
			if attempt.Result != test.want {
				t.Fatalf("result = %q, want %q; differences = %#v", attempt.Result, test.want, attempt.Differences)
			}
		})
	}
}

// TestCompareKeepsTheExcusedDimensionInTheDifferences guards the other half of
// the fix: reporting Divergent must not drop the record of what could not be
// compared. The verdict changes; the evidence does not shrink.
func TestCompareKeepsTheExcusedDimensionInTheDifferences(t *testing.T) {
	attempt := Compare(
		Scenario{
			ID:                 "excusal-evidence",
			Surface:            catalogparity.SurfaceKey{Kind: catalogparity.ManagedResource, Name: "unifi_network"},
			RequiredDimensions: []Dimension{StateDimension, DiagnosticsDimension},
		},
		Observation{AdapterID: "released", State: json.RawMessage(`{"enabled":true}`), Diagnostics: json.RawMessage(`[]`)},
		Observation{AdapterID: "candidate", State: json.RawMessage(`{"enabled":false}`)},
	)
	if attempt.Result != Divergent {
		t.Fatalf("result = %q, want %q", attempt.Result, Divergent)
	}
	var sawPresence, sawDivergence bool
	for _, difference := range attempt.Differences {
		if difference.Dimension == DiagnosticsDimension && difference.Candidate == "<missing>" {
			sawPresence = true
		}
		if difference.Dimension == StateDimension && difference.Pointer == "/enabled" {
			sawDivergence = true
		}
	}
	if !sawPresence || !sawDivergence {
		t.Fatalf("differences = %#v, want both the missing diagnostics and the state divergence", attempt.Differences)
	}
}

func TestHistoryRetainsEveryAttempt(t *testing.T) {
	history := History{FormatVersion: 1}
	history.Append(Attempt{FormatVersion: 1, Result: Inconclusive})
	history.Append(Attempt{FormatVersion: 1, Result: Pass})
	if len(history.Attempts) != 2 || history.Attempts[0].Result != Inconclusive || history.Attempts[1].Result != Pass {
		t.Fatalf("attempt history = %#v", history.Attempts)
	}
}
