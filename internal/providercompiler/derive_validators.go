package providercompiler

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/controllerregex"
)

// customValidator mirrors one element of a code-spec attribute's
// "validators" array -- the one shape this provider emits, a hand-written
// Go expression plus the imports it needs.
type customValidator struct {
	Custom *customValidatorBody `json:"custom,omitempty"`
}

type customValidatorBody struct {
	Imports          []customValidatorImport `json:"imports,omitempty"`
	SchemaDefinition string                  `json:"schema_definition"`
}

type customValidatorImport struct {
	Path string `json:"path"`
}

// deriveConstraintValidators folds the bootstrap field's constraint into a
// scalar attribute's validators -- OneOf from a fixed value set, RegexMatches
// from a pattern when there is no value set. A field the SDK constrains this
// way is derived here instead of hand-transcribed, so the compiler is the
// one place the fact can go stale. A hand validator of the same kind still
// present in policy is refused rather than silently shadowed by the derived
// one; policy may set "validators": "none" to record a deliberate exception
// and suppress every derivation for the field. attribute is returned
// unchanged when there is nothing to derive and nothing to suppress. notices
// collects observations worth a human seeing but not worth refusing the
// compile over -- see the uncompilable-pattern fallback below, the only
// producer today.
func deriveConstraintValidators(owner, terraformType string, constraint *bootstrapFieldConstraint, attribute json.RawMessage, notices *[]string) (json.RawMessage, error) {
	suppressed, err := validatorsSuppressed(owner, attribute)
	if err != nil {
		return nil, err
	}
	if suppressed {
		return stripValidators(owner, attribute)
	}
	if schemaDefinition, importPath, ok := oneOfSchemaDefinition(terraformType, constraint); ok {
		imports := []customValidatorImport{{Path: importPath}}
		return appendDerivedValidator(owner, attribute, schemaDefinition, imports, ".OneOf(", "OneOf")
	}
	schemaDefinition, imports, ok, err := regexMatchesSchemaDefinition(owner, terraformType, constraint)
	if err != nil {
		// A pattern controllerregex cannot express only refuses when
		// there's nothing else validating the field. An attribute that
		// already carries a hand validator (of any kind) is trusted to
		// already cover what the uncompilable pattern was expressing.
		// controllerregex's translated grammar covers every construct the
		// two SDK constraint tables are measured to use (regex-engine-study.md),
		// including the four lookaround patterns RE2 could never compile, so
		// this branch is not known to be live against any pattern today --
		// it stays as a fail-safe for a future pattern outside that measured
		// set (an escape outside the six this package refuses by name, or a
		// construct regexp2 itself rejects). "validators": "none" replaces
		// the whole array, so it cannot suppress just the derivation without
		// also deleting a hand validator; skipping is what lets the hand
		// validator stand instead. The skip itself is recorded in notices
		// rather than left silent, so a plain go generate run (and its CI
		// log) shows it without anyone having to know this fallback exists.
		// A field with no validators at all still refuses, naming the
		// field, so the gap becomes a recorded "validators": "none"
		// decision instead of shipping silently unvalidated.
		if hasHand, handErr := attributeHasValidators(attribute); handErr == nil && hasHand {
			if notices != nil {
				*notices = append(*notices, fmt.Sprintf(
					"skipped unparsable pattern for %s: hand validator present", owner,
				))
			}
			return attribute, nil
		}
		return nil, err
	}
	if !ok {
		return attribute, nil
	}
	// A GoDurationType attribute's config value is the human-typed duration
	// string ("4h", "3600s"), not the SDK's wire format (a bare digit string
	// of seconds) the derived pattern describes -- the derived validator
	// would then reject every real value. Keyed on the attribute's own
	// custom_type, not on whether a hand GoDurationBetween/GoDurationMultipleOf
	// validator happens to be present: a Computed-only attribute (a
	// data-source mirror, say) is never configured, so it has nothing to
	// hand-validate and carries no such validator, but still has the wrong
	// pattern derived for it if this only checked for one. Every field in
	// the policy corpus that does carry a hand Go-duration validator also
	// declares this custom_type (verified 2026-08-28), so custom_type alone
	// is the complete signal. Found by checking real shipped example values
	// against the derived patterns (ipv6_ra_preferred_lifetime = "4h",
	// interim_update_interval = "1h", both real examples, neither matching
	// the derived digits-only pattern), then by a coordinator review
	// pointing out this check still missed the Computed-only case.
	hasGoDurationType, err := attributeHasGoDurationCustomType(owner, attribute)
	if err != nil {
		return nil, err
	}
	if hasGoDurationType {
		if notices != nil {
			*notices = append(*notices, fmt.Sprintf(
				"skipped pattern derivation for %s: Go-duration custom type", owner,
			))
		}
		return attribute, nil
	}
	return appendDerivedValidator(owner, attribute, schemaDefinition, imports, ".RegexMatches(", "RegexMatches")
}

// attributeHasValidators reports whether the attribute already carries a
// non-empty hand validators array. Used only to decide whether an
// uncompilable pattern can be skipped instead of refused; any decoding
// trouble is treated as "no" so the caller falls through to the ordinary
// refusal, which will report the real problem.
func attributeHasValidators(attribute json.RawMessage) (bool, error) {
	if len(attribute) == 0 {
		return false, nil
	}
	var body struct {
		Validators json.RawMessage `json:"validators"`
	}
	if err := json.Unmarshal(attribute, &body); err != nil {
		return false, err
	}
	if len(body.Validators) == 0 {
		return false, nil
	}
	var decoded any
	if err := json.Unmarshal(body.Validators, &decoded); err != nil {
		return false, err
	}
	// Not an array -- either the "none" marker (handled earlier) or
	// malformed input; neither counts as "already validated".
	list, isArray := decoded.([]any)
	return isArray && len(list) > 0, nil
}

// attributeHasGoDurationCustomType reports whether the attribute declares
// timetypes.GoDurationType as its custom_type -- the signal that its config
// value is a Go duration string, not the SDK's wire-format string a derived
// pattern would describe. See deriveConstraintValidators for why this is
// checked instead of "does a hand Go-duration validator exist": the two
// are not equivalent for a Computed-only attribute, which has custom_type
// but no hand validator to bounds-check against.
func attributeHasGoDurationCustomType(owner string, attribute json.RawMessage) (bool, error) {
	if len(attribute) == 0 {
		return false, nil
	}
	var body struct {
		CustomType struct {
			Type string `json:"type"`
		} `json:"custom_type"`
	}
	if err := json.Unmarshal(attribute, &body); err != nil {
		return false, fmt.Errorf("field %q attribute: %w", owner, err)
	}
	return strings.Contains(body.CustomType.Type, "GoDurationType"), nil
}

// oneOfSchemaDefinition renders the SDK table's order into the same Go
// expression the hand-transcribed form used: values quoted with Go string
// escaping for a string field, bare decimal for an int64 field.
func oneOfSchemaDefinition(terraformType string, constraint *bootstrapFieldConstraint) (schemaDefinition, importPath string, ok bool) {
	if constraint == nil {
		return "", "", false
	}
	switch terraformType {
	case "string":
		if len(constraint.Values) == 0 {
			return "", "", false
		}
		args := make([]string, len(constraint.Values))
		for i, value := range constraint.Values {
			args[i] = strconv.Quote(value)
		}
		return fmt.Sprintf("stringvalidator.OneOf(%s)", strings.Join(args, ", ")),
			"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator", true
	case "int64":
		if len(constraint.Int64Values) == 0 {
			return "", "", false
		}
		args := make([]string, len(constraint.Int64Values))
		for i, value := range constraint.Int64Values {
			args[i] = strconv.FormatInt(value, 10)
		}
		return fmt.Sprintf("int64validator.OneOf(%s)", strings.Join(args, ", ")),
			"github.com/hashicorp/terraform-plugin-framework-validators/int64validator", true
	default:
		return "", "", false
	}
}

// regexMatchesSchemaDefinition renders a constraint's pattern into a call to
// controllerregex.Matches -- the pattern verbatim, exactly as the SDK
// publishes it, with no rewriting in this file at all. controllerregex
// compiles it the way the controller itself reads it (Java's Pattern,
// Matcher.matches(), a full match) and does its own internal anchoring
// (\A(?:...)\z); this file used to anchor the pattern itself (^(?:...)$) for
// Go's RE2-backed RegexMatches, which only calls MatchString (a partial
// match) -- that whole responsibility, and the RE2-specific \d/\w and
// trailing-newline hazards it carried, now lives once in controllerregex,
// not duplicated here. See that package's doc comment for the full account.
//
// A pattern beside a value set is the SDK table's display form of that same
// set, not a separate rule -- oneOfSchemaDefinition already claims the field
// in that case, so this only fires when there is no value set at all. A
// pattern controllerregex cannot compile (an escape outside its translated
// grammar, or a construct regexp2 itself refuses) is refused, naming the
// surface, field, and pattern, rather than left to panic at schema-build
// time.
func regexMatchesSchemaDefinition(owner, terraformType string, constraint *bootstrapFieldConstraint) (schemaDefinition string, imports []customValidatorImport, ok bool, err error) {
	if terraformType != "string" {
		return "", nil, false, nil
	}
	if constraint == nil || constraint.Pattern == "" {
		return "", nil, false, nil
	}
	if len(constraint.Values) > 0 || len(constraint.Int64Values) > 0 {
		return "", nil, false, nil
	}
	if _, compileErr := controllerregex.Compile(constraint.Pattern); compileErr != nil {
		return "", nil, false, fmt.Errorf(
			"field %q constraint pattern %q is not compilable by controllerregex (%v) -- "+
				"set validators to \"none\" on this field to record the exception",
			owner, constraint.Pattern, compileErr,
		)
	}
	schemaDefinition = fmt.Sprintf("controllerregex.Matches(%s, \"\")", patternLiteral(constraint.Pattern))
	imports = []customValidatorImport{
		{Path: "github.com/ubiquiti-community/terraform-provider-unifi/internal/controllerregex"},
	}
	return schemaDefinition, imports, true, nil
}

// patternLiteral renders a pattern as a Go raw string literal so the SDK's
// backslash escapes pass through unchanged. Falls back to a quoted string on
// the one input a raw literal cannot hold: a pattern containing a backtick.
// None do today, but a future SDK bump might add one, and mis-rendering it
// would be a subtler bug than a fallback nobody has exercised yet.
func patternLiteral(pattern string) string {
	if !strings.Contains(pattern, "`") {
		return "`" + pattern + "`"
	}
	return strconv.Quote(pattern)
}

// validatorsSuppressed reports whether the attribute carries the literal
// "validators": "none" marker. Any other string value is refused rather than
// silently treated as no marker at all: a typo here would otherwise pass
// straight through to the generator as invalid input.
func validatorsSuppressed(owner string, attribute json.RawMessage) (bool, error) {
	if len(attribute) == 0 {
		return false, nil
	}
	var body struct {
		Validators json.RawMessage `json:"validators"`
	}
	if err := json.Unmarshal(attribute, &body); err != nil {
		return false, fmt.Errorf("field %q attribute: %w", owner, err)
	}
	if len(body.Validators) == 0 {
		return false, nil
	}
	var decoded any
	if err := json.Unmarshal(body.Validators, &decoded); err != nil {
		return false, fmt.Errorf("field %q validators: %w", owner, err)
	}
	literal, isString := decoded.(string)
	if !isString {
		// The normal validators array, nothing to suppress.
		return false, nil
	}
	if literal != "none" {
		return false, fmt.Errorf(
			"field %q declares validators %q: the only recognised string value is \"none\"",
			owner, literal,
		)
	}
	return true, nil
}

// stripValidators removes the "validators" key entirely. "none" is a marker
// for the compiler, not a value the generator understands, so it must not
// reach the emitted specification.
func stripValidators(owner string, attribute json.RawMessage) (json.RawMessage, error) {
	var body map[string]json.RawMessage
	if err := json.Unmarshal(attribute, &body); err != nil {
		return nil, fmt.Errorf("field %q attribute: %w", owner, err)
	}
	delete(body, "validators")
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("field %q attribute: %w", owner, err)
	}
	return encoded, nil
}

// appendDerivedValidator adds a derived validator (OneOf or RegexMatches) to
// the attribute's validators array, refusing if a hand-written validator of
// the same kind is already there -- hand transcription is no longer allowed
// to shadow the derived fact. conflictMarker identifies the kind by the Go
// expression substring it always contains (".OneOf(" or ".RegexMatches(");
// any other hand validator (Between, LengthAtLeast, a validator of the
// other derived kind, ...) is left exactly as it is and the derived one is
// appended beside it.
func appendDerivedValidator(owner string, attribute json.RawMessage, schemaDefinition string, imports []customValidatorImport, conflictMarker, kindLabel string) (json.RawMessage, error) {
	var body map[string]json.RawMessage
	if len(attribute) > 0 {
		if err := json.Unmarshal(attribute, &body); err != nil {
			return nil, fmt.Errorf("field %q attribute: %w", owner, err)
		}
	}
	if body == nil {
		body = map[string]json.RawMessage{}
	}

	var existing []customValidator
	if raw, present := body["validators"]; present {
		if err := json.Unmarshal(raw, &existing); err != nil {
			return nil, fmt.Errorf("field %q validators: %w", owner, err)
		}
		for _, entry := range existing {
			if entry.Custom == nil {
				continue
			}
			if strings.Contains(entry.Custom.SchemaDefinition, conflictMarker) {
				return nil, fmt.Errorf(
					"field %q hand-transcribes a %s validator that the SDK constraint table now "+
						"derives; delete the hand validator, or set validators to \"none\" to record a "+
						"deliberate exception:\n  hand:    %s\n  derived: %s",
					owner, kindLabel, entry.Custom.SchemaDefinition, schemaDefinition,
				)
			}
		}
	}
	existing = append(existing, customValidator{Custom: &customValidatorBody{
		Imports:          imports,
		SchemaDefinition: schemaDefinition,
	}})
	encodedValidators, err := json.Marshal(existing)
	if err != nil {
		return nil, err
	}
	body["validators"] = encodedValidators
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}
