package providercompiler

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/controllerregex"
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

// TestCompileDerivesRegexMatchesFromConstraint is RegexMatches' positive
// control: a bootstrap field carries a pattern and no value set, policy has
// no hand validator, the compiler derives a controllerregex.Matches call from
// it -- the pattern verbatim, no anchoring or rewriting in this file, plus
// exactly the one import that expression needs. The pattern mirrors
// site_to_site_vpn's real x_ipsec_pre_shared_key constraint (unanchored,
// excludes quotes/apostrophe/space) -- the field this task's ledgered gap is
// about.
func TestCompileDerivesRegexMatchesFromConstraint(t *testing.T) {
	pattern := `[^\"\' ]+`
	result, err := Compile(CompileInput{
		Bootstrap: oneOfBootstrap(t, "key", map[string]any{"pattern": pattern}),
		Policy:    oneOfPolicy(t, "key", nil),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	validators := oneOfAttributeValidators(t, result.ProviderCodeSpec, "string", "name")
	if len(validators) != 1 {
		t.Fatalf("validators = %v, want exactly 1 derived entry", validators)
	}
	custom := jsonObject(validators[0]["custom"])
	wantDefinition := "controllerregex.Matches(`" + pattern + "`, \"\")"
	if got := jsonString(custom["schema_definition"]); got != wantDefinition {
		t.Fatalf("schema_definition = %q, want %q (pattern verbatim, unanchored)", got, wantDefinition)
	}
	imports := jsonArray(custom["imports"])
	if len(imports) != 1 {
		t.Fatalf("imports = %v, want exactly 1 (controllerregex)", imports)
	}
	wantImport := "github.com/ubiquiti-community/terraform-provider-unifi/internal/controllerregex"
	if got := jsonString(jsonObject(imports[0])["path"]); got != wantImport {
		t.Fatalf("imports[0] = %q, want %q", got, wantImport)
	}
}

// TestCompileRefusesAHandRegexMatchesShadowingAConstraint is the refusal
// path: a hand RegexMatches is still present in policy for a field the
// bootstrap also constrains via a pattern. The compiler must fail closed
// rather than silently keep the hand-transcribed expression, and must name
// the surface, the field, and both expressions.
func TestCompileRefusesAHandRegexMatchesShadowingAConstraint(t *testing.T) {
	_, err := Compile(CompileInput{
		Bootstrap: oneOfBootstrap(t, "key", map[string]any{"pattern": ".{1,128}"}),
		Policy: oneOfPolicy(t, "key", func(attribute map[string]any) {
			attribute["validators"] = []any{
				map[string]any{
					"custom": map[string]any{
						"imports": []any{
							map[string]any{"path": "regexp"},
							map[string]any{"path": "github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"},
						},
						"schema_definition": "stringvalidator.RegexMatches(regexp.MustCompile(`^.{1,64}$`), \"too long\")",
					},
				},
			}
		}),
	})
	if err == nil {
		t.Fatal("Compile() error = nil, want a refusal naming the shadowed hand RegexMatches")
	}
	for _, want := range []string{
		"unifi_dns_record", // surface
		"key",              // field
		"stringvalidator.RegexMatches(regexp.MustCompile(`^.{1,64}$`), \"too long\")", // hand
		"controllerregex.Matches(`.{1,128}`, \"\")",                                   // derived
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Compile() error = %v, want it to contain %q", err, want)
		}
	}
}

// TestCompileSuppressesRegexDerivationWhenValidatorsIsNone is the
// suppression path for RegexMatches: policy explicitly opts a patterned
// field out of derivation. The emitted attribute must carry no validators
// at all, same as the OneOf suppression path.
func TestCompileSuppressesRegexDerivationWhenValidatorsIsNone(t *testing.T) {
	result, err := Compile(CompileInput{
		Bootstrap: oneOfBootstrap(t, "key", map[string]any{"pattern": ".{1,128}"}),
		Policy: oneOfPolicy(t, "key", func(attribute map[string]any) {
			attribute["validators"] = "none"
		}),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if validators := oneOfAttributeValidators(t, result.ProviderCodeSpec, "string", "name"); len(validators) != 0 {
		t.Fatalf("validators = %v, want none: derivation was suppressed", validators)
	}
	if strings.Contains(string(result.ProviderCodeSpec), `"none"`) {
		t.Fatalf("provider code specification still carries the suppression marker: %s", result.ProviderCodeSpec)
	}
}

// TestCompileRefusesAPatternControllerregexCannotCompile is the refusal
// path now that lookaround patterns compile fine (controllerregex, unlike
// Go's RE2-based regexp package, understands them -- DHCPOption.code's real
// negative-lookahead constraint, the pattern this test used before this
// task, is no longer a usable fixture for "the compiler refuses"). What
// controllerregex still refuses is an escape outside its translated
// grammar -- \s here, never present in the real corpus (see
// internal/controllerregex's own positive control for the same fixture).
// The compiler must refuse, naming the surface, field, and pattern, rather
// than let a bad build panic at runtime.
func TestCompileRefusesAPatternControllerregexCannotCompile(t *testing.T) {
	pattern := `[\s]+`
	_, err := Compile(CompileInput{
		Bootstrap: oneOfBootstrap(t, "key", map[string]any{"pattern": pattern}),
		Policy:    oneOfPolicy(t, "key", nil),
	})
	if err == nil {
		t.Fatal("Compile() error = nil, want a refusal naming the uncompilable pattern")
	}
	// The error renders the pattern with %q, which doubles its backslash --
	// compare against the same rendering rather than the raw pattern string.
	for _, want := range []string{"unifi_dns_record", "key", fmt.Sprintf("%q", pattern), `\s`} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Compile() error = %v, want it to contain %q", err, want)
		}
	}
}

// TestCompileSkipsAnUncompilablePatternAlreadyCoveredByAHandValidator is the
// uncompilable-pattern path's other half: when the field already carries a
// hand validator, an uncompilable pattern is skipped rather than refused,
// and the hand validator survives untouched. This mirrored a real case
// before this task -- network.json's domain_name has a hand-written
// validators.DomainNameValidator() that was an RE2-safe reimplementation of
// the SDK's lookaround domain pattern, back when Go's RE2 engine could not
// compile it. controllerregex can, so that real pattern no longer exercises
// this path (see TestCompileAppendsDerivedRegexMatchesBesideAHandNonRegexValidator
// for what happens to it now: a second, derived validator appended beside
// the hand one); the fixture below is synthetic, the same kind of
// translator-grammar escape as the refusal test above, chosen only to keep
// this fallback covered as a fail-safe. Refusing here would force
// "validators": "none", which would delete the hand validator too (the
// suppression marker replaces the whole array, not just the derivation).
// The skip is not silent: it leaves a Notices entry naming the field, so a
// plain go generate run (and its CI log) shows it without anyone having to
// know this fallback exists.
func TestCompileSkipsAnUncompilablePatternAlreadyCoveredByAHandValidator(t *testing.T) {
	pattern := `[\S]{1,10}`
	result, err := Compile(CompileInput{
		Bootstrap: oneOfBootstrap(t, "key", map[string]any{"pattern": pattern}),
		Policy: oneOfPolicy(t, "key", func(attribute map[string]any) {
			attribute["validators"] = []any{
				map[string]any{
					"custom": map[string]any{
						"imports": []any{
							map[string]any{"path": "github.com/ubiquiti-community/terraform-provider-unifi/unifi/validators"},
						},
						"schema_definition": "validators.DomainNameValidator()",
					},
				},
			}
		}),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v, want the uncompilable pattern skipped, not refused", err)
	}
	validators := oneOfAttributeValidators(t, result.ProviderCodeSpec, "string", "name")
	if len(validators) != 1 {
		t.Fatalf("validators = %v, want exactly the untouched hand validator", validators)
	}
	custom := jsonObject(validators[0]["custom"])
	if got, want := jsonString(custom["schema_definition"]), "validators.DomainNameValidator()"; got != want {
		t.Fatalf("schema_definition = %q, want the hand validator %q unchanged", got, want)
	}
	wantNotice := "skipped unparsable pattern for unifi_dns_record.key: hand validator present"
	if len(result.Notices) != 1 || result.Notices[0] != wantNotice {
		t.Fatalf("Notices = %v, want exactly [%q]", result.Notices, wantNotice)
	}
}

// TestCompileAppendsDerivedRegexMatchesBesideAHandNonRegexValidator confirms
// the "only RegexMatches" boundary for the pattern derivation: a hand
// validator of some other kind (LengthBetween, say) is left exactly as it
// is, and the derived RegexMatches is appended beside it. Many real
// attributes carry both today.
func TestCompileAppendsDerivedRegexMatchesBesideAHandNonRegexValidator(t *testing.T) {
	result, err := Compile(CompileInput{
		Bootstrap: oneOfBootstrap(t, "value", map[string]any{"pattern": ".{1,256}"}),
		Policy: oneOfPolicy(t, "value", func(attribute map[string]any) {
			attribute["validators"] = []any{
				map[string]any{
					"custom": map[string]any{
						"imports": []any{
							map[string]any{"path": "github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"},
						},
						"schema_definition": "stringvalidator.LengthBetween(1, 256)",
					},
				},
			}
		}),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	validators := oneOfAttributeValidators(t, result.ProviderCodeSpec, "string", "value")
	if len(validators) != 2 {
		t.Fatalf("validators = %v, want the hand validator plus the derived one", validators)
	}
	var definitions []string
	for _, v := range validators {
		definitions = append(definitions, jsonString(jsonObject(v["custom"])["schema_definition"]))
	}
	wantHand := "stringvalidator.LengthBetween(1, 256)"
	wantDerived := "controllerregex.Matches(`.{1,256}`, \"\")"
	hasHand := definitions[0] == wantHand || definitions[1] == wantHand
	hasDerived := definitions[0] == wantDerived || definitions[1] == wantDerived
	if !hasHand || !hasDerived {
		t.Fatalf("validators = %v, want %q and %q", definitions, wantHand, wantDerived)
	}
}

// TestCompileSkipsPatternDerivationForAGoDurationTypedAttribute is a third
// silent-skip path, found by checking real shipped example values against
// the derived patterns: a field with CustomType timetypes.GoDurationType
// takes its config value as a Go duration string like "4h" or "3600s", but
// the SDK's constraint pattern describes the wire format the provider sends
// after converting that duration to seconds -- a bare digit string. The two
// are different representations of the same value, not the same string
// checked twice: "4h" fails a digits-only pattern and "3600" (what would
// pass it) is not valid Go duration syntax, so appending the derived
// RegexMatches would make every real value fail.
//
// This attribute carries no hand validator at all -- the Computed-only
// shape a data-source mirror takes, with nothing to hand-validate before
// it's ever written. A coordinator review caught that keying the skip on
// "does a hand GoDurationBetween/GoDurationMultipleOf validator exist"
// (the first fix) missed exactly this case: three real Computed-only
// attributes (network_ds's ipv6_ra_preferred_lifetime/ipv6_ra_valid_lifetime,
// radius_profile_ds's interim_update_interval) still carried the broken
// digits-only pattern, invisible to both the notice and the example check
// (a Computed-only attribute is never configured, so no example ever sets
// it). Keying on the attribute's own custom_type instead closes that gap.
func TestCompileSkipsPatternDerivationForAGoDurationTypedAttribute(t *testing.T) {
	result, err := Compile(CompileInput{
		Bootstrap: oneOfBootstrap(t, "value", map[string]any{"pattern": "[0-9]{1,4}"}),
		Policy: oneOfPolicy(t, "value", func(attribute map[string]any) {
			attribute["custom_type"] = map[string]any{
				"import": map[string]any{
					"path": "github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes",
				},
				"type":       "timetypes.GoDurationType{}",
				"value_type": "timetypes.GoDuration",
			}
		}),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v, want the pattern derivation skipped, not an error", err)
	}
	if validators := oneOfAttributeValidators(t, result.ProviderCodeSpec, "string", "value"); len(validators) != 0 {
		t.Fatalf("validators = %v, want none: no hand validator was present and derivation was skipped", validators)
	}
	wantNotice := "skipped pattern derivation for unifi_dns_record.value: Go-duration custom type"
	if len(result.Notices) != 1 || result.Notices[0] != wantNotice {
		t.Fatalf("Notices = %v, want exactly [%q]", result.Notices, wantNotice)
	}
}

// TestCompileSkipsPatternDerivationForAGoDurationTypedAttributeWithAHandValidator
// is the same skip, on the shape a resource-side (Optional) attribute
// actually takes: CustomType timetypes.GoDurationType alongside a hand
// validators.GoDurationBetween/GoDurationMultipleOf pair that does the real
// bounds-checking. The custom_type is still what triggers the skip -- the
// hand validator's presence is not the condition -- but its outcome must be
// the same as before: the hand validator survives untouched.
func TestCompileSkipsPatternDerivationForAGoDurationTypedAttributeWithAHandValidator(t *testing.T) {
	result, err := Compile(CompileInput{
		Bootstrap: oneOfBootstrap(t, "value", map[string]any{"pattern": "[0-9]{1,4}"}),
		Policy: oneOfPolicy(t, "value", func(attribute map[string]any) {
			attribute["custom_type"] = map[string]any{
				"import": map[string]any{
					"path": "github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes",
				},
				"type":       "timetypes.GoDurationType{}",
				"value_type": "timetypes.GoDuration",
			}
			attribute["validators"] = []any{
				map[string]any{
					"custom": map[string]any{
						"imports": []any{
							map[string]any{"path": "github.com/ubiquiti-community/terraform-provider-unifi/unifi/validators"},
							map[string]any{"path": "time"},
						},
						"schema_definition": "validators.GoDurationBetween(0, 3600*time.Second)",
					},
				},
			}
		}),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v, want the pattern derivation skipped, not an error", err)
	}
	validators := oneOfAttributeValidators(t, result.ProviderCodeSpec, "string", "value")
	if len(validators) != 1 {
		t.Fatalf("validators = %v, want exactly the untouched hand GoDuration validator", validators)
	}
	custom := jsonObject(validators[0]["custom"])
	if got, want := jsonString(custom["schema_definition"]), "validators.GoDurationBetween(0, 3600*time.Second)"; got != want {
		t.Fatalf("schema_definition = %q, want the hand validator %q unchanged", got, want)
	}
	wantNotice := "skipped pattern derivation for unifi_dns_record.value: Go-duration custom type"
	if len(result.Notices) != 1 || result.Notices[0] != wantNotice {
		t.Fatalf("Notices = %v, want exactly [%q]", result.Notices, wantNotice)
	}
}

// TestCompileEmitsAnAlreadyAnchoredPatternVerbatim confirms this file no
// longer rewrites a pattern's anchoring at all: a pattern that already
// anchors both ends (like x_uplink_password's real `^.{0,256}$`) is emitted
// exactly as the SDK published it, not wrapped into `^(?:^.{0,256}$)$` --
// that whole decision moved into controllerregex.Compile's own,
// unconditional `\A(?:...)\z` wrap (internal/controllerregex's own tests
// cover it; see TestAnchoredPatternRejectsATrailingNewline there).
func TestCompileEmitsAnAlreadyAnchoredPatternVerbatim(t *testing.T) {
	pattern := `^.{0,256}$`
	result, err := Compile(CompileInput{
		Bootstrap: oneOfBootstrap(t, "key", map[string]any{"pattern": pattern}),
		Policy:    oneOfPolicy(t, "key", nil),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	validators := oneOfAttributeValidators(t, result.ProviderCodeSpec, "string", "name")
	if len(validators) != 1 {
		t.Fatalf("validators = %v, want exactly 1 derived entry", validators)
	}
	custom := jsonObject(validators[0]["custom"])
	wantDefinition := "controllerregex.Matches(`^.{0,256}$`, \"\")"
	if got := jsonString(custom["schema_definition"]); got != wantDefinition {
		t.Fatalf("schema_definition = %q, want %q (pattern must not be rewritten)", got, wantDefinition)
	}
}

// TestCompileEmitsAPatternThatStillRejectsALeadingSpace is the
// anchoring path's behavioural proof, using site_to_site_vpn's real
// x_ipsec_pre_shared_key pattern. The controller validates the pattern as a
// full match; the schema definition now carries the raw, unanchored pattern
// verbatim and leaves anchoring to controllerregex.Compile internally
// (\A(?:...)\z) -- this test proves that pipeline, end to end through this
// package's own emission, still rejects a value with a leading space (which
// the controller rejects) the same way the old RE2-anchored form did. This
// is the ledgered gap the original RegexMatches derivation closed; this
// task only changed which engine enforces it.
func TestCompileEmitsAPatternThatStillRejectsALeadingSpace(t *testing.T) {
	pattern := `[^\"\' ]+`
	result, err := Compile(CompileInput{
		Bootstrap: oneOfBootstrap(t, "key", map[string]any{"pattern": pattern}),
		Policy:    oneOfPolicy(t, "key", nil),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	validators := oneOfAttributeValidators(t, result.ProviderCodeSpec, "string", "name")
	if len(validators) != 1 {
		t.Fatalf("validators = %v, want exactly 1 derived entry", validators)
	}
	custom := jsonObject(validators[0]["custom"])
	definition := jsonString(custom["schema_definition"])
	wantDefinition := "controllerregex.Matches(`" + pattern + "`, \"\")"
	if definition != wantDefinition {
		t.Fatalf("schema_definition = %q, want %q", definition, wantDefinition)
	}
	compiled, err := controllerregex.Compile(pattern)
	if err != nil {
		t.Fatalf("controllerregex.Compile(%q) error = %v", pattern, err)
	}
	if matched, err := compiled.MatchString(" leaked-with-leading-space"); err != nil || matched {
		t.Fatalf("pattern %q matched a value with a leading space (err=%v), want rejected", pattern, err)
	}
	if matched, err := compiled.MatchString("a-real-key"); err != nil || !matched {
		t.Fatalf("pattern %q rejected a plain value (err=%v), want accepted", pattern, err)
	}
	if matched, err := compiled.MatchString(`has"quote`); err != nil || matched {
		t.Fatalf("pattern %q matched a value containing a double quote (err=%v), want rejected", pattern, err)
	}
}

// TestCompileEmitsAnUnparenthesizedTopLevelAlternationThatStillForcesAFullMatch is the
// anchoring path's other behavioural proof: a leading ^ and a trailing $ on
// the pattern as a whole are not enough to force a full match under partial
// matching. "^A|B$" is the alternation of "^A" (no end anchor) and "B$" (no
// start anchor), the exact shape of go-unifi's DeviceConfigNetwork.netmask
// and Network.wan_netmask patterns -- both matched a value with trailing or
// leading garbage under the pre-RegexMatches-derivation provider. This test
// proves controllerregex.Compile's unconditional \A(?:...)\z wrap (not any
// rewriting in this package) is what forces the full match now.
func TestCompileEmitsAnUnparenthesizedTopLevelAlternationThatStillForcesAFullMatch(t *testing.T) {
	pattern := `^A|B$`
	result, err := Compile(CompileInput{
		Bootstrap: oneOfBootstrap(t, "key", map[string]any{"pattern": pattern}),
		Policy:    oneOfPolicy(t, "key", nil),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	validators := oneOfAttributeValidators(t, result.ProviderCodeSpec, "string", "name")
	if len(validators) != 1 {
		t.Fatalf("validators = %v, want exactly 1 derived entry", validators)
	}
	custom := jsonObject(validators[0]["custom"])
	definition := jsonString(custom["schema_definition"])
	wantDefinition := "controllerregex.Matches(`" + pattern + "`, \"\")"
	if definition != wantDefinition {
		t.Fatalf("schema_definition = %q, want %q (pattern must not be rewritten)", definition, wantDefinition)
	}
	compiled, err := controllerregex.Compile(pattern)
	if err != nil {
		t.Fatalf("controllerregex.Compile(%q) error = %v", pattern, err)
	}
	if matched, err := compiled.MatchString("Agarbage"); err != nil || matched {
		t.Fatalf("pattern %q matched %q on the unanchored first branch (err=%v), want rejected", pattern, "Agarbage", err)
	}
	if matched, err := compiled.MatchString("garbageB"); err != nil || matched {
		t.Fatalf("pattern %q matched %q on the unanchored second branch (err=%v), want rejected", pattern, "garbageB", err)
	}
	if matched, err := compiled.MatchString("A"); err != nil || !matched {
		t.Fatalf("pattern %q rejected %q (err=%v), want accepted", pattern, "A", err)
	}
	if matched, err := compiled.MatchString("B"); err != nil || !matched {
		t.Fatalf("pattern %q rejected %q (err=%v), want accepted", pattern, "B", err)
	}
}

// A pattern whose top-level branches are each already self-anchored (the
// real shape of go-unifi's DeviceIPv4.netmask, "^digits$|^$") is emitted
// exactly as published too -- this package makes no distinction between
// this shape and any other, now that anchoring is entirely
// controllerregex.Compile's job.
func TestCompileLeavesSelfAnchoredTopLevelBranchesUnwrapped(t *testing.T) {
	pattern := `^(0|[1-9]|1[0-9]|2[0-9]|3[0-2])$|^$`
	result, err := Compile(CompileInput{
		Bootstrap: oneOfBootstrap(t, "key", map[string]any{"pattern": pattern}),
		Policy:    oneOfPolicy(t, "key", nil),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	validators := oneOfAttributeValidators(t, result.ProviderCodeSpec, "string", "name")
	if len(validators) != 1 {
		t.Fatalf("validators = %v, want exactly 1 derived entry", validators)
	}
	custom := jsonObject(validators[0]["custom"])
	wantDefinition := "controllerregex.Matches(`" + pattern + "`, \"\")"
	if got := jsonString(custom["schema_definition"]); got != wantDefinition {
		t.Fatalf("schema_definition = %q, want %q (pattern must not be rewritten)", got, wantDefinition)
	}
}
