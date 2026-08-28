package providercompiler

import (
	"encoding/json"
	"strings"
	"testing"
)

// oneOfBootstrap builds the dns_record bootstrap fixture with one field's
// constraint attached -- the three derivation paths turn on that one field,
// so the rest of the surface stays the plain testBootstrap shape.
func oneOfBootstrap(t *testing.T, field string, constraint map[string]any) []byte {
	t.Helper()
	fields := make([]map[string]any, 0, len(dnsFieldNames()))
	for _, name := range dnsFieldNames() {
		fieldType := "int64"
		switch name {
		case "enabled":
			fieldType = "bool"
		case "key", "record_type", "value":
			fieldType = "string"
		}
		entry := map[string]any{"name": name, "type": fieldType}
		if name == field {
			entry["constraint"] = constraint
		}
		fields = append(fields, entry)
	}
	return mustJSON(t, map[string]any{
		"format_version": 1,
		"source": map[string]any{
			"repository":           "github.com/ubiquiti-community/go-unifi",
			"version":              "v1.103.0",
			"commit":               "e255518385e0104eb838be56c2a491de158f3194",
			"specification_sha256": testSpecificationDigest,
		},
		"resource": map[string]any{
			"name":   "unifi_dns_record",
			"fields": fields,
		},
	})
}

// oneOfPolicy returns the dns_record policy fixture with mutate applied to
// the named field's attribute, so a test can add a hand validator or a
// suppression marker without hand-authoring the whole policy.
func oneOfPolicy(t *testing.T, field string, mutate func(attribute map[string]any)) []byte {
	t.Helper()
	document := testPolicyObject(dnsFieldNames(), testSpecificationDigest)
	for _, raw := range jsonArray(document["fields"]) {
		entry := jsonObject(raw)
		if entry["structural_name"] != field {
			continue
		}
		attribute := jsonObject(entry["attribute"])
		if mutate != nil {
			mutate(attribute)
		}
	}
	return mustJSON(t, document)
}

// oneOfAttributeValidators decodes the validators array (if any) of one
// attribute out of a compiled provider code specification.
func oneOfAttributeValidators(t *testing.T, spec []byte, terraformType, name string) []map[string]any {
	t.Helper()
	var document struct {
		Resources []struct {
			Schema struct {
				Attributes []map[string]json.RawMessage `json:"attributes"`
			} `json:"schema"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(spec, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Resources) != 1 {
		t.Fatalf("resources = %d, want 1", len(document.Resources))
	}
	for _, attribute := range document.Resources[0].Schema.Attributes {
		var attributeName string
		if err := json.Unmarshal(attribute["name"], &attributeName); err != nil {
			t.Fatal(err)
		}
		if attributeName != name {
			continue
		}
		var body struct {
			Validators []map[string]any `json:"validators"`
		}
		if err := json.Unmarshal(attribute[terraformType], &body); err != nil {
			t.Fatal(err)
		}
		return body.Validators
	}
	t.Fatalf("attribute %q is absent from the specification", name)
	return nil
}

// TestCompileDerivesOneOfFromConstraint is the positive-control path: no
// hand OneOf in policy, the bootstrap field carries a constraint, the
// compiler emits the same shape the hand-transcribed form used to.
func TestCompileDerivesOneOfFromConstraint(t *testing.T) {
	tests := map[string]struct {
		field          string
		terraformType  string
		constraint     map[string]any
		wantDefinition string
		wantImport     string
	}{
		"string": {
			field:          "record_type",
			terraformType:  "string",
			constraint:     map[string]any{"values": []string{"A", "CNAME", "TXT"}},
			wantDefinition: `stringvalidator.OneOf("A", "CNAME", "TXT")`,
			wantImport:     "github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator",
		},
		"int64": {
			field:          "weight",
			terraformType:  "int64",
			constraint:     map[string]any{"int64_values": []int64{0, 1, 2}},
			wantDefinition: `int64validator.OneOf(0, 1, 2)`,
			wantImport:     "github.com/hashicorp/terraform-plugin-framework-validators/int64validator",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			result, err := Compile(CompileInput{
				Bootstrap: oneOfBootstrap(t, test.field, test.constraint),
				Policy:    oneOfPolicy(t, test.field, nil),
			})
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			validators := oneOfAttributeValidators(t, result.ProviderCodeSpec, test.terraformType, test.field)
			if len(validators) != 1 {
				t.Fatalf("validators = %v, want exactly 1 derived entry", validators)
			}
			custom := jsonObject(validators[0]["custom"])
			if got := jsonString(custom["schema_definition"]); got != test.wantDefinition {
				t.Fatalf("schema_definition = %q, want %q", got, test.wantDefinition)
			}
			imports := jsonArray(custom["imports"])
			if len(imports) != 1 || jsonString(jsonObject(imports[0])["path"]) != test.wantImport {
				t.Fatalf("imports = %v, want exactly [%q]", imports, test.wantImport)
			}
		})
	}
}

// TestCompileRefusesAHandOneOfShadowingAConstraint is the refusal path: a
// hand OneOf is still present in policy for a field the bootstrap also
// constrains. The compiler must fail closed rather than silently keep the
// (possibly stale) hand-transcribed set, and must name the surface, the
// field, and both value sets so the fix is obvious from the message alone.
func TestCompileRefusesAHandOneOfShadowingAConstraint(t *testing.T) {
	_, err := Compile(CompileInput{
		Bootstrap: oneOfBootstrap(t, "record_type", map[string]any{"values": []string{"A", "CNAME", "TXT"}}),
		Policy: oneOfPolicy(t, "record_type", func(attribute map[string]any) {
			attribute["validators"] = []any{
				map[string]any{
					"custom": map[string]any{
						"imports": []any{
							map[string]any{"path": "github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"},
						},
						"schema_definition": `stringvalidator.OneOf("A", "CNAME")`,
					},
				},
			}
		}),
	})
	if err == nil {
		t.Fatal("Compile() error = nil, want a refusal naming the shadowed hand OneOf")
	}
	for _, want := range []string{
		"unifi_dns_record",                           // surface
		"record_type",                                // field
		`stringvalidator.OneOf("A", "CNAME")`,        // hand set (stale: missing TXT)
		`stringvalidator.OneOf("A", "CNAME", "TXT")`, // derived set
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Compile() error = %v, want it to contain %q", err, want)
		}
	}
}

// TestCompileSuppressesDerivationWhenValidatorsIsNone is the suppression
// path: policy explicitly opts a constrained field out of derivation. The
// emitted attribute must carry no validators at all -- "none" is a marker
// for the compiler, not a value the generator understands.
func TestCompileSuppressesDerivationWhenValidatorsIsNone(t *testing.T) {
	result, err := Compile(CompileInput{
		Bootstrap: oneOfBootstrap(t, "record_type", map[string]any{"values": []string{"A", "CNAME", "TXT"}}),
		Policy: oneOfPolicy(t, "record_type", func(attribute map[string]any) {
			attribute["validators"] = "none"
		}),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if validators := oneOfAttributeValidators(t, result.ProviderCodeSpec, "string", "record_type"); len(validators) != 0 {
		t.Fatalf("validators = %v, want none: derivation was suppressed", validators)
	}
	// The "none" marker itself must not leak into the specification.
	if strings.Contains(string(result.ProviderCodeSpec), `"none"`) {
		t.Fatalf("provider code specification still carries the suppression marker: %s", result.ProviderCodeSpec)
	}
}

// TestCompileAppendsDerivedOneOfBesideAHandNonOneOfValidator confirms the
// "only OneOf" boundary: a hand validator that is not a OneOf is left
// exactly as it is, and the derived OneOf is appended beside it rather than
// replacing the list.
func TestCompileAppendsDerivedOneOfBesideAHandNonOneOfValidator(t *testing.T) {
	result, err := Compile(CompileInput{
		Bootstrap: oneOfBootstrap(t, "weight", map[string]any{"int64_values": []int64{0, 1, 2}}),
		Policy: oneOfPolicy(t, "weight", func(attribute map[string]any) {
			attribute["validators"] = []any{
				map[string]any{
					"custom": map[string]any{
						"imports": []any{
							map[string]any{"path": "github.com/hashicorp/terraform-plugin-framework-validators/int64validator"},
						},
						"schema_definition": "int64validator.AtLeast(0)",
					},
				},
			}
		}),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	validators := oneOfAttributeValidators(t, result.ProviderCodeSpec, "int64", "weight")
	if len(validators) != 2 {
		t.Fatalf("validators = %v, want the hand validator plus the derived one", validators)
	}
	var definitions []string
	for _, v := range validators {
		definitions = append(definitions, jsonString(jsonObject(v["custom"])["schema_definition"]))
	}
	wantHand, wantDerived := "int64validator.AtLeast(0)", "int64validator.OneOf(0, 1, 2)"
	hasHand := definitions[0] == wantHand || definitions[1] == wantHand
	hasDerived := definitions[0] == wantDerived || definitions[1] == wantDerived
	if !hasHand || !hasDerived {
		t.Fatalf("validators = %v, want %q and %q", definitions, wantHand, wantDerived)
	}
}
