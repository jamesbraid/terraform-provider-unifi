package controllerdifferential

import (
	"encoding/json"
	"strings"
	"testing"
)

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
