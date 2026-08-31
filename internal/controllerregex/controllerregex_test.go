package controllerregex

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/dlclark/regexp2"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// --- Anchored: ported from providercompiler's TestCompileEmitsAnAlreadyAnchoredPatternVerbatim
// / TestCompileEmitsAPatternThatStillRejectsALeadingSpace /
// TestCompileEmitsAnUnparenthesizedTopLevelAlternationThatStillForcesAFullMatch /
// TestCompileLeavesSelfAnchoredTopLevelBranchesUnwrapped (renamed since,
// when derive_validators.go stopped doing its own anchoring -- Task 2), and
// resourcekit's
// TestAnchoredControllerPatternWrapsAnUnparenthesizedTopLevelAlternation /
// TestAnchoredControllerPatternLeavesSelfAnchoredBranchesAlone -- both call
// sites tested the same rule against their own copy; this is the one copy
// now. ---

func TestAnchoredLeavesAnAlreadyAnchoredPatternUnwrapped(t *testing.T) {
	pattern := `^.{0,256}$`
	if got := Anchored(pattern); got != pattern {
		t.Fatalf("Anchored(%q) = %q, want the pattern left unwrapped", pattern, got)
	}
}

func TestAnchoredWrapsAnUnanchoredPatternAndRejectsALeadingSpace(t *testing.T) {
	pattern := `[^\"\' ]+`
	anchored := Anchored(pattern)
	want := "^(?:" + pattern + ")$"
	if anchored != want {
		t.Fatalf("Anchored(%q) = %q, want %q", pattern, anchored, want)
	}
	re := regexp.MustCompile(anchored)
	if re.MatchString(" leaked-with-leading-space") {
		t.Errorf("anchored pattern %q matched a value with a leading space, want rejected", anchored)
	}
	if !re.MatchString("a-real-key") {
		t.Errorf("anchored pattern %q rejected a plain value, want accepted", anchored)
	}
	if re.MatchString(`has"quote`) {
		t.Errorf("anchored pattern %q matched a value containing a double quote, want rejected", anchored)
	}
}

// TestAnchoredWrapsAPatternWithAnUnparenthesizedTopLevelAlternation is the
// v0.107.0 regression case: a leading ^ and a trailing $ on the pattern as a
// whole are not enough to call it anchored. "^A|B$" is the alternation of
// "^A" (no end anchor) and "B$" (no start anchor), the exact shape of
// go-unifi's DeviceConfigNetwork.netmask and Network.wan_netmask patterns --
// both were left unwrapped and matched a value with trailing or leading
// garbage before that fix.
func TestAnchoredWrapsAPatternWithAnUnparenthesizedTopLevelAlternation(t *testing.T) {
	pattern := `^A|B$`
	anchored := Anchored(pattern)
	want := "^(?:" + pattern + ")$"
	if anchored != want {
		t.Fatalf("Anchored(%q) = %q, want %q (pattern must be wrapped)", pattern, anchored, want)
	}
	re := regexp.MustCompile(anchored)
	if re.MatchString("Agarbage") {
		t.Errorf("wrapped pattern %q matched %q on the unanchored first branch, want rejected", anchored, "Agarbage")
	}
	if re.MatchString("garbageB") {
		t.Errorf("wrapped pattern %q matched %q on the unanchored second branch, want rejected", anchored, "garbageB")
	}
	if !re.MatchString("A") || !re.MatchString("B") {
		t.Errorf("wrapped pattern %q rejected a real value, want both %q and %q accepted", anchored, "A", "B")
	}
}

// A pattern whose top-level branches are each already self-anchored (the
// real shape of go-unifi's DeviceIPv4.netmask, "^digits$|^$") must not be
// re-wrapped: double-wrapping is harmless for correctness but would move
// every such attribute's generated pattern for no reason.
func TestAnchoredLeavesSelfAnchoredTopLevelBranchesUnwrapped(t *testing.T) {
	pattern := `^(0|[1-9]|1[0-9]|2[0-9]|3[0-2])$|^$`
	if got := Anchored(pattern); got != pattern {
		t.Fatalf("Anchored(%q) = %q, want the pattern left unwrapped", pattern, got)
	}
}

// --- Compile: the four lookaround patterns, quoted verbatim from
// regex-engine-study.md section 1. RE2 cannot compile any of these; this
// package's whole reason to exist is that regexp2 can. The study only
// checked that they compile -- it never ran them, so accept/reject here is
// new evidence, derived by reading each pattern, not carried over from the
// study. ---

func TestCompileAcceptsTheFourLookaroundPatterns(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		accept  []string
		reject  []string
	}{
		{
			name:    "DHCPOption.code",
			pattern: `^(?!(?:15|42|43|44|51|66|67|252)$)([7-9]|[1-9][0-9]|1[0-9][0-9]|2[0-4][0-9]|25[0-4])$`,
			accept:  []string{"100"},     // in range, not excluded
			reject:  []string{"42", "5"}, // "42" excluded by the lookahead; "5" out of the shape entirely
		},
		{
			name:    "Network.dhcpd_boot_server",
			pattern: `^(([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])\.){3}([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])$|^$|(?=^.{3,253}$)(^((?!-)[a-zA-Z0-9-]{1,63}(?<!-)\.)+[a-zA-Z]{2,63}$)|[a-zA-Z0-9-]{1,63}`,
			accept:  []string{"10.0.0.1", "good.example.com"},
			reject:  []string{"-bad.example.com"}, // leading hyphen label, rejected by the (?!-) lookahead
		},
		{
			name:    "Network.domain_name",
			pattern: `(?=^.{3,253}$)(^((?!-)[a-zA-Z0-9-]{1,63}(?<!-)\.)+[a-zA-Z]{2,63}$)|^$|[a-zA-Z0-9-]{1,63}`,
			accept:  []string{"example.com"},
			reject:  []string{"bad-.example.com"}, // trailing hyphen label, rejected by the (?<!-) lookbehind
		},
		{
			name:    "Network.pptpc_server_ip",
			pattern: `^(([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])\.){3}([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])$|(?=^.{3,253}$)(^((?!-)[a-zA-Z0-9-]{1,63}(?<!-)\.)+[a-zA-Z]{2,63}$)|^[a-zA-Z0-9-]{1,63}$`,
			accept:  []string{"10.0.0.1", "good.example.com"},
			reject:  []string{"-bad.example.com"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := Compile(tc.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error = %v, want RE2-uncompilable lookaround pattern accepted by regexp2", tc.pattern, err)
			}
			for _, v := range tc.accept {
				ok, err := p.MatchString(v)
				if err != nil {
					t.Fatalf("MatchString(%q) error = %v", v, err)
				}
				if !ok {
					t.Errorf("pattern %q rejected %q, want accepted", tc.name, v)
				}
			}
			for _, v := range tc.reject {
				ok, err := p.MatchString(v)
				if err != nil {
					t.Fatalf("MatchString(%q) error = %v", v, err)
				}
				if ok {
					t.Errorf("pattern %q accepted %q, want rejected", tc.name, v)
				}
			}
		})
	}
}

// TestCompileRefusesAConstructTheEngineCannotTake is the "genuine compile
// refusal" path: a pattern that is not valid under regexp2 at all (as
// opposed to a construct our own translator declines to touch). An
// unbalanced group is about as unambiguous as an invalid pattern gets, and
// regexp2's own error names the problem.
func TestCompileRefusesAConstructTheEngineCannotTake(t *testing.T) {
	pattern := `(unterminated`
	_, err := Compile(pattern)
	if err == nil {
		t.Fatalf("Compile(%q) succeeded, want an error naming the construct regexp2 cannot take", pattern)
	}
	if !strings.Contains(err.Error(), pattern) {
		t.Errorf("Compile error = %q, want it to name the pattern %q", err, pattern)
	}
}

// TestCompileRefusesBackslashSAsOutsideTheTranslatorsGrammar is deviation
// (3)'s positive control: \s is a synthetic construct (it never appears in
// the controller's captured patterns) carrying the identical ASCII/Unicode
// divergence risk \d and \w exist to fix. Without this test, the refusal
// rejects nothing in the real corpus and is indistinguishable from a typo.
func TestCompileRefusesBackslashSAsOutsideTheTranslatorsGrammar(t *testing.T) {
	pattern := `[\s]+`
	_, err := Compile(pattern)
	if err == nil {
		t.Fatalf("Compile(%q) succeeded, want the translator to refuse \\s by name", pattern)
	}
	if !strings.Contains(err.Error(), `\s`) {
		t.Errorf("Compile error = %q, want it to name the construct \\s", err)
	}
}

// --- The \d/\w translation, pinned in both directions over the go-unifi
// session's seven-pattern fixture (regex-engine-study.md addendum). ---

var sevenPatternFixture = []struct{ id, raw string }{
	{"1", `(^([-]?[\d]+)$)|(^([-]?[\d]+[.]?[\d]+)$)`},
	{"2", `WAN[1-9]?|[\d\w-]+|^$`},
	{"3", `[\d\w-]+`},
	{"4", `[\d\w-]+|`},
	{"5", `[\d\w-]+|^$`},
	{"6", `[\d]+|auto`},
	{"7", `[\d]+|custom`},
}

func fixtureRaw(t *testing.T, id string) string {
	t.Helper()
	for _, p := range sevenPatternFixture {
		if p.id == id {
			return p.raw
		}
	}
	t.Fatalf("no fixture pattern %q", id)
	return ""
}

// rawCompile compiles pattern with the same \A(?:...)\z wrap Compile uses,
// but skipping translateClasses entirely -- the untranslated counterfactual
// that proves the translation is doing real work, not decoration.
func rawCompile(t *testing.T, pattern string) *regexp2.Regexp {
	t.Helper()
	re, err := regexp2.Compile(`\A(?:`+pattern+`)\z`, regexp2.None)
	if err != nil {
		t.Fatalf("raw regexp2.Compile(%q) error = %v", pattern, err)
	}
	return re
}

func TestDigitWordTranslationRejectsNonASCIIAndAcceptsASCII(t *testing.T) {
	cases := []struct {
		fixtureID   string
		nonASCII    string
		asciiAccept string
	}{
		{"6", "٣", "3"},         // [\d]+|auto -- Arabic-Indic digit vs. an ASCII digit
		{"5", "café", "cafe"},   // [\d\w-]+|^$
		{"3", "café", "cafe-1"}, // [\d\w-]+
	}
	for _, tc := range cases {
		t.Run(tc.fixtureID, func(t *testing.T) {
			raw := fixtureRaw(t, tc.fixtureID)
			translated, err := Compile(raw)
			if err != nil {
				t.Fatalf("Compile(%q) error = %v", raw, err)
			}

			ok, err := translated.MatchString(tc.nonASCII)
			if err != nil {
				t.Fatalf("MatchString(%q) error = %v", tc.nonASCII, err)
			}
			if ok {
				t.Errorf("translated pattern %q accepted %q, want rejected", raw, tc.nonASCII)
			}

			ok, err = translated.MatchString(tc.asciiAccept)
			if err != nil {
				t.Fatalf("MatchString(%q) error = %v", tc.asciiAccept, err)
			}
			if !ok {
				t.Errorf("translated pattern %q rejected %q, want accepted", raw, tc.asciiAccept)
			}

			// The counterfactual: raw and untranslated, regexp2's own
			// Unicode-awareness accepts what the translated form rejects.
			// This is what stops a later reader deleting the translation as
			// redundant -- without it, the claim that translation matters is
			// only a comment.
			rawRE := rawCompile(t, raw)
			rawOK, err := rawRE.MatchString(tc.nonASCII)
			if err != nil {
				t.Fatalf("raw MatchString(%q) error = %v", tc.nonASCII, err)
			}
			if !rawOK {
				t.Errorf("raw (untranslated) pattern %q rejected %q, want accepted -- translation would be redundant", raw, tc.nonASCII)
			}
		})
	}
}

// TestDigitWordTranslationAppliesOutsideAClassToo pins the other half of
// deviation (2). Every case above puts \d/\w inside a character class (all
// seven fixture patterns do, and so do the four lookaround patterns' \d-free
// shapes), so none of them can tell the class-aware translation apart from a
// mutant that always emits the inside-a-class form (bare 0-9 / a-zA-Z_0-9)
// even outside one -- a mutant that leaves the whole package suite green,
// because nothing exercises that branch. These two patterns put \d and \w
// outside a class, where the bracketed forms ([0-9] / [a-zA-Z_0-9]) are
// required; the bare forms there mean something else entirely (0-9+ outside
// a class is the literal digit 0, a literal hyphen, then one or more 9s).
func TestDigitWordTranslationAppliesOutsideAClassToo(t *testing.T) {
	digits, err := Compile(`\d+`)
	if err != nil {
		t.Fatalf("Compile(`\\d+`) error = %v", err)
	}
	if ok, err := digits.MatchString("123"); err != nil {
		t.Fatalf("MatchString(%q) error = %v", "123", err)
	} else if !ok {
		t.Errorf(`\d+ rejected "123", want accepted`)
	}
	if ok, err := digits.MatchString("٣"); err != nil {
		t.Fatalf("MatchString(%q) error = %v", "٣", err)
	} else if ok {
		t.Errorf(`\d+ accepted "٣" (U+0663), want rejected`)
	}

	word, err := Compile(`\w+`)
	if err != nil {
		t.Fatalf("Compile(`\\w+`) error = %v", err)
	}
	if ok, err := word.MatchString("abc_1"); err != nil {
		t.Fatalf("MatchString(%q) error = %v", "abc_1", err)
	} else if !ok {
		t.Errorf(`\w+ rejected "abc_1", want accepted`)
	}
	if ok, err := word.MatchString("café"); err != nil {
		t.Fatalf("MatchString(%q) error = %v", "café", err)
	} else if ok {
		t.Errorf(`\w+ accepted "café", want rejected`)
	}
}

// TestAnchoredPatternRejectsATrailingNewline pins deviation (1): \z, not $,
// closes the trailing-newline leak that regexp2's default mode otherwise
// carries -- even for patterns (fixture 1, 2, 5) whose own raw text already
// carries an internal ^/$ that survives the wrap unmodified. None of those
// three is fullyAnchored, though (Anchored(fixtureRaw) != fixtureRaw for all
// three), so the skip-if-anchored branch of Anchored never fires on any of
// them either way -- these three cases alone cannot tell this package's
// unconditional \A(?:...)\z apart from a rule that reuses Anchored's
// already-anchored check and skips the wrap when it says yes. macAddress
// below is genuinely fullyAnchored (Anchored(macAddress) == macAddress), so
// it is the case that actually discriminates the two rules: 57 of the 316
// patterns in the live corpus are fullyAnchored this way, and 33 of them --
// macAddress among them -- leak a trailing newline under a skip-if-anchored
// rule. Only the unconditional wrap closes that gap.
func TestAnchoredPatternRejectsATrailingNewline(t *testing.T) {
	for _, id := range []string{"1", "2", "5"} {
		raw := fixtureRaw(t, id)
		p, err := Compile(raw)
		if err != nil {
			t.Fatalf("Compile(%q) error = %v", raw, err)
		}
		for _, in := range []string{"\n", "5\n"} {
			ok, err := p.MatchString(in)
			if err != nil {
				t.Fatalf("MatchString(%q) error = %v", in, err)
			}
			if ok {
				t.Errorf("[%s] pattern %q accepted %q (trailing newline), want rejected", id, raw, in)
			}
		}
	}

	const macAddress = `^([0-9A-Fa-f]{2}:){5}([0-9A-Fa-f]{2})$|^$`
	if Anchored(macAddress) != macAddress {
		t.Fatalf("test fixture %q is not fullyAnchored, and so cannot discriminate the unconditional wrap from skip-if-anchored", macAddress)
	}
	p, err := Compile(macAddress)
	if err != nil {
		t.Fatalf("Compile(%q) error = %v", macAddress, err)
	}
	if ok, err := p.MatchString("\n"); err != nil {
		t.Fatalf("MatchString(%q) error = %v", "\n", err)
	} else if ok {
		t.Errorf("fullyAnchored pattern %q accepted %q (trailing newline), want rejected", macAddress, "\n")
	}
}

// TestUngroupedWrapAcceptsWhatTheGroupedWrapRejects shows the non-capturing
// group in \A(?:...)\z is load bearing, not decorative: fixture 4's last
// alternative is empty, and without the group \A...\z applies only to the
// last branch, changing what the pattern as a whole means.
func TestUngroupedWrapAcceptsWhatTheGroupedWrapRejects(t *testing.T) {
	raw := fixtureRaw(t, "4") // `[\d\w-]+|`
	translated, err := translateClasses(raw)
	if err != nil {
		t.Fatalf("translateClasses(%q) error = %v", raw, err)
	}

	grouped, err := regexp2.Compile(`\A(?:`+translated+`)\z`, regexp2.None)
	if err != nil {
		t.Fatalf("grouped compile error = %v", err)
	}
	ungrouped, err := regexp2.Compile(`\A`+translated+`\z`, regexp2.None)
	if err != nil {
		t.Fatalf("ungrouped compile error = %v", err)
	}

	input := "zzz!!!"
	groupedOK, err := grouped.MatchString(input)
	if err != nil {
		t.Fatalf("grouped MatchString error = %v", err)
	}
	ungroupedOK, err := ungrouped.MatchString(input)
	if err != nil {
		t.Fatalf("ungrouped MatchString error = %v", err)
	}
	if groupedOK {
		t.Errorf("grouped wrap accepted %q, want rejected", input)
	}
	if !ungroupedOK {
		t.Errorf("ungrouped wrap rejected %q, want accepted -- the non-capturing group is not load-bearing after all", input)
	}
}

// --- MatchString against a sample of real controller patterns, compared to
// what RE2 says on the same (old, ^(?:...)$-anchored) form. Patterns are
// chosen with no \d, \w, \s, or lookaround, and the corpus excludes a
// trailing newline: those are this package's three deliberate deviations,
// pinned by name above. Outside them, the study found zero disagreements
// between RE2 and a .NET-flavoured engine, and this is that same claim,
// re-derived independently against real go-unifi patterns rather than
// re-reading the study's numbers. ---

func sampleTenPatterns(t *testing.T) []string {
	t.Helper()
	seen := map[string]bool{}
	var out []string

	typeNames := make([]string, 0, len(ui.FieldConstraints))
	for k := range ui.FieldConstraints {
		typeNames = append(typeNames, k)
	}
	sort.Strings(typeNames)

	for _, typeName := range typeNames {
		fields := ui.FieldConstraints[typeName]
		fieldNames := make([]string, 0, len(fields))
		for k := range fields {
			fieldNames = append(fieldNames, k)
		}
		sort.Strings(fieldNames)
		for _, fieldName := range fieldNames {
			p := fields[fieldName].Pattern
			if p == "" || seen[p] {
				continue
			}
			if strings.Contains(p, `\d`) || strings.Contains(p, `\w`) || strings.Contains(p, `\s`) {
				continue
			}
			if strings.Contains(p, "(?!") || strings.Contains(p, "(?=") || strings.Contains(p, "(?<") {
				continue
			}
			seen[p] = true
			out = append(out, p)
			if len(out) == 10 {
				return out
			}
		}
	}
	if len(out) < 10 {
		t.Fatalf("only found %d ASCII, lookaround-free patterns in unifi.FieldConstraints, want 10", len(out))
	}
	return out
}

func TestMatchStringAgreesWithRE2OnASampleOfTenPatterns(t *testing.T) {
	corpus := []string{
		"", " ", "0", "123", "ABCabc", "café",
		"192.168.1.1", "true", "false", "1e3", "0x10",
		"!@#$%^&*()", strings.Repeat("a", 64),
	}

	for _, pattern := range sampleTenPatterns(t) {
		t.Run(pattern, func(t *testing.T) {
			re2Form := Anchored(pattern)
			re2, err := regexp.Compile(re2Form)
			if err != nil {
				t.Fatalf("regexp.Compile(%q) error = %v -- sampleTenPatterns should only pick RE2-compilable patterns", re2Form, err)
			}
			p, err := Compile(pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error = %v", pattern, err)
			}
			for _, in := range corpus {
				want := re2.MatchString(in)
				got, err := p.MatchString(in)
				if err != nil {
					t.Fatalf("MatchString(%q) error = %v", in, err)
				}
				if got != want {
					t.Errorf("pattern %q input %q: RE2=%v regexp2=%v, want agreement", pattern, in, want, got)
				}
			}
		})
	}
}

// TestCompileTimesOutOnAPathologicalPatternAndInput exercises MatchTimeout:
// a classic catastrophic-backtracking shape ((a+)+ against a long run of a's
// followed by a non-matching character) that the study never fuzzed for --
// its ten-pattern timing table only sampled the controller's real patterns
// against a fixed non-adversarial input.
func TestCompileTimesOutOnAPathologicalPatternAndInput(t *testing.T) {
	pattern := `(a+)+$`
	p, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) error = %v", pattern, err)
	}
	input := strings.Repeat("a", 40) + "!"
	_, err = p.MatchString(input)
	if err == nil {
		t.Fatalf("MatchString on a pathological pattern/input pair succeeded instead of timing out")
	}
}

// --- Matches: the validator.String the generated schema will construct
// (Task 2). The brief's behavioural requirement is "a validation error
// naming the attribute -- never a panic and never a silent pass"; these are
// its tests. Follows the same runValidator/table shape as
// unifi/validators/ipv4_test.go. ---

func runMatchesValidator(v validator.String, value types.String) *validator.StringResponse {
	resp := &validator.StringResponse{}
	v.ValidateString(context.Background(), validator.StringRequest{
		Path:        path.Root("test"),
		ConfigValue: value,
	}, resp)
	return resp
}

func TestMatchesAcceptsAValueThatSatisfiesThePattern(t *testing.T) {
	v := Matches(`^[a-z]+$`, "")
	resp := runMatchesValidator(v, types.StringValue("abc"))
	if resp.Diagnostics.HasError() {
		t.Fatalf("valid value produced diagnostics: %v", resp.Diagnostics)
	}
}

func TestMatchesRejectsAValueThatDoesNotSatisfyThePattern(t *testing.T) {
	v := Matches(`^[a-z]+$`, "")
	resp := runMatchesValidator(v, types.StringValue("ABC"))
	if !resp.Diagnostics.HasError() {
		t.Fatalf("invalid value produced no diagnostics, want an error")
	}
}

// TestMatchesReportsAnUncompilablePatternAsADiagnostic is the "never a
// panic and never a silent pass" half of the brief's requirement: a pattern
// this package's translator refuses (\s -- see the refusal tests above)
// must still produce a usable validator.String, one that reports the
// problem as a diagnostic naming the attribute when it is actually asked to
// validate, rather than panicking at construction or silently accepting
// everything.
func TestMatchesReportsAnUncompilablePatternAsADiagnostic(t *testing.T) {
	v := Matches(`[\s]+`, "") // constructing this must not panic
	resp := runMatchesValidator(v, types.StringValue("anything"))
	if !resp.Diagnostics.HasError() {
		t.Fatalf("an uncompilable pattern produced no diagnostics, want an error naming the problem")
	}
}

// TestMatchesSkipsNullAndUnknownValues matches every other validator.String
// in this codebase (see runIPv4Validator's cases in
// unifi/validators/ipv4_test.go): an unconfigured or not-yet-known value is
// not validated at all.
func TestMatchesSkipsNullAndUnknownValues(t *testing.T) {
	v := Matches(`^[a-z]+$`, "")
	for _, value := range []types.String{types.StringNull(), types.StringUnknown()} {
		resp := runMatchesValidator(v, value)
		if resp.Diagnostics.HasError() {
			t.Errorf("value %#v produced diagnostics, want null/unknown skipped: %v", value, resp.Diagnostics)
		}
	}
}

func TestMatchesDescriptionStatesThePatternWhenNoneIsGiven(t *testing.T) {
	pattern := `^[a-z]+$`
	v := Matches(pattern, "")
	if desc := v.Description(context.Background()); !strings.Contains(desc, pattern) {
		t.Errorf("Description() = %q, want it to contain the pattern %q", desc, pattern)
	}
}
