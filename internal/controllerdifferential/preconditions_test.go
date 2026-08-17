package controllerdifferential

import (
	"strings"
	"testing"
)

func satisfiable() Requirements {
	return Requirements{
		OS:           "Linux",
		Architecture: "x86_64",
		Executables: []NamedPath{
			{Label: "herder", Variable: "CATALOG_HERDER_BIN", Value: "/opt/herder", Present: true},
			{Label: "terraform CLI", Variable: "TERRAFORM_BIN", Value: "/opt/tofu", Present: true},
		},
		Images: []NamedPath{
			{Label: "controller", Variable: "CATALOG_CONTROLLER_IMAGE", Value: "unifi:10.4.57", Present: true},
			{Label: "synthetic", Variable: "CATALOG_SYNTHETIC_IMAGE", Value: "synthetic:1", Present: true},
			{Label: "ryuk", Variable: "CATALOG_RYUK_IMAGE", Value: "ryuk:0.5", Present: true},
		},
	}
}

func TestASatisfiableMachineIsAccepted(t *testing.T) {
	if problems := CheckPreconditions(satisfiable()); len(problems) != 0 {
		t.Fatalf("a machine that meets every requirement was refused: %v", problems)
	}
}

// TestEveryUnmetRequirementSaysWhatItWantedAndWhatItFound. These were bare
// `test` lines under set -euo pipefail: each exited 1 printing nothing, so a
// caller saw exit 1 and could not tell a wrong OS from a missing binary from an
// unpulled image. Diagnosing "wrong OS" from that cost three provisioning runs
// and a bash -x trace.
func TestEveryUnmetRequirementSaysWhatItWantedAndWhatItFound(t *testing.T) {
	for _, c := range []struct {
		name    string
		says    []string
		corrupt func(r *Requirements)
	}{
		{"a developer workstation", []string{"darwin", "Linux", "cannot run on a developer workstation"},
			func(r *Requirements) { r.OS = "darwin" }},
		{"the wrong architecture", []string{"arm64", "x86_64", "x86_64 only"},
			func(r *Requirements) { r.Architecture = "arm64" }},
		{"a binary that is not there", []string{"herder", "CATALOG_HERDER_BIN", "/opt/herder"},
			func(r *Requirements) { r.Executables[0].Present = false }},
		{"a binary nobody named", []string{"terraform CLI", "TERRAFORM_BIN", "not set"},
			func(r *Requirements) { r.Executables[1].Value = "" }},
		{"an image that was never pulled", []string{"ryuk", "CATALOG_RYUK_IMAGE", "does not fetch images"},
			func(r *Requirements) { r.Images[2].Present = false }},
		{"an image nobody named", []string{"synthetic", "CATALOG_SYNTHETIC_IMAGE", "not set"},
			func(r *Requirements) { r.Images[1].Value = "" }},
	} {
		t.Run(c.name, func(t *testing.T) {
			requirements := satisfiable()
			c.corrupt(&requirements)
			problems := CheckPreconditions(requirements)
			if len(problems) != 1 {
				t.Fatalf("reported %d problem(s), want exactly 1: %v", len(problems), problems)
			}
			for _, phrase := range c.says {
				if !strings.Contains(problems[0], phrase) {
					t.Fatalf("the report %q does not say %q, so the operator is sent to the "+
						"wrong place", problems[0], phrase)
				}
			}
		})
	}
}

// TestAnUnprovisionedMachineIsReportedInOneGo. Fixing five requirements one per
// run is five provisioning cycles, which is the same waste as a gate that
// reports one disagreeing count.
func TestAnUnprovisionedMachineIsReportedInOneGo(t *testing.T) {
	requirements := satisfiable()
	requirements.OS = "darwin"
	requirements.Architecture = "arm64"
	requirements.Executables[0].Present = false
	requirements.Images[0].Present = false
	requirements.Images[2].Present = false

	problems := CheckPreconditions(requirements)
	if len(problems) != 5 {
		t.Fatalf("reported %d of 5 problems: %v", len(problems), problems)
	}
	// And each names a different subject, or the list is five ways of saying
	// one thing.
	seen := map[string]bool{}
	for _, problem := range problems {
		if seen[problem] {
			t.Fatalf("two requirements report the same sentence: %q", problem)
		}
		seen[problem] = true
	}
}
