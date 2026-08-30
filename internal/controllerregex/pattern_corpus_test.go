package controllerregex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// patternCorpusPath is the committed record of what every controller
// pattern in the pinned SDK's constraint tables accepts and rejects. A
// constraint-table text diff alone says a pattern changed, never what it
// now accepts -- this file is what turns that into a reviewable fact.
const patternCorpusPath = "testdata/pattern-corpus.golden"

// updatePatternCorpusEnv regenerates patternCorpusPath. Unlike UPDATE_GOLDEN
// elsewhere in this repo there is deliberately no removal guard: this is a
// full record, not an append-only inventory, and the diff on the committed
// file is the guard -- the same reasoning unifi/schema_snapshot_test.go's
// updateSchemaSnapshotEnv documents for itself.
const updatePatternCorpusEnv = "UPDATE_PATTERN_CORPUS"

// patternCorpus is the fixed set of probe strings every controller pattern
// is checked against. Each entry earns its place for a specific reason --
// see the comments -- rather than as filler: a corpus of only values every
// pattern accepts (or only adversarial reject bait) would produce a golden
// full of identical rows, detecting almost nothing. The last three entries
// are not generic filler either -- they are this cycle's own finding, not a
// synthetic example: FirewallPolicy.protocol's (and three sibling fields')
// unescaped "." in the "ax.25" alternative matches any single character
// there, so "axX25" and "ax:25" were silently valid controller input, a
// fact no reading of the constraint table's own text says.
var patternCorpus = []string{
	"",                       // the empty string -- several patterns special-case it with |^$
	"\n",                     // a bare trailing newline -- \A...\z vs. regexp2's lenient default $
	"5\n",                    // a value many single-char/numeric patterns accept without the newline
	"٣",                      // U+0663, an Arabic-Indic digit -- \d must translate to ASCII-only, not pass through
	"3",                      // the ASCII digit ٣ stands in for -- pairs with it to show a translation, not just a reject
	"café",                   // a non-ASCII letter -- \w must translate to ASCII-only, not pass through
	"cafe",                   // the ASCII spelling of café, for the same reason
	" ",                      // a single space
	"\t",                     // a literal tab
	"a\rb",                   // a bare CR -- pins this package's known, uncorrected "." divergence at its exact effect
	"true",                   // a common boolean-shaped token
	"false",                  // ditto
	"-1",                     // a common negative-number sentinel several patterns special-case
	"0",                      // a common numeric edge value
	"192.168.1.1",            // dotted-quad -- exercises IP-shaped patterns' own alphabet
	"aa:bb:cc:dd:ee:ff",      // colon-hex -- exercises MAC-shaped patterns' own alphabet
	"AA:BB:CC:DD:EE:FF",      // the uppercase form of the same
	"example.com",            // a plain domain -- exercises hostname/domain-shaped patterns' own alphabet
	"-leadinghyphen.com",     // a leading-hyphen label -- the (?<!-)/(?!-) lookaround probe Task 1/2/3 used
	"trailinghyphen-.com",    // a trailing-hyphen label -- the same construct, the other side
	"a-real-key",             // a generic alphanumeric-and-hyphen token
	`!@#$%^&*()`,             // punctuation and symbols
	strings.Repeat("a", 64),  // a common length-cap boundary (many patterns cap at 63/64)
	strings.Repeat("a", 129), // just past the common 128-character cap
	"axX25",                  // this cycle's finding, verbatim -- see the var doc comment above
	"ax:25",                  // ditto
	"ax.25",                  // the literal value the pattern's author actually meant
}

// patternField is one field the pinned SDK's constraint tables say the
// controller validates with a regular expression.
type patternField struct {
	source, typeName, field, pattern string
}

// key identifies a patternField uniquely across both tables. unifi and
// unifi/settings never share a Go type name today (checked directly against
// the pinned SDK), but the source prefix keeps that an invariant this file
// enforces rather than assumes.
func (f patternField) key() string { return f.source + "." + f.typeName + "." + f.field }

// liveFieldPatterns reads every non-empty Pattern out of both of the pinned
// SDK's constraint tables -- unifi.FieldConstraints covers the top-level
// package, unifi/settings.FieldConstraints covers settings types, the same
// split cmd/sdk-bootstrap's sdkConstraints uses for the same reason. Read
// live from the SDK, never copied into a hardcoded list here: a golden
// built from a list someone typed would drift from the SDK silently, which
// is exactly the defect this file exists to prevent.
func liveFieldPatterns() []patternField {
	var out []patternField
	for typeName, fields := range ui.FieldConstraints {
		for fieldName, c := range fields {
			if c.Pattern == "" {
				continue
			}
			out = append(out, patternField{"unifi", typeName, fieldName, c.Pattern})
		}
	}
	for typeName, fields := range settings.FieldConstraints {
		for fieldName, c := range fields {
			if c.Pattern == "" {
				continue
			}
			out = append(out, patternField{"settings", typeName, fieldName, c.Pattern})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key() < out[j].key() })
	return out
}

// corpusRecord is one row of the golden: one SDK field's pattern and its
// accept/reject verdict over patternCorpus, one character per entry, in
// order -- 'A' for accepted, 'R' for rejected, 'E' for a Compile or
// MatchString error (never observed against the live tables as of this
// writing -- see TestPatternCorpusEveryLivePatternCompiles -- but a future
// SDK pattern could use a construct this package refuses, and that has to
// show up as data, not a panic).
type corpusRecord struct {
	Source  string `json:"source"`
	Type    string `json:"type"`
	Field   string `json:"field"`
	Pattern string `json:"pattern"`
	Verdict string `json:"verdict"`
}

func (r corpusRecord) key() string { return r.Source + "." + r.Type + "." + r.Field }

// verdictFor computes pattern's accept/reject verdict over patternCorpus.
func verdictFor(pattern string) string {
	var verdict strings.Builder
	compiled, err := Compile(pattern)
	for _, probe := range patternCorpus {
		if err != nil {
			verdict.WriteByte('E')
			continue
		}
		matched, mErr := compiled.MatchString(probe)
		switch {
		case mErr != nil:
			verdict.WriteByte('E')
		case matched:
			verdict.WriteByte('A')
		default:
			verdict.WriteByte('R')
		}
	}
	return verdict.String()
}

// buildPatternCorpus computes the current, live verdict for every field
// liveFieldPatterns finds.
func buildPatternCorpus(t *testing.T) []corpusRecord {
	t.Helper()
	fields := liveFieldPatterns()
	if len(fields) == 0 {
		t.Fatal("liveFieldPatterns found nothing in unifi.FieldConstraints or " +
			"settings.FieldConstraints -- the SDK's constraint tables are unexpectedly empty")
	}
	out := make([]corpusRecord, 0, len(fields))
	for _, f := range fields {
		out = append(out, corpusRecord{
			Source:  f.source,
			Type:    f.typeName,
			Field:   f.field,
			Pattern: f.pattern,
			Verdict: verdictFor(f.pattern),
		})
	}
	return out
}

// patternCorpusHeader documents the file for whoever opens it without this
// test in front of them, including the corpus itself in verdict-string
// order -- generated from patternCorpus, not duplicated by hand, so it
// cannot drift from the code that actually produced the rows below it.
func patternCorpusHeader() string {
	var b strings.Builder
	b.WriteString("# pattern-corpus.golden\n")
	b.WriteString("#\n")
	b.WriteString("# One JSON object per line (this header's '#' lines are not JSON and are\n")
	b.WriteString("# skipped on read). Each line is one field the pinned SDK's constraint\n")
	b.WriteString("# tables say the controller validates with a regular expression, and its\n")
	b.WriteString("# accept ('A') / reject ('R') / error ('E') verdict over the fixed corpus\n")
	b.WriteString("# below, compiled the way the generated schema actually compiles it --\n")
	b.WriteString("# controllerregex.Compile, the controller's own dialect, not Go's RE2.\n")
	b.WriteString("#\n")
	b.WriteString("# Corpus, in verdict-string order:\n")
	for i, probe := range patternCorpus {
		fmt.Fprintf(&b, "#   %2d: %q\n", i, probe)
	}
	b.WriteString("#\n")
	fmt.Fprintf(&b,
		"# Regenerate with: %s=1 go test ./internal/controllerregex/ -run TestPatternCorpus\n",
		updatePatternCorpusEnv,
	)
	b.WriteString("# Review the diff -- that review is what this file is for -- then commit it.\n")
	b.WriteString("#\n")
	return b.String()
}

// writePatternCorpusGolden rewrites patternCorpusPath in full. No removal
// guard -- see updatePatternCorpusEnv.
func writePatternCorpusGolden(t *testing.T, records []corpusRecord) {
	t.Helper()
	var buf strings.Builder
	buf.WriteString(patternCorpusHeader())
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // patterns carry "<"/">" (e.g. (?<!-)); keep the diff readable, not <-mangled
	for _, r := range records {
		if err := enc.Encode(r); err != nil {
			t.Fatalf("encoding record %s: %v", r.key(), err)
		}
	}
	if err := os.WriteFile(patternCorpusPath, []byte(buf.String()), 0o644); err != nil {
		t.Fatalf("writing %s: %v", patternCorpusPath, err)
	}
	t.Logf("wrote %d records to %s", len(records), patternCorpusPath)
}

// readPatternCorpusGolden reads patternCorpusPath back. A missing golden
// fails with the regeneration command rather than being silently created.
func readPatternCorpusGolden(t *testing.T) []corpusRecord {
	t.Helper()
	f, err := os.Open(patternCorpusPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("%s does not exist yet.\n"+
				"    Run %s=1 go test ./internal/controllerregex/ -run TestPatternCorpus\n"+
				"    to create it, review the diff, and commit it.",
				patternCorpusPath, updatePatternCorpusEnv)
		}
		t.Fatalf("reading %s: %v", patternCorpusPath, err)
	}
	defer f.Close()

	var out []corpusRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var r corpusRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("parsing %s: %v\n    line: %s", patternCorpusPath, err, line)
		}
		out = append(out, r)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanning %s: %v", patternCorpusPath, err)
	}
	return out
}

// diffCorpusRecords compares two record sets and returns one message per
// field whose pattern or verdict differs, each naming the field and exactly
// which corpus strings moved -- not just "golden mismatch": the whole point
// of this file is that a pattern's meaning has to be legible from the
// failure, not just the fact that something changed.
func diffCorpusRecords(want, got []corpusRecord) []string {
	wantByKey := make(map[string]corpusRecord, len(want))
	for _, r := range want {
		wantByKey[r.key()] = r
	}
	gotByKey := make(map[string]corpusRecord, len(got))
	for _, r := range got {
		gotByKey[r.key()] = r
	}

	keys := make(map[string]bool, len(wantByKey)+len(gotByKey))
	for k := range wantByKey {
		keys[k] = true
	}
	for k := range gotByKey {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	var diffs []string
	for _, k := range sorted {
		w, wok := wantByKey[k]
		g, gok := gotByKey[k]
		switch {
		case wok && !gok:
			diffs = append(diffs, fmt.Sprintf("%s: removed (was pattern %q)", k, w.Pattern))
		case !wok && gok:
			diffs = append(diffs, fmt.Sprintf("%s: added (now pattern %q)", k, g.Pattern))
		case w.Pattern != g.Pattern || w.Verdict != g.Verdict:
			diffs = append(diffs, describeVerdictMove(k, w, g))
		}
	}
	return diffs
}

// describeVerdictMove names exactly which corpus strings moved between
// accept, reject and error for one field.
func describeVerdictMove(key string, w, g corpusRecord) string {
	msg := fmt.Sprintf("%s: pattern %q -> %q", key, w.Pattern, g.Pattern)

	var moved []string
	if len(w.Verdict) == len(patternCorpus) && len(g.Verdict) == len(patternCorpus) {
		for i, probe := range patternCorpus {
			if w.Verdict[i] != g.Verdict[i] {
				moved = append(moved, fmt.Sprintf("%q: %c->%c", probe, w.Verdict[i], g.Verdict[i]))
			}
		}
	} else {
		moved = append(moved, fmt.Sprintf(
			"verdict %q -> %q (different length -- patternCorpus itself changed)", w.Verdict, g.Verdict))
	}
	if len(moved) > 0 {
		msg += fmt.Sprintf("\n        verdict moved for: %s", strings.Join(moved, "; "))
	}
	return msg
}

// formatCorpusDiffs renders diffs for a test failure, capped so a change
// that touches most of the table produces a readable report instead of a
// wall of text nobody will read -- the exact failure mode the brief that
// commissioned this file warned against.
func formatCorpusDiffs(diffs []string) string {
	const maxShown = 25
	if len(diffs) <= maxShown {
		return strings.Join(diffs, "\n    ")
	}
	shown := strings.Join(diffs[:maxShown], "\n    ")
	return fmt.Sprintf("%s\n    ... and %d more", shown, len(diffs)-maxShown)
}

// TestPatternCorpus diffs every controller pattern's live accept/reject
// verdict against the committed golden at patternCorpusPath. A future SDK
// bump that changes what a pattern accepts -- not just its text -- fails
// here, naming the field and the strings whose verdict moved, rather than
// leaving that fact to be inferred from a constraint-table text diff.
func TestPatternCorpus(t *testing.T) {
	got := buildPatternCorpus(t)

	if os.Getenv(updatePatternCorpusEnv) != "" {
		writePatternCorpusGolden(t, got)
		return
	}

	want := readPatternCorpusGolden(t)

	if diffs := diffCorpusRecords(want, got); len(diffs) > 0 {
		t.Errorf("the controller patterns' derived accept/reject verdicts disagree with %s at %d field(s):\n    %s\n\n"+
			"    If this change is intended (a controller pattern legitimately changed meaning), run\n"+
			"    %s=1 go test ./internal/controllerregex/ -run TestPatternCorpus,\n"+
			"    review the diff to %s, and commit it.",
			patternCorpusPath, len(diffs), formatCorpusDiffs(diffs), updatePatternCorpusEnv, patternCorpusPath)
	}
}

// TestPatternCorpusEveryLivePatternCompiles pins today's fact that every
// pattern in both live constraint tables compiles under controllerregex --
// so 'E' rows in the golden are a possibility this file's format accounts
// for, not a silent gap. If a future SDK pattern uses a construct this
// package refuses (see controllerregex.go's translator grammar), this test
// is the one that is supposed to start failing, by name.
func TestPatternCorpusEveryLivePatternCompiles(t *testing.T) {
	for _, f := range liveFieldPatterns() {
		if _, err := Compile(f.pattern); err != nil {
			t.Errorf("%s: Compile(%q) error = %v", f.key(), f.pattern, err)
		}
	}
}

// TestPatternCorpusDetectsAMutatedPattern is this file's positive control.
// It reproduces, not invents, the shape of this cycle's own finding: an
// unescaped "." inside one alternative of a big top-level alternation
// (FirewallPolicy.protocol's "ax.25", among three sibling fields) matches
// any single character there, so escaping it to "ax\.25" is a one-character
// pattern-table diff whose behavioural effect -- "axX25" and "ax:25" stop
// validating -- a text diff never states. This asserts the comparison
// mechanism actually catches that, names the field, and names the exact
// strings whose verdict moved -- not just "golden mismatch".
func TestPatternCorpusDetectsAMutatedPattern(t *testing.T) {
	got := buildPatternCorpus(t)

	const needle = "ax.25"
	var target corpusRecord
	var found bool
	for _, r := range got {
		if strings.Contains(r.Pattern, needle) {
			target = r
			found = true
			break // got is sorted by key(), so this is deterministic
		}
	}
	if !found {
		t.Fatal("no live field's pattern contains \"ax.25\" any more; this control's " +
			"mutation no longer applies and needs a new pattern to mutate")
	}

	mutated := append([]corpusRecord(nil), got...)
	for i, r := range mutated {
		if r.key() != target.key() {
			continue
		}
		escaped := strings.Replace(r.Pattern, needle, `ax\.25`, 1)
		if escaped == r.Pattern {
			t.Fatalf("escaping %s's %q did not change it; the control did not actually mutate anything", r.key(), r.Pattern)
		}
		mutated[i] = corpusRecord{
			Source: r.Source, Type: r.Type, Field: r.Field,
			Pattern: escaped, Verdict: verdictFor(escaped),
		}
		break
	}

	diffs := diffCorpusRecords(got, mutated)
	if len(diffs) == 0 {
		t.Fatal("escaping the \"ax.25\" alternative's dot produced no diff; the comparison " +
			"cannot go red, which means a real pattern-meaning change would pass silently too")
	}
	if len(diffs) != 1 {
		t.Fatalf("mutating one field's pattern produced %d diffs, want 1: %v", len(diffs), diffs)
	}

	diff := diffs[0]
	if !strings.Contains(diff, target.key()) {
		t.Errorf("the diff does not name the mutated field %s:\n%s", target.key(), diff)
	}
	for _, want := range []string{`"axX25": A->R`, `"ax:25": A->R`} {
		if !strings.Contains(diff, want) {
			t.Errorf("the diff does not name the moved string %s:\n%s", want, diff)
		}
	}
	if strings.Contains(diff, `"ax.25": `) {
		t.Errorf("the diff claims \"ax.25\" itself moved, but escaping its own dot must still match it literally:\n%s", diff)
	}
	t.Logf("positive control diff:\n%s", diff)
}
