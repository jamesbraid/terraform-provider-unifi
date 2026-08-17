package controllerdifferential

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

const diagnosticsScript = "../../.woodpecker/scripts/catalog-controller-diagnostics.sh"

// TestEveryIdentifierIsRedacted. Each of the four is a thing this repository
// says must never leave it, and a CI log is the least private place a failure
// goes.
func TestEveryIdentifierIsRedacted(t *testing.T) {
	for _, c := range []struct {
		name, in, mustNotContain, mustContain string
	}{
		{"a controller URL", "dial https://unifi.example.internal:8443/api failed",
			"unifi.example.internal", "[redacted-url]"},
		{"an http URL", "GET http://10.0.0.4/status", "10.0.0.4", "[redacted-url]"},
		{"the runner workspace", "open /woodpecker/src/git.example.dev/infra/provider/x.go",
			"git.example.dev", "[redacted-workspace]"},
		{"a MAC address", "device 00:1A:2B:3C:4D:5E did not appear", "00:1A:2B", "[redacted-mac]"},
		{"a lowercase MAC", "device aa:bb:cc:dd:ee:ff missing", "aa:bb:cc", "[redacted-mac]"},
		{"an IPv4 address", "no route to 192.168.7.31", "192.168.7.31", "[redacted-ip]"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := Redact(c.in)
			if strings.Contains(got, c.mustNotContain) {
				t.Fatalf("%q survived redaction: %q", c.mustNotContain, got)
			}
			if !strings.Contains(got, c.mustContain) {
				t.Fatalf("redacted to %q, want it to contain %q", got, c.mustContain)
			}
		})
	}
}

// TestTheURLIsRedactedBeforeTheAddressInsideIt pins the ORDER, which is the one
// thing about this set that is not obvious.
//
// The URL pattern is greedy to whitespace, so it must run before the address
// patterns. Reversed, the IP inside a URL is replaced first and the URL no
// longer matches its own pattern -- leaving the host, the port and the path in
// the log with a redacted address in the middle, which reads as redacted and is
// not.
func TestTheURLIsRedactedBeforeTheAddressInsideIt(t *testing.T) {
	got := Redact("connect https://192.168.7.31:8443/api/s/default/rest/network failed")
	if strings.Contains(got, "8443") || strings.Contains(got, "/api/") {
		t.Fatalf("the URL survived as %q; the address inside it was replaced first and the host, "+
			"port and path were left behind", got)
	}
	if !strings.Contains(got, "[redacted-url]") {
		t.Fatalf("redacted to %q", got)
	}
}

// TestOnlyTheNamedTestsOutputIsReturned. The diagnostics are printed per failed
// test; returning another test's lines would attribute one failure's output to
// another.
func TestOnlyTheNamedTestsOutputIsReturned(t *testing.T) {
	log := diagnosticLog(t,
		event{"output", "TestAccOne", "one says https://a.internal/x\n"},
		event{"output", "TestAccTwo", "two says https://b.internal/y\n"},
		event{"run", "TestAccOne", ""},
		event{"output", "", "package level line\n"},
	)
	got := SanitiseDiagnostics(log, "TestAccOne")
	if len(got) != 1 {
		t.Fatalf("returned %d line(s): %v", len(got), got)
	}
	if strings.Contains(got[0], "two says") || strings.Contains(got[0], "package level") {
		t.Fatalf("returned another subject's output: %q", got[0])
	}
	if strings.Contains(got[0], "a.internal") {
		t.Fatalf("the URL was returned unredacted: %q", got[0])
	}
}

// TestSanitiseAgreesWithTheShell runs the deployed script over the same log.
//
// DELETE THIS TEST IN THE COMMIT THAT DELETES THE SCRIPT. It does not skip when
// the script is missing.
func TestSanitiseAgreesWithTheShell(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Fatalf("jq is required to run the shell implementation: %v", err)
	}
	log := diagnosticLog(t,
		event{"output", "TestAccOne", "dial https://unifi.example.internal:8443/api/s/default failed\n"},
		event{"output", "TestAccOne", "device 00:1A:2B:3C:4D:5E at 192.168.7.31 did not appear\n"},
		event{"output", "TestAccOne", "open /woodpecker/src/git.example.dev/infra/provider/x.go\n"},
		event{"output", "TestAccTwo", "another test's line https://other.internal/z\n"},
	)

	command := exec.Command("bash", diagnosticsScript, "TestAccOne")
	command.Stdin = strings.NewReader(string(log))
	out, err := command.Output()
	if err != nil {
		t.Fatalf("run %s: %v.\nIf it was deleted deliberately, delete this test in the same "+
			"commit rather than letting it skip", diagnosticsScript, err)
	}
	// BLANK LINES ARE DROPPED FROM BOTH SIDES, and that is a declared
	// divergence rather than a loosened comparison. Each Output value already
	// ends in a newline, and `jq -r` adds its own, so the shell emits an empty
	// line after every real one. Three lines of diagnostics come out as five.
	// The redacted CONTENT is identical; only the doubling differs, and
	// reproducing it would be preserving an artifact of how the shell printed
	// rather than anything the reader wants.
	byShell := nonEmpty(strings.Split(string(out), "\n"))
	byGo := nonEmpty(SanitiseDiagnostics(log, "TestAccOne"))

	if len(byShell) != len(byGo) {
		t.Fatalf("shell returned %d line(s), Go returned %d:\nshell %q\ngo    %q",
			len(byShell), len(byGo), byShell, byGo)
	}
	for i := range byShell {
		if byGo[i] != byShell[i] {
			t.Fatalf("line %d:\n  shell %q\n  go    %q", i, byShell[i], byGo[i])
		}
	}
}

type event struct{ action, test, output string }

func diagnosticLog(t *testing.T, events ...event) []byte {
	t.Helper()
	var lines []string
	for _, e := range events {
		record := map[string]any{"Action": e.action, "Package": "unifi", "Output": e.output}
		if e.test != "" {
			record["Test"] = e.test
		}
		encoded, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(encoded))
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func nonEmpty(lines []string) []string {
	out := []string{}
	for _, line := range lines {
		if trimmed := strings.TrimRight(line, "\n"); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
