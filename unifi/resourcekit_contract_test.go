package unifi

// The contract between the compiler's mapping artifact and the descriptor a
// resource is generated from: a field can be added to a policy and mapped by
// nobody, serving an attribute the resource never sends or reads back as a
// silent permanent diff. Checked against
// provider-codegen/generated/<name>.mapping.json rather than by asking the
// generator what it generated, since the latter would agree with itself by
// construction.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type mappingArtifact struct {
	SurfaceKind string `json:"surface_kind"`
	SurfaceName string `json:"surface_name"`
	Resource    string `json:"resource"`
	Fields      []struct {
		StructuralName string `json:"structural_name"`
		TerraformName  string `json:"terraform_name"`
		StructuralType string `json:"structural_type"`
		TerraformType  string `json:"terraform_type"`
		Disposition    string `json:"disposition"`
	} `json:"fields"`
	ProviderOwned []struct {
		TerraformName string `json:"terraform_name"`
		Disposition   string `json:"disposition"`
		Generated     bool   `json:"generated"`
	} `json:"provider_owned"`
}

func readMapping(t *testing.T, artifact string) mappingArtifact {
	t.Helper()
	path := filepath.Join("..", "provider-codegen", "generated", artifact+".mapping.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the mapping artifact is the oracle for this check and it is unreadable: %v", err)
	}
	var parsed mappingArtifact
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if len(parsed.Fields) == 0 {
		t.Fatalf("%s declares no fields, so every comparison below would hold vacuously", path)
	}
	return parsed
}

// kitContract is one row: wire names come from the SPEC rather than
// generated source, since the spec is what the provider actually runs -- a
// descriptor that compiles but is never referenced would pass a
// source-level check.
type kitContract struct {
	artifact  string
	typeName  string
	wires     []string
	idWire    string
	bootstrap string // "" when the surface is compiled from a catalog instead
}

func kitContracts() map[string]kitContract {
	dns := dnsRecordKitSpec()
	zone := firewallZoneKitSpec()
	return map[string]kitContract{
		// Catalog-compiled: no bootstrap, so the two SDK facts (go_name,
		// pointer-ness) can't be checked here -- the catalog carries neither,
		// verified instead by its policy digest.
		"dns_record": {artifact: "dns_record", typeName: dns.TypeName, wires: dns.WireNames()},
		// Bootstrap-compiled, so the SDK facts have a subject. Its identity
		// is a MANAGED field rather than provider_owned, which is the case
		// idWire exists for.
		"firewall_zone": {
			artifact:  "firewall_zone",
			typeName:  zone.TypeName,
			wires:     zone.WireNames(),
			idWire:    zone.IDWire,
			bootstrap: "go-unifi-v1.103.0-firewall-zone.json",
		},
	}
}

type bootstrapArtifact struct {
	Resource struct {
		Fields []struct {
			Name    string `json:"name"`
			Type    string `json:"type"`
			GoName  string `json:"go_name"`
			Pointer bool   `json:"pointer"`
		} `json:"fields"`
	} `json:"resource"`
}

// TestTheBootstrapCarriesTheTwoSDKFactsForEveryMappedField joins the two
// artifacts on structural_name: the bootstrap says what the SDK is, the
// mapping says how policy pairs it to Terraform, and copying the SDK facts
// into the mapping would just be a second home for them.
func TestTheBootstrapCarriesTheTwoSDKFactsForEveryMappedField(t *testing.T) {
	checked := 0
	for name, contract := range kitContracts() {
		if contract.bootstrap == "" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			checked++
			mapping := readMapping(t, contract.artifact)
			raw, err := os.ReadFile(filepath.Join("..", "provider-codegen", "bootstrap", contract.bootstrap))
			if err != nil {
				t.Fatalf("read bootstrap: %v", err)
			}
			var boot bootstrapArtifact
			if err := json.Unmarshal(raw, &boot); err != nil {
				t.Fatalf("parse bootstrap: %v", err)
			}
			sdk := map[string]struct {
				goName  string
				pointer bool
			}{}
			for _, f := range boot.Resource.Fields {
				sdk[f.Name] = struct {
					goName  string
					pointer bool
				}{f.GoName, f.Pointer}
			}
			var silent []string
			for _, field := range mapping.Fields {
				if field.Disposition != "managed" {
					continue
				}
				fact, present := sdk[field.StructuralName]
				if !present {
					t.Errorf("the mapping declares %q and the bootstrap has no such field, so the "+
						"two artifacts describe different structs", field.StructuralName)
					continue
				}
				if fact.goName == "" {
					silent = append(silent, field.StructuralName)
				}
			}
			if len(silent) > 0 {
				sort.Strings(silent)
				t.Errorf("%d mapped field(s) carry no go_name: %s\n\n"+
					"    A generator cannot write code that touches them. Regenerate the\n"+
					"    bootstraps; the fact is only produced by cmd/sdk-bootstrap.",
					len(silent), strings.Join(silent, ", "))
			}
		})
	}
	if checked == 0 {
		t.Fatal("no bootstrap-compiled surface is in the table, so this test asserted nothing")
	}
}

// TestEveryMappedFieldIsInTheDescriptor fails in both directions: a field
// mapped but absent from the descriptor, or declared by the descriptor but
// in no policy.
func TestEveryMappedFieldIsInTheDescriptor(t *testing.T) {
	for name, contract := range kitContracts() {
		t.Run(name, func(t *testing.T) {
			mapping := readMapping(t, contract.artifact)

			// Managed only: provider_owned entries (id, site, timeouts) are
			// reached through the spec's own accessors, not the field list,
			// so counting them would report permanent false absences.
			want := map[string]bool{}
			for _, field := range mapping.Fields {
				if field.Disposition != "managed" {
					continue
				}
				// The identity is not a mapped field, though which side of
				// the mapping it lands on varies by surface; the kit reaches
				// it through Backend.GetID rather than the field list.
				if contract.idWire != "" && field.StructuralName == contract.idWire {
					continue
				}
				want[field.StructuralName] = true
			}
			got := map[string]bool{}
			for _, wire := range contract.wires {
				got[wire] = true
			}

			var missing, extra []string
			for wire := range want {
				if !got[wire] {
					missing = append(missing, wire)
				}
			}
			for wire := range got {
				if !want[wire] {
					extra = append(extra, wire)
				}
			}
			sort.Strings(missing)
			sort.Strings(extra)

			if len(missing) > 0 {
				t.Errorf("%d field(s) the policy declares are mapped by nothing: %s\n\n"+
					"    The schema serves them and the resource never sends or reads them, so a\n"+
					"    configuration that sets one produces a permanent diff and no error.",
					len(missing), strings.Join(missing, ", "))
			}
			if len(extra) > 0 {
				t.Errorf("%d field(s) the descriptor maps are in no policy: %s\n\n"+
					"    This is the direction that reaches the controller: the provider would\n"+
					"    write an attribute nothing licensed it to write.",
					len(extra), strings.Join(extra, ", "))
			}
		})
	}
}

// TestTheTypeNameMatchesTheMapping exists because nothing else catches this
// on a push: the generated artifacts take their type name from a literal in
// the go:generate line, not from Metadata(), and the only path from
// Metadata() to a comparison needs a build+serve+dump check triggered by hand.
func TestTheTypeNameMatchesTheMapping(t *testing.T) {
	for name, contract := range kitContracts() {
		t.Run(name, func(t *testing.T) {
			mapping := readMapping(t, contract.artifact)
			want := strings.TrimPrefix(mapping.Resource, "unifi_")
			if want == mapping.Resource {
				t.Fatalf("the mapping's resource %q does not carry the provider prefix, so the "+
					"expectation below is derived from the wrong thing", mapping.Resource)
			}
			if contract.typeName != want {
				t.Errorf("the descriptor's TypeName is %q and the mapping says %q.\n\n"+
					"    Metadata() returns the provider prefix plus that string, and Terraform\n"+
					"    matches configuration blocks against it. A mismatch does not error --\n"+
					"    the resource is absent and a configuration naming it finds nothing.",
					contract.typeName, want)
			}
		})
	}
}
