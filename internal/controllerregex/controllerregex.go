// Package controllerregex compiles the UniFi controller's own validation
// patterns the way the controller itself reads them, rather than the way
// Go's RE2 happens to be able to read them. The controller's dialect is
// Java, driving Pattern.compile(pattern).matcher(s).matches() -- a
// single-argument compile (no flags, no UNICODE_CHARACTER_CLASS) and a
// full-string match. RE2 cannot express lookaround at all, which rules out
// four of the controller's patterns outright; this package runs every
// pattern under github.com/dlclark/regexp2 instead, in its default (.NET)
// mode, so the rule becomes "what the controller wrote is what we
// enforce."
//
// regexp2's default mode is not identical to Java in every respect -- see
// the note on "." at the end of this comment, a known divergence this
// package does not correct. What it does correct is a closed, mechanical
// list of exactly three differences, each pinned by this package's tests.
// A pattern is never rewritten beyond this list:
//
//  1. Anchoring wraps a pattern in \A(?:...)\z, not ^(?:...)$. regexp2's
//     default $ carries Perl's leniency (it also matches immediately before
//     a single trailing "\n"); \z does not, and neither does the
//     controller's own Matcher.matches(). \A/\z anchoring is applied
//     unconditionally, even to a pattern that already carries its own
//     internal ^/$ -- those survive untouched inside the wrap, and the
//     outer \A...\z is what actually forces the full match.
//  2. \d and \w are expanded to their ASCII-only forms, class-aware: inside
//     a character class \d becomes 0-9 and \w becomes a-zA-Z_0-9; outside
//     one, the bracketed forms [0-9] and [a-zA-Z_0-9]. Java without
//     UNICODE_CHARACTER_CLASS is ASCII-only for both; regexp2's default
//     mode is Unicode-aware, so left untranslated a pattern accepts values
//     (an Arabic-Indic digit, an accented letter) the controller rejects. A
//     text substitution is not class-aware and is wrong: \d -> [0-9] inside
//     an existing class produces [[0-9]], which Java parses as a nested
//     union and .NET does not.
//  3. The translation in (2) understands exactly \d and \w. \s, \S, \D, \W,
//     \b and \B carry the identical ASCII/Unicode divergence risk, but none
//     of them appears in the controller's captured patterns, so none was
//     ever characterised, and passing any of them through unexpanded would
//     silently repeat the bug (2) exists to fix. Compile refuses each by
//     name instead. Every other backslash escape passes through verbatim --
//     the study found nothing else in the corpus that a .NET-flavoured
//     engine cannot already read.
//
// Known, uncorrected divergence: "." under regexp2's default mode excludes
// only "\n". Java's "." (without DOTALL) also excludes "\r", U+0085,
// U+2028 and U+2029. 23 of the corpus's patterns carry an unescaped "."
// outside a character class, so Compile(".{0,128}").MatchString("a\rb")
// returns true where the controller's own Matcher.matches() would return
// false. This is an over-accept, and it is identical to what Go's RE2
// already does for the same patterns today, so it is not a regression --
// it simply falls outside the closed list above: unmeasured against the
// controller until now, and not translated.
package controllerregex

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dlclark/regexp2"
	"github.com/hashicorp/terraform-plugin-framework-validators/helpers/validatordiag"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// MatchTimeout bounds a single MatchString call. It is not sized off match
// cost -- the worst measured match across every shipped pattern is tens of
// microseconds, several orders of magnitude below this bound -- because
// match cost is not what the deadline has to clear. MatchTimeout is
// wall-clock, so scheduler and GC jitter alone can consume it on a busy
// host even when the match itself is trivial: a 1.6ms bound (this
// constant's original value) produced spurious timeouts on ordinary,
// perfectly valid input under CPU contention -- a hard "Controller Pattern
// Match Timeout" error a practitioner cannot explain or reproduce. 250ms
// clears realistic jitter by a wide margin while still being roughly
// 13,000x the worst legitimate match, so it still bounds a pattern/input
// pair that backtracks catastrophically -- the hazard this timeout exists
// for in the first place.
const MatchTimeout = 250 * time.Millisecond

// Anchored returns pattern wrapped so that a match means what the controller
// means -- a full match -- unless it already anchors both ends. A leading ^
// and trailing $ on the pattern as a whole are not sufficient: regexp's `|`
// binds looser than either anchor, so "^A|B$" is the alternation of "^A"
// (matches anything starting with A, no end anchor) and "B$" (matches
// anything ending with B, no start anchor) -- neither branch alone forces a
// full match, and go-unifi's DeviceConfigNetwork.netmask / Network.wan_netmask
// patterns are exactly this shape. What's actually required is that every
// top-level alternative -- split on a `|` outside any group or character
// class -- independently starts with ^ and ends with $; "^A$|^B$" is fine
// as-is, because both branches are self-anchored.
//
// Anchored is the RE2-facing half of this package: it reproduces, byte for
// byte, the wrap the generated schema has always used (^(?:...)$), so the
// two callers that build a Go regexp.Regexp from a controller pattern --
// providercompiler's derived RegexMatches validator and resourcekit's
// omit-zero census -- see no change from moving here. Compile, which builds
// the full-match rule this package's engine actually needs, does not call
// Anchored; see its own doc comment.
func Anchored(pattern string) string {
	if fullyAnchored(pattern) {
		return pattern
	}
	return "^(?:" + pattern + ")$"
}

// fullyAnchored reports whether pattern, matched with Go's regexp
// MatchString (a partial match), can only ever match the whole input.
func fullyAnchored(pattern string) bool {
	for _, alternative := range splitTopLevelAlternatives(pattern) {
		if !strings.HasPrefix(alternative, "^") || !strings.HasSuffix(alternative, "$") {
			return false
		}
	}
	return true
}

// splitTopLevelAlternatives splits pattern on every `|` that sits outside a
// group, a character class, and an escape sequence -- the points where a
// regex engine treats the pattern as a genuine top-level alternation, as
// opposed to a `|` inside `(...)` or `[...]` that only alternates within
// that piece.
func splitTopLevelAlternatives(pattern string) []string {
	var alternatives []string
	depth := 0
	inClass := false
	start := 0
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '\\':
			i++ // the next byte is escaped, whatever it is; skip it too
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '(':
			if !inClass {
				depth++
			}
		case ')':
			if !inClass && depth > 0 {
				depth--
			}
		case '|':
			if !inClass && depth == 0 {
				alternatives = append(alternatives, pattern[start:i])
				start = i + 1
			}
		}
	}
	return append(alternatives, pattern[start:])
}

// translateClasses expands \d and \w to their ASCII-only forms, class-aware
// -- deviation (2) in the package doc. Every other escape passes through
// untouched, except \s, \S, \D, \W, \b and \B, which this package's grammar
// does not cover: deviation (3) says refuse each by name rather than risk a
// silent mistranslation of a construct nobody has measured.
func translateClasses(pattern string) (string, error) {
	var out strings.Builder
	inClass := false
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		if c == '\\' && i+1 < len(pattern) {
			switch pattern[i+1] {
			case 'd':
				if inClass {
					out.WriteString(`0-9`)
				} else {
					out.WriteString(`[0-9]`)
				}
			case 'w':
				if inClass {
					out.WriteString(`a-zA-Z_0-9`)
				} else {
					out.WriteString(`[a-zA-Z_0-9]`)
				}
			case 's', 'S', 'D', 'W', 'b', 'B':
				escaped := pattern[i+1]
				return "", fmt.Errorf(
					"controllerregex: pattern %q uses \\%c, which this package's translator does not "+
						"cover -- only \\d and \\w are expanded to ASCII forms, and \\%c never appeared "+
						"in the corpus that rule was measured against, so it is refused rather than "+
						"silently left Unicode-aware",
					pattern, escaped, escaped,
				)
			default:
				out.WriteByte(c)
				out.WriteByte(pattern[i+1])
			}
			i++
			continue
		}
		if c == '[' && !inClass {
			inClass = true
		} else if c == ']' && inClass {
			inClass = false
		}
		out.WriteByte(c)
	}
	return out.String(), nil
}

// Pattern is a controller pattern compiled the way the controller reads it.
type Pattern struct {
	raw string
	re  *regexp2.Regexp
}

// Compile compiles pattern -- verbatim from the controller's own constraint
// table, never rewritten beyond the closed list of deviations this
// package's doc comment names -- under regexp2's default mode, full-match
// anchored with \A(?:...)\z, with MatchTimeout applied. The error names the
// construct: either translateClasses refusing an escape outside its
// grammar, or regexp2 itself refusing a construct the engine cannot
// express.
func Compile(pattern string) (*Pattern, error) {
	translated, err := translateClasses(pattern)
	if err != nil {
		return nil, err
	}
	wrapped := `\A(?:` + translated + `)\z`
	re, err := regexp2.Compile(wrapped, regexp2.None)
	if err != nil {
		return nil, fmt.Errorf("controllerregex: pattern %q does not compile: %w", pattern, err)
	}
	re.MatchTimeout = MatchTimeout
	return &Pattern{raw: pattern, re: re}, nil
}

// MatchString reports whether s satisfies the pattern -- a full match, the
// controller's own semantics. The only error regexp2's MatchString can
// return is a MatchTimeout excess (that is the whole of its documented
// error contract); this returns it rather than leaving the caller to hang.
func (p *Pattern) MatchString(s string) (bool, error) {
	ok, err := p.re.MatchString(s)
	if err != nil {
		return false, fmt.Errorf("controllerregex: matching %q against pattern %q: %w", s, p.raw, err)
	}
	return ok, nil
}

// matchesValidator implements validator.String for one controller pattern.
type matchesValidator struct {
	pattern     string
	description string
	compiled    *Pattern
	compileErr  error
}

// Matches is the validator the generated schema constructs in place of
// stringvalidator.RegexMatches(regexp.MustCompile(...), ""). It compiles
// pattern once, at construction -- the schema is built once per provider
// instance, so compiling per validation request would be wasteful -- but a
// pattern that fails to compile does not panic the provider: it becomes a
// validation error naming the attribute and the pattern the first time the
// attribute is validated, so one bad pattern degrades one field rather than
// the whole schema build. A match that exceeds MatchTimeout is reported the
// same way, never left to hang.
func Matches(pattern, description string) validator.String {
	compiled, err := Compile(pattern)
	return matchesValidator{
		pattern:     pattern,
		description: description,
		compiled:    compiled,
		compileErr:  err,
	}
}

func (v matchesValidator) Description(_ context.Context) string {
	if v.description != "" {
		return v.description
	}
	return fmt.Sprintf("value must match regular expression '%s'", v.pattern)
}

func (v matchesValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v matchesValidator) ValidateString(ctx context.Context, request validator.StringRequest, response *validator.StringResponse) {
	if request.ConfigValue.IsNull() || request.ConfigValue.IsUnknown() {
		return
	}

	if v.compileErr != nil {
		response.Diagnostics.Append(diag.NewAttributeErrorDiagnostic(
			request.Path,
			"Invalid Controller Pattern",
			fmt.Sprintf("The controller's own pattern %q could not be compiled, so this attribute "+
				"cannot be validated: %s", v.pattern, v.compileErr),
		))
		return
	}

	value := request.ConfigValue.ValueString()
	matched, err := v.compiled.MatchString(value)
	if err != nil {
		response.Diagnostics.Append(diag.NewAttributeErrorDiagnostic(
			request.Path,
			"Controller Pattern Match Timeout",
			fmt.Sprintf("Validating against the controller's pattern %q timed out: %s", v.pattern, err),
		))
		return
	}
	if !matched {
		response.Diagnostics.Append(validatordiag.InvalidAttributeValueMatchDiagnostic(
			request.Path,
			v.Description(ctx),
			value,
		))
	}
}
