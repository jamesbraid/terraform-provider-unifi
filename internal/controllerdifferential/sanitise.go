package controllerdifferential

import (
	"bytes"
	"encoding/json"
	"regexp"
)

// The four redactions catalog-controller-diagnostics.sh applied, in its order.
//
// THIS IS NOT COSMETIC. A controller suite's output carries the controller's
// URL, the runner's workspace path, the synthetic fleet's MAC addresses and the
// container network's addresses, and this repository's rule is that nothing
// published carries a homelab identifier. The diagnostics are printed to a CI
// log, which is the least private place a failure goes.
//
// Ordered as the shell ordered them: the URL pattern is greedy to whitespace or
// a quote, so it must run before the address patterns or an address inside a
// URL is replaced first and the URL no longer matches.
var redactions = []struct {
	pattern *regexp.Regexp
	with    string
}{
	{regexp.MustCompile(`https?://[^\s"]+`), "[redacted-url]"},
	{regexp.MustCompile(`/woodpecker/src/[^/\s]+`), "[redacted-workspace]"},
	{regexp.MustCompile(`([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}`), "[redacted-mac]"},
	{regexp.MustCompile(`([0-9]{1,3}\.){3}[0-9]{1,3}`), "[redacted-ip]"},
}

// SanitiseDiagnostics returns one test's output lines with identifiers removed.
//
// It replaces .woodpecker/scripts/catalog-controller-diagnostics.sh, which was
// twelve lines of jq and sed and which an earlier note of mine called "absorbed"
// into the differential port. It was not: the port printed the failing test's
// NAME and none of its output, so the redaction had no implementation at all.
// The script survived being called absorbed because nothing compared the two.
func SanitiseDiagnostics(raw []byte, testName string) []string {
	var lines []string
	for _, line := range bytes.Split(raw, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var event struct {
			Action string  `json:"Action"`
			Test   *string `json:"Test"`
			Output string  `json:"Output"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		if event.Action != "output" || event.Test == nil || *event.Test != testName {
			continue
		}
		lines = append(lines, Redact(event.Output))
	}
	return lines
}

// Redact applies the four substitutions to one string.
//
// Exported so a caller printing anything else derived from a controller run can
// use the same set. Two implementations of "what counts as an identifier" is
// how one of them ends up missing a case.
func Redact(text string) string {
	for _, redaction := range redactions {
		text = redaction.pattern.ReplaceAllString(text, redaction.with)
	}
	return text
}
