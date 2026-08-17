package unitdifferential

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// This file exists for exactly as long as catalog-unit-differential.sh does.
//
// The jq program it compares against is EXTRACTED FROM THE SCRIPT rather than
// copied here. A copy would be a second implementation of the thing being
// verified, and it would keep passing after the original changed. The
// extraction fails loudly if the script's shape moves, which is the signal that
// this comparison needs looking at rather than a green run that means nothing.
//
// DELETE THIS FILE IN THE SAME COMMIT THAT DELETES THE SCRIPT. It does not skip
// when the script is missing.

const unitScript = "../../.woodpecker/scripts/catalog-unit-differential.sh"

// shellSummary is the subset of the jq program's output this comparison is
// about: everything the receipt derives from the log, and nothing about how the
// events were rendered.
type shellSummary struct {
	ExitCode          int `json:"exit_code"`
	UnparsedLineCount int `json:"unparsed_line_count"`
	PackagePassCount  int `json:"package_pass_count"`
	PackageFailCount  int `json:"package_fail_count"`
	PassedTestCount   int `json:"passed_test_count"`
	SkippedTestCount  int `json:"skipped_test_count"`
	FailedTestCount   int `json:"failed_test_count"`
	Result            string
	Normalized        []Event `json:"normalized"`
}

func TestTheSummaryAgreesWithTheShellsJQProgram(t *testing.T) {
	program := extractJQProgram(t)
	requireJQ(t)

	for _, c := range []struct {
		name  string
		lines []string
		exit  int
	}{
		{"a clean run", []string{testRun, testOutput, testPass, testSkip, packagePass}, 0},
		{"a failing test with a zero exit", []string{testFail, packagePass}, 0},
		{"a clean log with a non-zero exit", []string{testPass, packagePass}, 2},
		{"a build error in the log", []string{"# example.com/a", "./broken.go:3:2: undefined: nope", testPass, packagePass}, 0},
		{"a failing package and a failing test", []string{packageFail, testFail}, 1},
		{"repeated events", []string{testPass, testPass, packagePass, packagePass}, 0},
		{"only non-outcome actions", []string{testRun, testOutput}, 0},
		{"an empty log", nil, 0},
		{"several packages out of order", []string{
			`{"Action":"pass","Package":"example.com/z","Test":"TestB"}`,
			`{"Action":"pass","Package":"example.com/a"}`,
			`{"Action":"pass","Package":"example.com/z","Test":"TestA"}`,
			`{"Action":"pass","Package":"example.com/z"}`,
		}, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			var raw []byte
			if len(c.lines) > 0 {
				raw = log(c.lines...)
			}
			byShell := runJQ(t, program, raw, c.exit)
			byGo := Summarise(raw, c.exit)

			for _, field := range []struct {
				name       string
				shell, got int
			}{
				{"exit_code", byShell.ExitCode, byGo.Receipt.ExitCode},
				{"unparsed_line_count", byShell.UnparsedLineCount, byGo.Receipt.UnparsedLineCount},
				{"package_pass_count", byShell.PackagePassCount, byGo.Receipt.PackagePassCount},
				{"package_fail_count", byShell.PackageFailCount, byGo.Receipt.PackageFailCount},
				{"passed_test_count", byShell.PassedTestCount, byGo.Receipt.PassedTestCount},
				{"skipped_test_count", byShell.SkippedTestCount, byGo.Receipt.SkippedTestCount},
				{"failed_test_count", byShell.FailedTestCount, byGo.Receipt.FailedTestCount},
			} {
				if field.shell != field.got {
					t.Errorf("%s: shell says %d, Go says %d", field.name, field.shell, field.got)
				}
			}
			if byShell.Result != byGo.Receipt.Result {
				t.Errorf("result: shell says %q, Go says %q", byShell.Result, byGo.Receipt.Result)
			}

			// The ORDER of the events matters as much as the set: the receipt
			// records a digest over them, so two implementations that agree on
			// the counts and disagree on the order produce receipts that look
			// different for identical runs.
			if len(byShell.Normalized) != len(byGo.Events) {
				t.Fatalf("shell produced %d events, Go produced %d", len(byShell.Normalized), len(byGo.Events))
			}
			for i := range byShell.Normalized {
				if !sameEvent(byShell.Normalized[i], byGo.Events[i]) {
					t.Fatalf("event %d: shell %s, Go %s", i,
						describe(byShell.Normalized[i]), describe(byGo.Events[i]))
				}
			}
		})
	}
}

func sameEvent(a, b Event) bool {
	if a.Package != b.Package || a.Action != b.Action {
		return false
	}
	if (a.Test == nil) != (b.Test == nil) {
		return false
	}
	return a.Test == nil || *a.Test == *b.Test
}

func describe(e Event) string {
	test := "<package>"
	if e.Test != nil {
		test = *e.Test
	}
	return e.Package + "/" + test + ":" + e.Action
}

// extractJQProgram lifts the jq expression out of the script between the
// invocation that opens it and the redirection that closes it.
func extractJQProgram(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(unitScript))
	if err != nil {
		t.Fatalf("read %s: %v.\nIf the script was deleted deliberately, delete this comparison "+
			"in the same commit rather than letting it skip", unitScript, err)
	}
	const opener = `--argjson exit_code "${exit_code}" '`
	start := strings.Index(string(raw), opener)
	if start < 0 {
		t.Fatalf("%s no longer opens its jq program with %q, so this comparison cannot find "+
			"the code it is meant to be checking", unitScript, opener)
	}
	rest := string(raw)[start+len(opener):]
	end := strings.Index(rest, "\n    ' \"${log}\"")
	if end < 0 {
		t.Fatalf("%s no longer closes its jq program where this comparison expects", unitScript)
	}
	program := rest[:end]
	if strings.Count(program, "$normalized") < 2 {
		t.Fatalf("the extracted jq program does not look like the summariser: %q", program)
	}
	return program
}

func runJQ(t *testing.T, program string, raw []byte, exitCode int) shellSummary {
	t.Helper()
	command := exec.Command("jq", "-R", "-s", "--argjson", "exit_code", strconv.Itoa(exitCode), program)
	command.Stdin = strings.NewReader(string(raw))
	var complaint strings.Builder
	command.Stderr = &complaint
	out, err := command.Output()
	if err != nil {
		t.Fatalf("run the shell's jq program: %v\n%s", err, complaint.String())
	}
	var summary shellSummary
	if err := json.Unmarshal(out, &summary); err != nil {
		t.Fatalf("parse the shell's output: %v\n%s", err, out)
	}
	// Result is decoded separately because the field name collides with
	// nothing but is worth reading explicitly rather than trusting a tag.
	var loose map[string]any
	if err := json.Unmarshal(out, &loose); err != nil {
		t.Fatal(err)
	}
	result, _ := loose["result"].(string)
	summary.Result = result
	return summary
}

func requireJQ(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Fatalf("jq is required to compare against the shell implementation: %v", err)
	}
}
