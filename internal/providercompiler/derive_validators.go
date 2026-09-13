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

// deriveConstraintValidators folds the bootstrap field's constraint into an
// attribute's validators -- OneOf from a fixed value set, Between from a
// contiguous numeric range, LengthBetween from character-count bounds,
// RegexMatches from a pattern when the constraint is none of those parsed
// shapes. A field the SDK constrains this way is derived here instead of
// hand-transcribed, so the compiler is the one place the fact can go stale.
// A hand validator of the same kind still present in policy is refused
// rather than silently shadowed by the derived one; policy may set
// "validators": "none" to record a deliberate exception and suppress every
// derivation for the field. For a collection (elementType non-empty,
// terraformType list or set) the same shapes derive against the element,
// wrapped in ValueStringsAre/ValueInt64sAre. attribute is returned
// unchanged when there is nothing to derive and nothing to suppress.
// notices collects observations worth a human seeing but not worth
// refusing the compile over -- see the uncompilable-pattern fallback
// below, the only producer today.
//
// The parsed shapes claim the field in order (values, then bounds or
// length, then the raw pattern) and exactly one derives: the SDK documents
// Values/Int64Values, Min/Max and MinLength/MaxLength as parsed forms of
// the same Pattern, so emitting two of them would state one controller
// rule twice. Where the SDK exports no parsed shape and no pattern,
// nothing derives -- its refusals (a hole-riddled alternation, a constant,
// a scan-edge range) are deliberate, and this file must not paper over
// them with a guess.
func deriveConstraintValidators(owner, terraformType, elementType string, constraint *bootstrapFieldConstraint, attribute json.RawMessage, notices *[]string) (json.RawMessage, error) {
	suppressed, err := validatorsSuppressed(owner, attribute)
	if err != nil {
		return nil, err
	}
	if suppressed {
		return stripValidators(owner, attribute)
	}
	if elementType != "" {
		return deriveElementConstraintValidators(owner, terraformType, elementType, constraint, attribute, notices)
	}
	if schemaDefinition, importPath, ok := oneOfSchemaDefinition(terraformType, constraint); ok {
		imports := []customValidatorImport{{Path: importPath}}
		return appendDerivedValidator(owner, attribute, schemaDefinition, imports, oneOfConflictMarkers, "OneOf")
	}
	if schemaDefinition, importPath, ok := betweenSchemaDefinition(terraformType, constraint); ok {
		imports := []customValidatorImport{{Path: importPath}}
		return appendDerivedValidator(owner, attribute, schemaDefinition, imports, betweenConflictMarkers, "Between")
	}
	// A GoDurationType attribute's config value is the human-typed duration
	// string ("4h", "3600s"), not the SDK's wire form the length or pattern
	// facts describe -- the fuller account is below, where the pattern path
	// repeats this check for the notice it emits.
	hasGoDuration, err := attributeHasGoDurationCustomType(owner, attribute)
	if err != nil {
		return nil, err
	}
	if schemaDefinition, importPath, ok := lengthSchemaDefinition(terraformType, constraint); ok && !hasGoDuration {
		imports := []customValidatorImport{{Path: importPath}}
		return appendDerivedValidator(owner, attribute, schemaDefinition, imports, lengthConflictMarkers, "Length")
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
	if hasGoDuration {
		if notices != nil {
			*notices = append(*notices, fmt.Sprintf(
				"skipped pattern derivation for %s: Go-duration custom type", owner,
			))
		}
		return attribute, nil
	}
	return appendDerivedValidator(owner, attribute, schemaDefinition, imports, regexConflictMarkers, "RegexMatches")
}

// Conflict markers, one set per derived kind: the Go expression substrings a
// hand validator of that kind always contains, dot included so a different
// identifier merely ending in the same word (AtLeastOneOf, GoDurationBetween)
// never matches. The regex kind has two spellings -- the framework's
// RegexMatches and this provider's controllerregex.Matches -- and a hand
// transcription in either shadows the same derived fact.
var (
	oneOfConflictMarkers       = []string{".OneOf("}
	betweenConflictMarkers     = []string{".Between("}
	lengthConflictMarkers      = []string{".Length"}
	regexConflictMarkers       = []string{".RegexMatches(", "controllerregex.Matches("}
	sizeAtLeastConflictMarkers = []string{".SizeAtLeast("}
)

// injectSizeAtLeast folds a measured min_items count into a list or set
// attribute's validators as SizeAtLeast(count) -- the plan-time check the
// controller's own minimum implies. The validator package follows the
// collection kind (listvalidator for a list, setvalidator for a set); a min
// on anything else is refused rather than dropped, since the artifact then
// disagrees with the served shape. A hand-transcribed SizeAtLeast is refused
// the same way every other derived validator refuses its hand twin.
func injectSizeAtLeast(owner, terraformType string, attribute json.RawMessage, count int) (json.RawMessage, error) {
	var pkg string
	switch terraformType {
	case "list", "list_nested":
		pkg = "listvalidator"
	case "set", "set_nested":
		pkg = "setvalidator"
	default:
		return nil, fmt.Errorf(
			"field %q records min_items %d but is terraform_type %q, not a list or set",
			owner, count, terraformType,
		)
	}
	schemaDefinition := fmt.Sprintf("%s.SizeAtLeast(%d)", pkg, count)
	imports := []customValidatorImport{
		{Path: "github.com/hashicorp/terraform-plugin-framework-validators/" + pkg},
	}
	return appendDerivedValidator(owner, attribute, schemaDefinition, imports, sizeAtLeastConflictMarkers, "SizeAtLeast")
}

// deriveElementConstraintValidators is deriveConstraintValidators for a
// collection: the same shapes, derived against the element type and wrapped
// in the collection's element validator (listvalidator/setvalidator
// ValueStringsAre or ValueInt64sAre). The conflict markers are the inner
// kind's, not the wrapper's: a hand ValueStringsAre carrying the same
// derived kind inside is a transcription of the same fact and is refused,
// while one carrying a provider-opinion validator the SDK has no shape for
// (a semantic CIDR or MAC check, say) stands beside the derived one.
func deriveElementConstraintValidators(owner, terraformType, elementType string, constraint *bootstrapFieldConstraint, attribute json.RawMessage, notices *[]string) (json.RawMessage, error) {
	var wrapper string
	switch terraformType {
	case "list":
		wrapper = "listvalidator"
	case "set":
		wrapper = "setvalidator"
	default:
		// collectionTerraformType has already refused everything else; a
		// value reaching here means the call site changed under this file.
		return nil, fmt.Errorf(
			"field %q: collection terraform_type %q cannot carry element validators, want list or set",
			owner, terraformType,
		)
	}
	wrapperImport := customValidatorImport{Path: "github.com/hashicorp/terraform-plugin-framework-validators/" + wrapper}
	var method string
	switch elementType {
	case "string":
		method = "ValueStringsAre"
	case "int64":
		method = "ValueInt64sAre"
	default:
		return nil, fmt.Errorf(
			"field %q: element type %q cannot carry derived element validators, want string or int64",
			owner, elementType,
		)
	}
	wrap := func(inner string, innerImports []customValidatorImport) (string, []customValidatorImport) {
		return fmt.Sprintf("%s.%s(%s)", wrapper, method, inner),
			append([]customValidatorImport{wrapperImport}, innerImports...)
	}
	if inner, importPath, ok := oneOfSchemaDefinition(elementType, constraint); ok {
		schemaDefinition, imports := wrap(inner, []customValidatorImport{{Path: importPath}})
		return appendDerivedValidator(owner, attribute, schemaDefinition, imports, oneOfConflictMarkers, "element OneOf")
	}
	if inner, importPath, ok := betweenSchemaDefinition(elementType, constraint); ok {
		schemaDefinition, imports := wrap(inner, []customValidatorImport{{Path: importPath}})
		return appendDerivedValidator(owner, attribute, schemaDefinition, imports, betweenConflictMarkers, "element Between")
	}
	if inner, importPath, ok := lengthSchemaDefinition(elementType, constraint); ok {
		schemaDefinition, imports := wrap(inner, []customValidatorImport{{Path: importPath}})
		return appendDerivedValidator(owner, attribute, schemaDefinition, imports, lengthConflictMarkers, "element Length")
	}
	inner, innerImports, ok, err := regexMatchesSchemaDefinition(owner, elementType, constraint)
	if err != nil {
		// The same skip-or-refuse rule the scalar path applies to a pattern
		// controllerregex cannot compile; see the account there.
		if hasHand, handErr := attributeHasValidators(attribute); handErr == nil && hasHand {
			if notices != nil {
				*notices = append(*notices, fmt.Sprintf(
					"skipped unparsable element pattern for %s: hand validator present", owner,
				))
			}
			return attribute, nil
		}
		return nil, err
	}
	if !ok {
		return attribute, nil
	}
	schemaDefinition, imports := wrap(inner, innerImports)
	return appendDerivedValidator(owner, attribute, schemaDefinition, imports, regexConflictMarkers, "element RegexMatches")
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

// betweenSchemaDefinition renders the SDK's contiguous numeric range into
// the same Go expression the hand-transcribed form used. Only an int64
// member takes it: a numeric wire served as a string keeps its pattern (the
// range's display form already enforces it there), and a Go-duration
// attribute's bounds are in wire units the config value does not use, so
// deriving them would need the policy's unit decision -- those stay with
// their hand GoDurationBetween validators.
func betweenSchemaDefinition(terraformType string, constraint *bootstrapFieldConstraint) (schemaDefinition, importPath string, ok bool) {
	if constraint == nil || !constraint.HasBounds || terraformType != "int64" {
		return "", "", false
	}
	return fmt.Sprintf("int64validator.Between(%s, %s)",
			strconv.FormatInt(constraint.Min, 10), strconv.FormatInt(constraint.Max, 10)),
		"github.com/hashicorp/terraform-plugin-framework-validators/int64validator", true
}

// lengthSchemaDefinition renders the SDK's character-count bounds into
// LengthBetween. The SDK sets HasLength only when the whole pattern is the
// bare length shape .{min,max} (anchored or not; verified across both
// constraint tables), so claiming the field here instead of deriving that
// pattern loses nothing and reads back to the practitioner as a length
// rule, not a regex.
func lengthSchemaDefinition(terraformType string, constraint *bootstrapFieldConstraint) (schemaDefinition, importPath string, ok bool) {
	if constraint == nil || !constraint.HasLength || terraformType != "string" {
		return "", "", false
	}
	return fmt.Sprintf("stringvalidator.LengthBetween(%s, %s)",
			strconv.FormatInt(constraint.MinLength, 10), strconv.FormatInt(constraint.MaxLength, 10)),
		"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator", true
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

// appendDerivedValidator adds a derived validator to the attribute's
// validators array, refusing if a hand-written validator of the same kind
// is already there -- hand transcription is no longer allowed to shadow the
// derived fact. conflictMarkers identify the kind by the Go expression
// substrings it always contains (see the marker sets above); a hand
// validator of any other kind -- a cross-field rule, a structural size
// bound, a semantic check the SDK has no shape for -- is left exactly as
// it is and the derived one is appended beside it.
func appendDerivedValidator(owner string, attribute json.RawMessage, schemaDefinition string, imports []customValidatorImport, conflictMarkers []string, kindLabel string) (json.RawMessage, error) {
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
			for _, marker := range conflictMarkers {
				if strings.Contains(entry.Custom.SchemaDefinition, marker) {
					return nil, fmt.Errorf(
						"field %q hand-transcribes a %s validator that the SDK constraint table now "+
							"derives; delete the hand validator, or set validators to \"none\" to record a "+
							"deliberate exception:\n  hand:    %s\n  derived: %s",
						owner, kindLabel, entry.Custom.SchemaDefinition, schemaDefinition,
					)
				}
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
