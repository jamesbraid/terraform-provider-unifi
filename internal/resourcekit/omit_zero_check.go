package resourcekit

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"

	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// OmitZeroProblems reports every Int64PtrField whose wire name resolves to a
// controller constraint whose pattern rejects a literal "0", and which does
// not set OmitZero. Without it, an Unknown plan value (an Optional+Computed
// field with no schema default, on create) or an explicit zero collapses to
// a non-nil pointer-to-zero that bypasses the SDK's omitempty and reaches
// the controller as a literal 0, rejected with api.err.InvalidValue. This is
// the class behind wlan's dtim_6e/dtim_na/dtim_ng and
// roaming_assistant_6e_rssi/roaming_assistant_na_rssi (R2-C Task 10b); the
// check exists so the next one is caught here instead of by a live
// acceptance run.
//
// Int64Field (the non-pointer sibling) does not share this hazard and is not
// walked: its own ToSDK skips Null and Unknown outright, so an unset value
// is never written at all and there is nothing for omitempty to bypass.
// DurationPtrField skips Null/Unknown the same way, for the same reason.
// Int64PtrField is the one kind whose ToSDK always writes a pointer, Unknown
// included, unless OmitZero says otherwise -- see its own doc comment.
//
// constraints is the SDK's per-struct constraint table (e.g.
// unifi.FieldConstraints["WLAN"]); resourcekit has no way to name S at this
// generic boundary, so the caller resolves the lookup and hands in the
// result.
func OmitZeroProblems[M any, S any](spec Spec[M, S], constraints map[string]ui.FieldConstraint) []string {
	var problems []string
	for _, field := range spec.Fields {
		// Unlike ElideProblems, a read-only field is skipped rather than
		// unwrapped: readOnlyField.ToSDK is a hard no-op ("the field never
		// reaches the controller"), so there is no write path for OmitZero
		// to protect and nothing here to check. Unwrapping it anyway was
		// this check's own first bug, caught by firewall_policy.index --
		// Computed-only, wrapped in ReadOnly specifically because the
		// controller rejects any client-supplied value on create or update.
		if _, readOnly := field.(interface{ Unwrap() Field[M, S] }); readOnly {
			continue
		}
		value := reflect.ValueOf(field)
		kind := value.Type().Name()
		if i := strings.IndexByte(kind, '['); i > 0 {
			kind = kind[:i] // strip the generic instantiation
		}
		if kind != "Int64PtrField" {
			continue
		}
		constraint, ok := constraints[field.WireName()]
		if !ok || constraint.Pattern == "" {
			continue
		}
		re, err := regexp.Compile(anchoredControllerPattern(constraint.Pattern))
		if err != nil {
			problems = append(problems, fmt.Sprintf(
				"%s.%s: constraint pattern %q does not compile as RE2: %v",
				spec.TypeName, field.WireName(), constraint.Pattern, err))
			continue
		}
		if re.MatchString("0") {
			continue // zero is a legal value on the wire; nothing to omit
		}
		omitZero := value.FieldByName("OmitZero")
		if omitZero.IsValid() && omitZero.Bool() {
			continue
		}
		problems = append(problems, fmt.Sprintf(
			"%s.%s: the controller's pattern %q rejects a literal zero but the field sets no "+
				"OmitZero, so an Unknown or unset plan value is force-emitted as the zero the "+
				"controller refuses",
			spec.TypeName, field.WireName(), constraint.Pattern))
	}
	sort.Strings(problems)
	return problems
}

// anchoredControllerPattern full-matches a controller pattern the way the
// controller itself does: wrapped ^(?:...)$ unless it is already anchored at
// both ends. Matches the anchoring rule provider-codegen's own derived
// validators use for these same constraint tables.
func anchoredControllerPattern(pattern string) string {
	if fullyAnchored(pattern) {
		return pattern
	}
	return "^(?:" + pattern + ")$"
}

// fullyAnchored reports whether pattern, matched with Go's regexp
// MatchString (a partial match), can only ever match the whole input --
// equivalent to the controller's own full-match semantics. A leading ^ and
// trailing $ on the pattern as a whole are not sufficient: regexp's `|`
// binds looser than either anchor, so "^A|B$" is the alternation of "^A"
// (matches anything starting with A, no end anchor) and "B$" (matches
// anything ending with B, no start anchor) -- neither branch alone forces a
// full match, and go-unifi's DeviceConfigNetwork.netmask /
// Network.wan_netmask patterns are exactly this shape. What's actually
// required is that every top-level alternative -- split on a `|` outside
// any group or character class -- independently starts with ^ and ends
// with $; "^A$|^B$" is fine as-is, because both branches are self-anchored.
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

// OmitZeroProblems on the resource binds the spec to its own SDK struct's
// constraint table, so a caller holding only a resource.Resource can ask the
// same question ZeroReadProblems answers, through one non-generic interface
// that reaches every kit surface without naming any.
func (r *Resource[M, S]) OmitZeroProblems() []string {
	var probe S
	return OmitZeroProblems(r.Spec, ui.FieldConstraints[reflect.TypeOf(probe).Name()])
}
