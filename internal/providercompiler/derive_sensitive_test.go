package providercompiler

import (
	"encoding/json"
	"strings"
	"testing"
)

// The controller's own sensitivity declaration arrives on bootstrap fields
// (cmd/sdk-bootstrap copies it from unifi.SensitiveFieldsByCollection); the
// compiler emits it, refuses a hand flag that duplicates it, and leaves a
// hand flag the controller does not declare alone -- the provider may mark
// more than the controller, never less.

func Test_deriveSensitive(t *testing.T) {
	t.Run("no declaration passes the attribute through untouched", func(t *testing.T) {
		attribute := json.RawMessage(`{"computed_optional_required": "optional"}`)
		got, err := deriveSensitive("unifi_probe.x_token", false, attribute)
		if err != nil {
			t.Fatalf("deriveSensitive() error = %v", err)
		}
		if string(got) != string(attribute) {
			t.Errorf("deriveSensitive() = %s, want the input unchanged", got)
		}
	})
	t.Run("a declaration marks the attribute", func(t *testing.T) {
		got, err := deriveSensitive("unifi_probe.x_token", true,
			json.RawMessage(`{"computed_optional_required": "optional"}`))
		if err != nil {
			t.Fatalf("deriveSensitive() error = %v", err)
		}
		var body struct {
			Sensitive bool `json:"sensitive"`
		}
		if err := json.Unmarshal(got, &body); err != nil || !body.Sensitive {
			t.Errorf("deriveSensitive() = %s, want sensitive true (err %v)", got, err)
		}
	})
	t.Run("a declaration with no attribute body builds one", func(t *testing.T) {
		got, err := deriveSensitive("unifi_probe.x_token", true, nil)
		if err != nil {
			t.Fatalf("deriveSensitive() error = %v", err)
		}
		if string(got) != `{"sensitive":true}` {
			t.Errorf("deriveSensitive() = %s, want {\"sensitive\":true}", got)
		}
	})
	t.Run("a hand flag duplicating the declaration is refused", func(t *testing.T) {
		_, err := deriveSensitive("unifi_probe.x_token", true,
			json.RawMessage(`{"sensitive": true}`))
		if err == nil {
			t.Fatal("deriveSensitive() accepted a hand flag the controller already declares")
		}
		for _, want := range []string{"unifi_probe.x_token", "delete the hand entry"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("deriveSensitive() error = %v, want it to say %q", err, want)
			}
		}
	})
}

// sensitiveInput builds the standard fixture pair with one extra observed
// string field, x_token, declared however the test needs: candidate marks it
// x_-prefixed on the bootstrap, declared carries the controller's verdict,
// and handFlag hand-sets "sensitive": true in its policy attribute.
func sensitiveInput(t *testing.T, declared, handFlag bool) CompileInput {
	t.Helper()
	var bootstrap map[string]any
	if err := json.Unmarshal(testBootstrap(t, dnsFieldNames()), &bootstrap); err != nil {
		t.Fatal(err)
	}
	resource := jsonObject(bootstrap["resource"])
	resource["fields"] = append(jsonArray(resource["fields"]), map[string]any{
		"name":             "x_token",
		"type":             "string",
		"secret_candidate": true,
		"sensitive":        declared,
	})

	policy := testPolicyObject(dnsFieldNames(), testSpecificationDigest)
	attribute := map[string]any{"computed_optional_required": "optional"}
	if handFlag {
		attribute["sensitive"] = true
	}
	policy["fields"] = append(jsonArray(policy["fields"]), map[string]any{
		"structural_name": "x_token",
		"terraform_name":  "token",
		"disposition":     "managed",
		"attribute":       attribute,
	})
	return CompileInput{Bootstrap: mustJSON(t, bootstrap), Policy: mustJSON(t, policy)}
}

func specSensitive(t *testing.T, spec []byte, attribute string) bool {
	t.Helper()
	var document struct {
		Resources []struct {
			Schema struct {
				Attributes []struct {
					Name   string `json:"name"`
					String struct {
						Sensitive bool `json:"sensitive"`
					} `json:"string"`
				} `json:"attributes"`
			} `json:"schema"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(spec, &document); err != nil {
		t.Fatal(err)
	}
	for _, entry := range document.Resources[0].Schema.Attributes {
		if entry.Name == attribute {
			return entry.String.Sensitive
		}
	}
	t.Fatalf("specification carries no attribute %q", attribute)
	return false
}

// TestCompileMarksADeclaredSecretWithoutAHandFlag is the derivation working
// end to end: a bootstrap-declared secret exposed by policy with no hand
// flag compiles -- the secret-candidate gate accepts the derived disposition
// -- and the emitted attribute is sensitive.
func TestCompileMarksADeclaredSecretWithoutAHandFlag(t *testing.T) {
	result, err := Compile(sensitiveInput(t, true, false))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if !specSensitive(t, result.ProviderCodeSpec, "token") {
		t.Error("the declared secret was emitted without sensitive: true")
	}
}

// TestCompileRefusesAHandFlagTheControllerAlreadyDeclares: the duplicate
// would go stale the day the declaration moves, so it is refused, naming
// the field so the author knows which hand entry to delete.
func TestCompileRefusesAHandFlagTheControllerAlreadyDeclares(t *testing.T) {
	_, err := Compile(sensitiveInput(t, true, true))
	if err == nil {
		t.Fatal("Compile() accepted a hand sensitive flag on a controller-declared field")
	}
	for _, want := range []string{"x_token", "delete the hand entry"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Compile() error = %v, want it to say %q", err, want)
		}
	}
}

// TestCompileKeepsAHandFlagTheControllerDoesNotDeclare is outcome (b) of the
// derivation: provider-opinion sensitivity survives, and still satisfies the
// secret-candidate gate on its own.
func TestCompileKeepsAHandFlagTheControllerDoesNotDeclare(t *testing.T) {
	result, err := Compile(sensitiveInput(t, false, true))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if !specSensitive(t, result.ProviderCodeSpec, "token") {
		t.Error("the provider-opinion sensitive flag was dropped")
	}
}
