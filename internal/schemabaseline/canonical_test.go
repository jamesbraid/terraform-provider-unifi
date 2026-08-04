package schemabaseline

import (
	"bytes"
	"strings"
	"testing"
)

const providerAddress = "registry.terraform.io/ubiquiti-community/unifi"

func TestCanonicalizeStripsOnlyCLIEnvelope(t *testing.T) {
	terraform := []byte(`{
		"format_version":"1.0",
		"provider_schemas":{
			"registry.terraform.io/ubiquiti-community/unifi":{
				"provider":{"version":0,"block":{"attributes":{"username":{"type":"string","optional":true}}}},
				"resource_schemas":{"unifi_dns_record":{"version":1,"block":{"attributes":{"ttl":{"type":"string","required":true}}}}},
				"data_source_schemas":{"unifi_dns_record":{"version":0,"block":{}}},
				"list_resource_schemas":{"unifi_dns_record":{"version":0,"block":{}}},
				"functions":{"normalize_name":{"return_type":"string"}},
				"action_schemas":{"restart":{"version":0,"block":{}}},
				"ephemeral_resource_schemas":{"session":{"version":0,"block":{}}}
			}
		},
		"diagnostics":[{"summary":"terraform-only envelope metadata"}]
	}`)
	tofu := []byte(`{
		"format_version":"1.1",
		"provider_schemas":{
			"registry.terraform.io/ubiquiti-community/unifi":{
				"ephemeral_resource_schemas":{"session":{"block":{},"version":0}},
				"action_schemas":{"restart":{"block":{},"version":0}},
				"functions":{"normalize_name":{"return_type":"string"}},
				"list_resource_schemas":{"unifi_dns_record":{"block":{},"version":0}},
				"data_source_schemas":{"unifi_dns_record":{"block":{},"version":0}},
				"resource_schemas":{"unifi_dns_record":{"block":{"attributes":{"ttl":{"required":true,"type":"string"}}},"version":1}},
				"provider":{"block":{"attributes":{"username":{"optional":true,"type":"string"}}},"version":0}
			}
		},
		"tofu_version":"1.12.1"
	}`)

	terraformCanonical, terraformDigests, err := Canonicalize(terraform, providerAddress)
	if err != nil {
		t.Fatal(err)
	}
	tofuCanonical, tofuDigests, err := Canonicalize(tofu, providerAddress)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(terraformCanonical, tofuCanonical) {
		t.Fatalf("canonical projections differ:\nterraform: %s\ntofu: %s", terraformCanonical, tofuCanonical)
	}
	if !bytes.Equal(terraformDigests, tofuDigests) {
		t.Fatalf("digest manifests differ:\nterraform: %s\ntofu: %s", terraformDigests, tofuDigests)
	}
	for _, category := range []string{
		`"provider"`,
		`"resource_schemas"`,
		`"data_source_schemas"`,
		`"list_resource_schemas"`,
		`"functions"`,
		`"action_schemas"`,
		`"ephemeral_resource_schemas"`,
	} {
		if !bytes.Contains(terraformCanonical, []byte(category)) {
			t.Errorf("canonical projection omitted %s", category)
		}
	}
	if bytes.Contains(terraformCanonical, []byte("format_version")) || bytes.Contains(terraformCanonical, []byte("diagnostics")) {
		t.Fatalf("canonical projection retained CLI envelope: %s", terraformCanonical)
	}
	for _, key := range []string{
		`"canonical_schema_sha256"`,
		`"provider"`,
		`"resource_schemas.unifi_dns_record"`,
		`"data_source_schemas.unifi_dns_record"`,
		`"list_resource_schemas.unifi_dns_record"`,
		`"functions.normalize_name"`,
		`"action_schemas.restart"`,
		`"ephemeral_resource_schemas.session"`,
	} {
		if !bytes.Contains(terraformDigests, []byte(key)) {
			t.Errorf("digest manifest omitted %s: %s", key, terraformDigests)
		}
	}
}

func TestCanonicalizeRetainsSemanticDifferences(t *testing.T) {
	optional := []byte(`{"provider_schemas":{"` + providerAddress + `":{"provider":{"block":{}},"resource_schemas":{"unifi_dns_record":{"block":{"attributes":{"ttl":{"type":"string","optional":true}}}}}}}}`)
	required := []byte(`{"provider_schemas":{"` + providerAddress + `":{"provider":{"block":{}},"resource_schemas":{"unifi_dns_record":{"block":{"attributes":{"ttl":{"type":"string","required":true}}}}}}}}`)

	optionalCanonical, optionalDigests, err := Canonicalize(optional, providerAddress)
	if err != nil {
		t.Fatal(err)
	}
	requiredCanonical, requiredDigests, err := Canonicalize(required, providerAddress)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(optionalCanonical, requiredCanonical) {
		t.Fatal("semantic schema change disappeared during canonicalization")
	}
	if bytes.Equal(optionalDigests, requiredDigests) {
		t.Fatal("semantic schema change did not alter digests")
	}
}

func TestCanonicalizeRejectsMissingOrInvalidProviderProjection(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "missing map", raw: `{}`, want: "provider_schemas"},
		{name: "missing address", raw: `{"provider_schemas":{}}`, want: providerAddress},
		{name: "non-object provider", raw: `{"provider_schemas":{"` + providerAddress + `":[]}}`, want: "object"},
		{name: "trailing JSON", raw: `{"provider_schemas":{}} true`, want: "after top-level"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := Canonicalize([]byte(test.raw), providerAddress)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Canonicalize() error = %v, want %q", err, test.want)
			}
		})
	}
}
