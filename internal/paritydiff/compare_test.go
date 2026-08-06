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

func TestHistoryRetainsEveryAttempt(t *testing.T) {
	history := History{FormatVersion: 1}
	history.Append(Attempt{FormatVersion: 1, Result: Inconclusive})
	history.Append(Attempt{FormatVersion: 1, Result: Pass})
	if len(history.Attempts) != 2 || history.Attempts[0].Result != Inconclusive || history.Attempts[1].Result != Pass {
		t.Fatalf("attempt history = %#v", history.Attempts)
	}
}
