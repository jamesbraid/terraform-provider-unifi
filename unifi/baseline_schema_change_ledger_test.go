package unifi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// THE SCHEMA-CHANGE LEDGER.
//
// TestBuiltSchemaMatchesReleasedBaseline treats the released v0.101.2 schema as
// authoritative for every field of every attribute. Until this existed there was
// NO way to accept a deliberate change: the failure message told you to record it
// in provider-codegen/migrations/v0.101.2-to-next.json, but that file is a
// state-migration policy, the test never reads it, and MigrationOverride has no
// field capable of expressing a description. Recording a valid override there
// left the gate failing unchanged. So the message named a remedy with no causal
// connection to the check printing it.
//
// This is that remedy, built.
//
// AN ENTRY LICENSES ONE NAMED CHANGE, NOT AN ATTRIBUTE. It carries the OLD and
// the NEW value and both must match, so drifting to a third value fails again.
// That is the difference between a ledger and a blanket exemption, and it is the
// property most worth protecting: the failure mode to design against is an entry
// that quietly stops looking.
//
// AN ENTRY MUST ALSO SAY WHY THE CHANGE CANNOT BREAK AN EXISTING CONFIGURATION,
// with evidence, and label each claim measured or inferred. The gate compares
// fields; fields cannot distinguish benign from breaking. The released contract
// can, and a reviewer needs to see which half of the argument was observed.
const schemaChangeLedgerPath = "../provider-codegen/schema-changes/v0.101.2-to-next.json"

type schemaChangeEvidence struct {
	Claim  string `json:"claim"`
	Kind   string `json:"kind"` // "measured" or "inferred"
	Source string `json:"source"`
}

type schemaChangeEntry struct {
	Surface        string                 `json:"surface"`
	Attribute      string                 `json:"attribute"`
	Field          string                 `json:"field"`
	Old            string                 `json:"old"`
	New            string                 `json:"new"`
	WhyNotBreaking string                 `json:"why_not_breaking"`
	Evidence       []schemaChangeEvidence `json:"evidence"`
}

type schemaChangeLedger struct {
	FormatVersion int                 `json:"format_version"`
	FromVersion   string              `json:"from_version"`
	ToVersion     string              `json:"to_version"`
	Entries       []schemaChangeEntry `json:"entries"`
}

// loadSchemaChangeLedger reads the ledger and refuses malformed entries.
//
// A missing file is not an error: most of the time there is nothing to declare,
// and requiring an empty file would be ceremony. A file that exists and cannot
// be parsed IS an error, because silently treating it as empty would turn every
// declared change back into a failure with no hint why.
func loadSchemaChangeLedger(t *testing.T) []schemaChangeEntry {
	t.Helper()

	raw, err := os.ReadFile(filepath.Clean(schemaChangeLedgerPath))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("read %s: %v", schemaChangeLedgerPath, err)
	}

	var ledger schemaChangeLedger
	if err := json.Unmarshal(raw, &ledger); err != nil {
		t.Fatalf("parse %s: %v", schemaChangeLedgerPath, err)
	}

	for i, e := range ledger.Entries {
		where := fmt.Sprintf("%s entry %d (%s.%s %s)", schemaChangeLedgerPath, i, e.Surface, e.Attribute, e.Field)
		switch {
		case e.Surface == "" || e.Attribute == "" || e.Field == "":
			t.Errorf("%s: surface, attribute and field are all required", where)
		case e.Old == e.New:
			t.Errorf("%s: old and new are identical, so it licenses nothing", where)
		case e.WhyNotBreaking == "":
			t.Errorf("%s: why_not_breaking is empty. An entry that does not argue its own "+
				"safety is a rubber stamp, which is the failure mode this mechanism exists "+
				"to avoid", where)
		case len(e.Evidence) == 0:
			t.Errorf("%s: no evidence. State what was observed and what was reasoned", where)
		}
		for j, ev := range e.Evidence {
			if ev.Kind != "measured" && ev.Kind != "inferred" {
				t.Errorf("%s evidence %d: kind is %q, want \"measured\" or \"inferred\". "+
					"A reader has to know which claims were observed", where, j, ev.Kind)
			}
		}
	}
	return ledger.Entries
}

// schemaChangeDeclared reports whether this exact transition is licensed.
//
// Matching on BOTH old and new is the whole point: an entry names one text, not
// an attribute. If the built schema drifts to a third value the entry stops
// matching and the gate fails again, which is what stops a declaration becoming
// a permanent hole.
func schemaChangeDeclared(entries []schemaChangeEntry, surface, attribute, field, want, have string) bool {
	for _, e := range entries {
		if e.Surface == surface && e.Attribute == attribute && e.Field == field &&
			e.Old == want && e.New == have {
			return true
		}
	}
	return false
}
