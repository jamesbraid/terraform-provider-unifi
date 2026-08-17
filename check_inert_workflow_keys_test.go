package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// inertWorkflowKeys are keys Woodpecker PARSES AND IGNORES. A declaration using
// one is not configuration, it is a claim that enforces nothing -- and it reads
// as configuration to everyone who follows.
//
// RE-DERIVE THIS LIST, DO NOT TRUST IT:
//
//	woodpecker-cli lint .woodpecker/<file>.yml
//
// Anything reported as "Additional property X is not allowed" belongs here. That
// command is not run by this test on purpose: woodpecker-cli appears nowhere in
// .woodpecker/ and the step image is golang:1.25.8-bookworm, so invoking it
// would mean fetching a release tarball on the critical path of every push to
// re-learn something already measured.
//
// timeout, measured 2026-08-16 against woodpecker-cli 3.16.0:
//
//	step level  steps.0   Additional property timeout is not allowed
//	root level  (root)    Additional property timeout is not allowed
//
// Both rejected, so there is no correct placement in this file for it. The
// enforcing timeout is per-repository server state, reachable only as
// `woodpecker-cli repo update --timeout`, which is task 50's problem: live
// configuration with no declared source.
var inertWorkflowKeys = map[string]string{
	"timeout": "parsed and ignored at every level; the real limit is per-repository server state",
}

// TestNoWorkflowDeclaresAnInertKey answers: DOES ANY WORKFLOW CONFIGURE
// SOMETHING WOODPECKER WILL SILENTLY DISCARD?
//
// Pipeline 220 ran seventeen minutes on a step declaring `timeout: 8m` and was
// never killed; it stopped because someone superseded it. Five such declarations
// existed across four of the six workflows, and one carried four lines of
// careful reasoning about why the limit had been RAISED from 5m to 8m -- correct
// reasoning about the work the step does, attached to a mechanism that has never
// existed.
//
// A JUSTIFICATION ATTACHED TO A KEY THAT DOES NOTHING IS WORSE THAN NO
// JUSTIFICATION, because it is the thing that stops the next reader checking.
//
// This does not validate the schema. It asserts that keys MEASURED to be
// discarded are absent, which is why the denylist is short and carries the
// command that regenerates it: the alternative -- reimplementing Woodpecker's
// grammar here -- would put the rule in two places that can disagree, which is
// the defect the check exists to prevent.
//
// PROVEN TO FAIL, four ways, each with its own message: re-adding `timeout: 8m`
// under fast-loop's step image reports `.woodpecker/fast-loop.yml:47 declares
// "timeout"`; breaking the control matcher reports the scanner cannot see key
// positions; globbing a directory that does not exist reports it read nothing;
// and the four comments naming a timeout stay clean throughout.
func TestNoWorkflowDeclaresAnInertKey(t *testing.T) {
	workflows, err := filepath.Glob(filepath.Join(".woodpecker", "*.yml"))
	if err != nil {
		t.Fatalf("glob .woodpecker: %v", err)
	}
	sort.Strings(workflows)

	// Without this, a moved or renamed directory makes the loop below iterate
	// nothing and report success over a tree it never opened.
	if len(workflows) == 0 {
		t.Fatal(".woodpecker contains no workflows, so this test would pass without reading anything")
	}

	keys := make([]string, 0, len(inertWorkflowKeys))
	for key := range inertWorkflowKeys {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	matcher := regexp.MustCompile(`^\s*(` + strings.Join(keys, "|") + `):`)

	// The scanner skips comments, so a matcher that had stopped working -- a bad
	// pattern, a changed comment convention -- would report every file clean.
	// `image:` is a key every workflow declares, so finding it proves the scanner
	// still sees key positions at all.
	control := regexp.MustCompile(`^\s*image:`)
	found := 0
	for _, workflow := range workflows {
		found += len(scanKeys(t, workflow, control))
	}
	if found == 0 {
		t.Fatal("control: the scanner found no `image:` key in any workflow, so it cannot see key positions and every case below would pass vacuously")
	}

	for _, workflow := range workflows {
		for _, hit := range scanKeys(t, workflow, matcher) {
			t.Errorf("%s:%d declares %q, which Woodpecker parses and ignores -- %s",
				workflow, hit.line, hit.key, inertWorkflowKeys[hit.key])
		}
	}
}

type keyHit struct {
	line int
	key  string
}

// scanKeys reports where pattern matches a key position, skipping comment lines
// so prose that mentions a key by name is not mistaken for declaring it. Four
// such comments exist today and every one of them should survive this check.
func scanKeys(t *testing.T, path string, pattern *regexp.Regexp) []keyHit {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var hits []keyHit
	for index, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if match := pattern.FindStringSubmatch(line); match != nil {
			key := ""
			if len(match) > 1 {
				key = match[1]
			}
			hits = append(hits, keyHit{line: index + 1, key: key})
		}
	}
	return hits
}
