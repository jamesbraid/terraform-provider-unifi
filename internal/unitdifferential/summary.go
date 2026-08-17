// Package unitdifferential turns `go test -json` output into the receipt the
// catalog unit differential records.
//
// It replaces a twenty-eight-line jq program embedded in
// .woodpecker/scripts/catalog-unit-differential.sh. That program was the whole
// judgement of the gate -- which runs count as passes, what makes a suite fail,
// whether the log was even parsed -- and nothing could call it, so nothing ever
// asserted what it did with a log that was truncated, interleaved, or carrying
// a build error.
package unitdifferential

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

// Event is one outcome the test log reports, reduced to the three fields the
// receipt is derived from.
//
// Test is a pointer because a package-level result has none, and the
// distinction is load-bearing: package_pass_count counts entries WITHOUT a
// test and passed_test_count counts entries WITH one. Collapsing null to the
// empty string would file a package's own result under a test named "".
type Event struct {
	Package string  `json:"package"`
	Test    *string `json:"test"`
	Action  string  `json:"action"`
}

// eventKey is Event made comparable, so deduplication is a map lookup rather
// than a scan. Kept unexported: the pointer in Event is what the receipt's JSON
// needs, and this is only how the set is held.
type eventKey struct {
	pkg     string
	test    string
	hasTest bool
	action  string
}

func (k eventKey) event() Event {
	event := Event{Package: k.pkg, Action: k.action}
	if k.hasTest {
		name := k.test
		event.Test = &name
	}
	return event
}

// Summary is what one suite run produced.
type Summary struct {
	Receipt catalogparity.UnitSuiteReceipt
	// Events is the deduplicated, sorted outcome set the counts were derived
	// from. It is returned rather than kept private because the receipt records
	// a digest of it, and a digest of something the caller cannot see is a
	// number nobody can check.
	Events []Event
}

// Summarise reduces a `go test -json` log to one suite's receipt.
//
// THE FOUR CONDITIONS ARE DIFFERENT QUESTIONS and all four must hold for a
// pass:
//
//   - the command exited zero,
//   - no reported outcome was a failure,
//   - every non-empty line parsed as JSON,
//   - and at least one package actually passed.
//
// The third looks redundant and is not. The suite's stderr is redirected into
// the same log, so a build error, a panic outside a test, or a toolchain
// complaint arrives as text rather than as an event. Those lines produce no
// failing outcome at all: the failure count stays zero while the log describes
// a run that partly did not happen. Reporting them as unparsed_line_count is
// what stops "no failures" from meaning "nothing was read".
//
// THE FOURTH IS A FLOOR ON THE MEASUREMENT RATHER THAN ON THE FAILURES, and it
// came from the shell after this port was written -- caught by the differential
// against the deployed jq program rather than by reading. The other three all
// hold over an empty run: exit zero, no failing events, and every line parsed
// because there were no lines. So a `go test ./...` that produced nothing was
// indistinguishable from a full green suite, in a receipt catalog admission
// consumes. The assertion that stood between us and that hollow pass lived in
// catalog-unit-differential_test.sh, which no pipeline invokes.
func Summarise(raw []byte, exitCode int) Summary {
	var lineCount, parsedCount int
	seen := map[eventKey]bool{}
	var keys []eventKey

	for _, line := range bytes.Split(raw, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		lineCount++
		var reported struct {
			Package string  `json:"Package"`
			Test    *string `json:"Test"`
			Action  string  `json:"Action"`
		}
		if err := json.Unmarshal(line, &reported); err != nil {
			continue
		}
		parsedCount++
		switch reported.Action {
		case "pass", "fail", "skip":
		default:
			continue
		}
		key := eventKey{pkg: reported.Package, action: reported.Action}
		if reported.Test != nil {
			key.test, key.hasTest = *reported.Test, true
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}

	sortKeys(keys)
	events := make([]Event, 0, len(keys))
	for _, key := range keys {
		events = append(events, key.event())
	}

	summary := Summary{Events: events}
	summary.Receipt.ExitCode = exitCode
	summary.Receipt.UnparsedLineCount = lineCount - parsedCount
	for _, key := range keys {
		switch {
		case !key.hasTest && key.action == "pass":
			summary.Receipt.PackagePassCount++
		case !key.hasTest && key.action == "fail":
			summary.Receipt.PackageFailCount++
		case key.hasTest && key.action == "pass":
			summary.Receipt.PassedTestCount++
		case key.hasTest && key.action == "skip":
			summary.Receipt.SkippedTestCount++
		case key.hasTest && key.action == "fail":
			summary.Receipt.FailedTestCount++
		}
	}

	failed := summary.Receipt.PackageFailCount > 0 || summary.Receipt.FailedTestCount > 0
	if exitCode == 0 && !failed && summary.Receipt.UnparsedLineCount == 0 &&
		summary.Receipt.PackagePassCount > 0 {
		summary.Receipt.Result = "pass"
	} else {
		summary.Receipt.Result = "fail"
	}
	return summary
}

// sortKeys orders by package, then a package-level result BEFORE any test, then
// test name, then action. That is jq's ordering, where null sorts before every
// string -- worth stating because it is the one place this port could silently
// reorder the events the digest is taken over.
func sortKeys(keys []eventKey) {
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.pkg != b.pkg {
			return a.pkg < b.pkg
		}
		if a.hasTest != b.hasTest {
			return !a.hasTest
		}
		if a.test != b.test {
			return a.test < b.test
		}
		return a.action < b.action
	})
}

// RenderEvents produces the bytes the normalized-summary digest is taken over.
//
// Compact and ordered, which is what the jq expression emitted. The BYTES will
// not match the shell's for the same run: encoding/json and jq escape and space
// differently. Nothing compares this digest against anything -- it is recorded
// so two runs of one implementation can be told apart -- and reproducing jq's
// exact output to preserve a number no consumer reads would tie this to the
// formatting of a tool being deleted.
func RenderEvents(events []Event) ([]byte, error) {
	if events == nil {
		events = []Event{}
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(events); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// Digest is the sha256 the receipt records for a blob.
func Digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
