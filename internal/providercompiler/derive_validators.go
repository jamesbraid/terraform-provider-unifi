package providercompiler

import (
	"encoding/json"
	"fmt"
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

// deriveOneOfValidator folds the bootstrap field's constraint into a scalar
// attribute's validators. A field the SDK constrains to a fixed value set is
// derived here instead of hand-transcribed, so the compiler is the one place
// that value set can go stale. A hand OneOf still present in policy is
// refused rather than silently shadowed by the derived one; policy may set
// "validators": "none" to record a deliberate exception and suppress
// derivation entirely. attribute is returned unchanged when there is
// nothing to derive and nothing to suppress.
func deriveOneOfValidator(owner, terraformType string, constraint *bootstrapFieldConstraint, attribute json.RawMessage) (json.RawMessage, error) {
	suppressed, err := validatorsSuppressed(owner, attribute)
	if err != nil {
		return nil, err
	}
	if suppressed {
		return stripValidators(owner, attribute)
	}
	schemaDefinition, importPath, ok := oneOfSchemaDefinition(terraformType, constraint)
	if !ok {
		return attribute, nil
	}
	return appendOneOfValidator(owner, attribute, schemaDefinition, importPath)
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

// appendOneOfValidator adds the derived OneOf to the attribute's validators
// array, refusing if a hand-written OneOf is already there -- hand
// transcription is no longer allowed to shadow the derived fact. Any other
// hand validator (Between, LengthAtLeast, a regex, ...) is left exactly as
// it is; this task touches only OneOf.
func appendOneOfValidator(owner string, attribute json.RawMessage, schemaDefinition, importPath string) (json.RawMessage, error) {
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
			if strings.Contains(entry.Custom.SchemaDefinition, ".OneOf(") {
				return nil, fmt.Errorf(
					"field %q hand-transcribes a OneOf validator that the SDK constraint table now "+
						"derives; delete the hand validator, or set validators to \"none\" to record a "+
						"deliberate exception:\n  hand:    %s\n  derived: %s",
					owner, entry.Custom.SchemaDefinition, schemaDefinition,
				)
			}
		}
	}
	existing = append(existing, customValidator{Custom: &customValidatorBody{
		Imports:          []customValidatorImport{{Path: importPath}},
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
