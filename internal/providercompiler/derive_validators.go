package providercompiler

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
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
// unchanged when there is nothing to derive and nothing to suppress.
func deriveConstraintValidators(owner, terraformType string, constraint *bootstrapFieldConstraint, attribute json.RawMessage) (json.RawMessage, error) {
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
		// A pattern Go's RE2 engine cannot express only refuses when
		// there's nothing else validating the field. An attribute that
		// already carries a hand validator (of any kind) is trusted to
		// already cover what the uncompilable pattern was expressing --
		// e.g. network.domain_name's hand DomainNameValidator is a
		// hand-written RE2-safe equivalent of the SDK's lookaround domain
		// pattern. "validators": "none" replaces the whole array, so it
		// cannot suppress just the derivation without also deleting that;
		// skipping silently here is what lets the hand validator stand.
		// A field with no validators at all still refuses, naming the
		// field, so the gap becomes a recorded "validators": "none"
		// decision instead of shipping silently unvalidated.
		if hasHand, handErr := attributeHasValidators(attribute); handErr == nil && hasHand {
			return attribute, nil
		}
		return nil, err
	}
	if !ok {
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

// regexMatchesSchemaDefinition renders a constraint's pattern into the same
// Go expression a hand-transcribed RegexMatches used: a raw string literal
// so the SDK's own backslash escapes need no translation, plus both imports
// the expression needs (the standard library's regexp package for
// MustCompile, and stringvalidator for RegexMatches itself).
//
// A pattern beside a value set is the SDK table's display form of that same
// set, not a separate rule -- oneOfSchemaDefinition already claims the field
// in that case, so this only fires when there is no value set at all. The
// controller validates a pattern as a full match; Go's RegexMatches calls
// MatchString, a partial match, so an unanchored pattern is wrapped in
// ^(?:...)$ before it is compiled -- otherwise a value like a leading space
// on site_to_site_vpn's pre_shared_key would pass Terraform's validation
// even though the controller itself rejects it. A pattern Go's RE2 engine
// cannot express (the controller's own dialect allows lookaround, which RE2
// does not) is refused, naming the surface, field, and pattern, rather than
// left to panic MustCompile at runtime.
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
	anchored := anchorPattern(constraint.Pattern)
	if _, compileErr := regexp.Compile(anchored); compileErr != nil {
		return "", nil, false, fmt.Errorf(
			"field %q constraint pattern %q is not compilable by Go's regexp package (%v); the "+
				"controller's regex dialect includes syntax RE2 does not support (e.g. lookaround) -- "+
				"set validators to \"none\" on this field to record the exception",
			owner, constraint.Pattern, compileErr,
		)
	}
	schemaDefinition = fmt.Sprintf("stringvalidator.RegexMatches(regexp.MustCompile(%s), \"\")", patternLiteral(anchored))
	imports = []customValidatorImport{
		{Path: "regexp"},
		{Path: "github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"},
	}
	return schemaDefinition, imports, true, nil
}

// anchorPattern wraps a pattern in ^(?:...)$ unless it already anchors both
// ends -- see regexMatchesSchemaDefinition for why an unanchored pattern is
// unsafe to compile as-is.
func anchorPattern(pattern string) string {
	if strings.HasPrefix(pattern, "^") && strings.HasSuffix(pattern, "$") {
		return pattern
	}
	return "^(?:" + pattern + ")$"
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
