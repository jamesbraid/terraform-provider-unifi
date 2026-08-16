// Package schemaparity decides whether a candidate provider schema may differ
// from the released one, and by exactly what.
//
// It exists because the question was previously answered by `cmp`. A byte
// comparison can say "these differ at char 139633" and nothing else: not which
// attribute, not how many, not whether anyone meant it. That gap cost a full
// evening of uncertainty over a difference that turned out to be six leaves.
//
// Every function here is pure over parsed JSON and a ledger. Nothing in this
// package builds a provider, executes terraform or tofu, or reads the network.
// That is deliberate and it is the point: the assertions can then run in the
// fast loop on every push, rather than only inside a manual pipeline that has
// to be triggered by hand. The gate that only ran manually is how the drift
// reached a release candidate unnoticed.
package schemaparity

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EvidenceKind separates how a claim was obtained from what it argues.
//
// It is NOT a verdict and must not be used as one. An entry can carry two
// measured items and still have no measurement of the defect it fixes:
// unifi_wlan.network_id does exactly that today, measuring the released
// schema's own declaration and the ABSENCE of the attribute from recorded
// failures, while the claim that the defect occurs is inferred. A rule that
// counted measured items would license that entry, which is the one whose
// author took the trouble to write "this is stated as the limit of the
// evidence, not as support for the change".
//
// Expressing "measured THAT THE DEFECT OCCURS" needs a second axis this schema
// does not have -- what an item supports, as opposed to how it was obtained.
// Adding one is a ledger change, not something to approximate here.
type EvidenceKind string

const (
	Measured EvidenceKind = "measured"
	Inferred EvidenceKind = "inferred"
)

// Evidence is one supporting claim behind a declared change.
type Evidence struct {
	Claim  string       `json:"claim"`
	Kind   EvidenceKind `json:"kind"`
	Source string       `json:"source"`
}

// Entry licenses ONE named transition of one field of one attribute.
//
// Old and New both matter. An entry names a transition, not an attribute, so a
// later drift to a third value stops matching and fails the gate again. That is
// what stops a declaration becoming a permanent hole.
type Entry struct {
	Surface        string     `json:"surface"`
	Attribute      string     `json:"attribute"`
	Field          string     `json:"field"`
	Old            string     `json:"old"`
	New            string     `json:"new"`
	WhyNotBreaking string     `json:"why_not_breaking"`
	Evidence       []Evidence `json:"evidence"`
}

// Ledger is the declared set of schema changes between two released versions.
type Ledger struct {
	FormatVersion int     `json:"format_version"`
	FromVersion   string  `json:"from_version"`
	ToVersion     string  `json:"to_version"`
	Entries       []Entry `json:"entries"`
}

// LoadLedger reads and validates a ledger.
//
// A missing file is not an error and yields an empty ledger: most of the time
// there is nothing to declare, and requiring an empty file would be ceremony. A
// file that EXISTS and cannot be parsed IS an error, because treating it as
// empty would turn every declared change back into a failure with no hint why.
//
// Errors are returned rather than reported to a *testing.T, which is the whole
// reason this is a package and not a test helper: the same validation has to
// serve the gate, the unit tests and any future tool.
func LoadLedger(path string) (Ledger, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, os.ErrNotExist) {
		return Ledger{}, nil
	}
	if err != nil {
		return Ledger{}, fmt.Errorf("read %s: %w", path, err)
	}
	var ledger Ledger
	if err := json.Unmarshal(raw, &ledger); err != nil {
		return Ledger{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := ledger.Validate(path); err != nil {
		return Ledger{}, err
	}
	return ledger, nil
}

// Validate refuses entries that license nothing or argue nothing.
//
// Every message names its subject -- file, index, surface, attribute and field
// -- because a validation failure that says only "invalid entry" leaves the
// reader to find it, and the ledger is edited by hand under time pressure.
func (l Ledger) Validate(where string) error {
	var problems []string
	for index, entry := range l.Entries {
		at := fmt.Sprintf("%s entry %d (%s.%s %s)", where, index, entry.Surface, entry.Attribute, entry.Field)
		switch {
		case entry.Surface == "" || entry.Attribute == "" || entry.Field == "":
			problems = append(problems, at+": surface, attribute and field are all required")
		case entry.Old == entry.New:
			problems = append(problems, at+": old and new are identical, so it licenses nothing")
		case entry.WhyNotBreaking == "":
			problems = append(problems, at+": why_not_breaking is empty. An entry that does not "+
				"argue its own safety is a rubber stamp, which is the failure mode this exists to avoid")
		case len(entry.Evidence) == 0:
			problems = append(problems, at+": no evidence. State what was observed and what was reasoned")
		}
		for j, ev := range entry.Evidence {
			if ev.Kind != Measured && ev.Kind != Inferred {
				problems = append(problems, fmt.Sprintf("%s evidence %d: kind is %q, want %q or %q. "+
					"A reader has to know which claims were observed", at, j, ev.Kind, Measured, Inferred))
			}
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("schema change ledger is invalid:\n  %s", strings.Join(problems, "\n  "))
	}
	return nil
}

// Licenses reports whether this exact transition is declared.
//
// Matching on both old and new is the point -- see Entry.
func (l Ledger) Licenses(d Difference) bool {
	for _, e := range l.Entries {
		if e.Surface == d.Surface && e.Attribute == d.Attribute && e.Field == d.Field &&
			e.Old == d.Old && e.New == d.New {
			return true
		}
	}
	return false
}
