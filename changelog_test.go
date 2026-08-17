package main

import (
	"os"
	"strings"
	"testing"
)

// TestEveryChangelogSectionIsTerminated guards the one thing that decides what a
// GitHub release actually says.
//
// update-release-notes.yaml extracts a release's notes with
//
//	awk "/^## \[${tag}\]/{found=1; next} found && /^---$/{exit} found{print}"
//
// so a section ends at a line that is exactly "---" and NOT at the next "## ["
// header. A section written without a separator therefore publishes itself AND
// every section below it, down to the next separator or the end of the file.
//
// This is not hypothetical and it is not only about new entries. When this test
// was written nine sections were unterminated, and the published notes for
// v0.101.2 contained nine later releases, v0.101.1 contained eight and v0.101.0
// seven. The v0.102.0 entry was written without one too and would have shipped
// three older releases inside itself; that was caught by running the workflow's
// own awk against the file rather than by reading the file.
//
// The check reads the same rule the workflow does. It cannot notice a change to
// the workflow's extraction, which is the residual risk and the reason the rule
// is quoted above rather than only described.
func TestEveryChangelogSectionIsTerminated(t *testing.T) {
	const path = "CHANGELOG.md"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")

	var headings []int
	for i, line := range lines {
		if strings.HasPrefix(line, "## [") {
			headings = append(headings, i)
		}
	}
	// A file with no sections would pass every assertion below without making
	// any of them, which is the failure this whole family of checks keeps
	// producing.
	if len(headings) < 2 {
		t.Fatalf("found %d release sections in %s; with fewer than two there is no boundary to check "+
			"and this test would pass without examining anything", len(headings), path)
	}

	terminated := 0
	for at, start := range headings {
		// The last section is terminated by end of file, which awk handles.
		if at == len(headings)-1 {
			terminated++
			continue
		}
		next := headings[at+1]
		found := false
		for _, line := range lines[start+1 : next] {
			if strings.TrimSpace(line) == "---" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: %s is not terminated by a --- before %s.\n"+
				"    update-release-notes.yaml ends a section at a line that is exactly ---, not at\n"+
				"    the next heading, so this release's published notes contain every section below\n"+
				"    it down to the next separator.",
				path, strings.TrimSpace(lines[start]), strings.TrimSpace(lines[next]))
			continue
		}
		terminated++
	}

	t.Logf("%d of %d release sections are terminated", terminated, len(headings))
}
