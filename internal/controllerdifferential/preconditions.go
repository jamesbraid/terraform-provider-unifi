package controllerdifferential

import (
	"fmt"
	"sort"
)

// Requirements is what the suites need from the machine, as MEASURED values
// beside the values they must be.
//
// Every entry says what it wanted and what it found. They used to be bare
// `test` lines under set -euo pipefail: each exited 1 printing nothing, so a
// caller saw exit 1 and could not tell a wrong OS from a missing binary from an
// unpulled image. Diagnosing "wrong OS" from that cost three provisioning runs
// and a bash -x trace.
type Requirements struct {
	OS           string
	Architecture string

	// Executables are label -> path, and each must be an executable file. The
	// label is what an operator reads; the environment variable that carried it
	// is what they have to change, so both are reported.
	Executables []NamedPath
	// Images are label -> reference, and each must ALREADY be present locally.
	// This gate does not fetch images: a run that pulled one would be measuring
	// whatever the registry holds now rather than the pinned artifact, and it
	// would do so silently.
	Images []NamedPath
}

// NamedPath is one requirement's label, the variable that supplies it, and the
// value found there.
type NamedPath struct {
	Label    string
	Variable string
	Value    string
	// Present is the measurement: executable for a binary, locally available
	// for an image. Measured by the caller, because only the caller can look.
	Present bool
}

// CheckPreconditions reports EVERY unmet requirement, not the first.
//
// An operator fixing them one per run is the same waste as a gate that reports
// one disagreeing count -- and this gate's setup involves a machine, two
// binaries and three images, so one-at-a-time is several provisioning cycles.
func CheckPreconditions(requirements Requirements) []string {
	var problems []string

	if requirements.OS != "Linux" {
		problems = append(problems, fmt.Sprintf(
			"operating system is %q, want \"Linux\". This suite starts controllers and is built "+
				"for the Linux CI builders; it cannot run on a developer workstation",
			requirements.OS))
	}
	if requirements.Architecture != "x86_64" {
		problems = append(problems, fmt.Sprintf(
			"architecture is %q, want \"x86_64\". The controller and emulator images are x86_64 only",
			requirements.Architecture))
	}

	for _, executable := range requirements.Executables {
		if executable.Value == "" {
			problems = append(problems, fmt.Sprintf("%s is not set (%s)", executable.Label, executable.Variable))
			continue
		}
		if !executable.Present {
			problems = append(problems, fmt.Sprintf(
				"%s is not executable: %s=%s. Build it, or point %s at a binary that exists",
				executable.Label, executable.Variable, executable.Value, executable.Variable))
		}
	}

	for _, image := range requirements.Images {
		if image.Value == "" {
			problems = append(problems, fmt.Sprintf("%s image is not set (%s)", image.Label, image.Variable))
			continue
		}
		if !image.Present {
			problems = append(problems, fmt.Sprintf(
				"%s image is not present locally: %s=%s. Pull or build it before running; this "+
					"gate does not fetch images",
				image.Label, image.Variable, image.Value))
		}
	}

	sort.Strings(problems)
	return problems
}
