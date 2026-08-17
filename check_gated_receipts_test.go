package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestEveryGatedReceiptIsProducedFirst is #153 with the arrow reversed.
//
// #153 was a workflow invoking a script that had been deleted. This is the
// mirror image: a gate asserting on a receipt whose PRODUCER has been deleted,
// leaving it to read a path nothing writes any more.
//
// NEITHER go test NOR THE GATE ITSELF CAN SEE IT. The gate is correct, the file
// is simply never created, and `go test` never looks at a workflow. The failure
// surfaces mid-pipeline as a missing file, which reads as a broken run rather
// than as an assertion that lost its subject -- and the pipelines that would
// show it are event: manual, so nobody would see it until a campaign.
//
// This is live rather than hypothetical: the scripts that write these paths are
// being deleted right now as their judgement moves into Go. I checked it by hand
// after each merge; a test does it on every push and does not depend on my
// remembering.
//
// WITHIN ONE WORKFLOW AND IN ORDER, both deliberate. Woodpecker steps in a
// workflow share a filesystem and run top to bottom, so a producer below its
// consumer is as broken as an absent one -- pipeline 190 died exactly that way,
// on a cache populated fourteen lines after the step that needed it.
func TestEveryGatedReceiptIsProducedFirst(t *testing.T) {
	workflows, err := filepath.Glob(filepath.Join(".woodpecker", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(workflows) == 0 {
		t.Fatal("no workflows found, so this check would assert nothing")
	}

	// Both spellings a producer uses today. A path written by neither is not a
	// producer; a path written by both is fine.
	produces := regexp.MustCompile(`(?:-output|[A-Z][A-Z0-9_]*_OUTPUT=)\s?(/tmp/[a-z0-9.-]+\.json)`)
	consumes := regexp.MustCompile(`-receipt\s+(/tmp/[a-z0-9.-]+\.json)`)

	gated := 0
	var orphaned []string
	for _, workflow := range workflows {
		body, err := os.ReadFile(workflow)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(body), "\n")

		// Producers are collected with the line they appear on, so ordering can
		// be checked rather than just membership.
		producedAt := map[string]int{}
		for index, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			for _, match := range produces.FindAllStringSubmatch(line, -1) {
				if _, seen := producedAt[match[1]]; !seen {
					producedAt[match[1]] = index
				}
			}
		}

		for index, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			for _, match := range consumes.FindAllStringSubmatch(line, -1) {
				gated++
				path := match[1]
				at, produced := producedAt[path]
				switch {
				case !produced:
					orphaned = append(orphaned, fmt.Sprintf(
						"%s:%d asserts on %s, which nothing in that workflow writes",
						filepath.Base(workflow), index+1, path))
				case at > index:
					orphaned = append(orphaned, fmt.Sprintf(
						"%s:%d asserts on %s, which is not written until line %d",
						filepath.Base(workflow), index+1, path, at+1))
				}
			}
		}
	}
	sort.Strings(orphaned)

	// Without this the check passes by finding no gates, which is the state it
	// would be in if the flag were ever renamed.
	if gated == 0 {
		t.Fatal("no gated receipts were found; the pattern is not matching the workflows, " +
			"so a missing producer would go unreported")
	}

	if len(orphaned) > 0 {
		t.Errorf("%d gate(s) assert on a receipt that is not produced above them:\n    %s\n\n"+
			"    A gate whose producer was deleted still passes its own tests and fails\n"+
			"    mid-pipeline as a missing file, which reads as a broken run rather than as\n"+
			"    an assertion that lost its subject. Either restore the producer or remove\n"+
			"    the gate -- and note which of the two is the decision.",
			len(orphaned), strings.Join(orphaned, "\n    "))
	}
	t.Logf("%d gated receipt(s) across %d workflow(s), each produced above its gate", gated, len(workflows))
}
