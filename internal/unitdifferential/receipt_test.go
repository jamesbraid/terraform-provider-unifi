package unitdifferential

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

func passingSuite() catalogparity.UnitSuiteReceipt {
	return catalogparity.UnitSuiteReceipt{Result: "pass", PackagePassCount: 1, PassedTestCount: 1}
}

// TestResultIsDerivedFromTheSuitesAndTheBlockers walks every combination that
// changes the answer. In the shell these were separate variables assembled at
// the end, so a receipt could carry any pair of them.
func TestResultIsDerivedFromTheSuitesAndTheBlockers(t *testing.T) {
	failing := catalogparity.UnitSuiteReceipt{Result: "fail", FailedTestCount: 1}
	for _, c := range []struct {
		name      string
		run       Run
		want      string
		wantWhy   string
		blockedBy int
	}{
		{
			name: "clean suites and no blockers",
			run:  Run{Released: passingSuite(), Candidate: passingSuite()},
			want: "pass",
		},
		{
			name: "a failing candidate suite",
			run:  Run{Released: passingSuite(), Candidate: failing},
			want: "fail",
		},
		{
			name: "a failing released suite",
			run:  Run{Released: failing, Candidate: passingSuite()},
			want: "fail",
		},
		{
			name: "blockers, diagnostics allowed",
			run: Run{Released: passingSuite(), Candidate: passingSuite(),
				PromotionBlockers: []string{"go_version"}, DiagnosticToolchainAllowed: true},
			want: "diagnostic_pass",
		},
		{
			name: "blockers, diagnostics not allowed",
			run: Run{Released: passingSuite(), Candidate: passingSuite(),
				PromotionBlockers: []string{"platform"}},
			want: "blocked",
		},
		{
			name: "a failing suite outranks a permitted diagnostic toolchain",
			run: Run{Released: passingSuite(), Candidate: failing,
				PromotionBlockers: []string{"go_version"}, DiagnosticToolchainAllowed: true},
			want:    "fail",
			wantWhy: "an environment mismatch cannot excuse a broken test",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := BuildUnitDifferentialReceipt(c.run).Result
			if got != c.want {
				t.Fatalf("result = %q, want %q. %s", got, c.want, c.wantWhy)
			}
		})
	}
}

func TestAbsentBlockersRenderAsAnEmptyArray(t *testing.T) {
	receipt := BuildUnitDifferentialReceipt(Run{Released: passingSuite(), Candidate: passingSuite()})
	encoded, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"promotion_blockers":[]`)) {
		t.Fatalf("blockers rendered as something other than an empty array: %s", encoded)
	}
}

// TestTheConstructorReproducesTheFrozenShellReceipt feeds the constructor what
// the shell measured on pipeline 232 and requires it to emit that receipt.
//
// The pass-through fields are near-tautological here; the teeth are in what the
// constructor DECIDES -- result, gate, format_version, network -- every one of
// which was a literal in the jq template. It is also blind wherever the frozen
// run's value equals the literal a port would write: that run was promotable
// with no blockers, so a constructor that always wrote "pass" would satisfy
// this. The table above is what covers that, by passing combinations the frozen
// receipt does not contain.
func TestTheConstructorReproducesTheFrozenShellReceipt(t *testing.T) {
	path := filepath.Join("..", "..", "build", "migration-baseline", "catalog-unit-differential.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var frozen catalogparity.UnitDifferentialReceipt
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&frozen); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	if len(frozen.PromotionBlockers) != 0 || frozen.Result != "pass" {
		t.Fatalf("the frozen run is %q with blockers %v, so it no longer exercises the pass path",
			frozen.Result, frozen.PromotionBlockers)
	}

	rebuilt := BuildUnitDifferentialReceipt(Run{
		SourceCommit:      frozen.SourceCommit,
		ReleasedCommit:    frozen.ReleasedCommit,
		Platform:          frozen.Platform,
		GoVersion:         frozen.GoVersion,
		InventorySHA256:   frozen.InventorySHA256,
		Released:          frozen.Released,
		Candidate:         frozen.Candidate,
		TreeState:         frozen.TreeState,
		PromotionBlockers: frozen.PromotionBlockers,
	})

	want, err := catalogparity.MarshalReceipt(frozen)
	if err != nil {
		t.Fatal(err)
	}
	got, err := catalogparity.MarshalReceipt(rebuilt)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("the constructor does not reproduce the shell's receipt.\nfrozen:\n%s\nrebuilt:\n%s", want, got)
	}
}

// TestTheFrozenSuiteCountsSurviveTheSummariser is the one assertion that ties
// this package's two halves together: the counts in the frozen receipt are the
// shape Summarise produces, not just the shape the type allows.
func TestTheFrozenSuiteCountsSurviveTheSummariser(t *testing.T) {
	path := filepath.Join("..", "..", "build", "migration-baseline", "catalog-unit-differential.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var frozen catalogparity.UnitDifferentialReceipt
	if err := json.Unmarshal(raw, &frozen); err != nil {
		t.Fatal(err)
	}
	for name, suite := range map[string]catalogparity.UnitSuiteReceipt{
		"released": frozen.Released, "candidate": frozen.Candidate,
	} {
		if suite.Result != "pass" {
			t.Fatalf("%s suite is %q in the frozen receipt", name, suite.Result)
		}
		// The three conditions Summarise applies, checked against a real
		// passing run rather than against a fixture written to satisfy them.
		if suite.ExitCode != 0 || suite.UnparsedLineCount != 0 ||
			suite.FailedTestCount != 0 || suite.PackageFailCount != 0 {
			t.Fatalf("%s suite passed in the shell while carrying %+v, which Summarise would "+
				"call a failure -- the two implementations disagree about what a pass is",
				name, suite)
		}
		if suite.PackagePassCount == 0 || suite.PassedTestCount == 0 {
			t.Fatalf("%s suite passed with no packages or no tests, so the counts the receipt "+
				"records are not evidence that anything ran: %+v", name, suite)
		}
	}
}
