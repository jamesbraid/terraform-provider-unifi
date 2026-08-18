package unifi

// The contract between the compiler's mapping artifact and the descriptor a
// resource is generated from.
//
// WHY THIS IS THE CHECK THAT DECIDES WHAT ELSE CAN BE DELETED. The generated
// CRUD's correctness rests on the descriptor naming the same fields the policy
// does, and nothing compares those two today: a field can be added to a policy,
// appear in the schema, and be mapped by nobody. The resource would serve an
// attribute it never sends and never reads back, which is a silent permanent
// diff rather than an error.
//
// THE ORACLE IS OUTSIDE THE THING IT CHECKS. It reads
// provider-codegen/generated/<name>.mapping.json, which the compiler emits from
// the policy and the SDK, and compares it against the descriptor as the running
// provider holds it. A check that asked the generator what it generated would
// agree with itself by construction.
//
// It also runs on a push, which the schema comparison does not: fast-loop is the
// only workflow with an automatic trigger and what it runs is go test.

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

// kitContracts is the table each generated resource joins. One line per surface.
//
// The wire names come from the SPEC rather than from the generated source,
// because the spec is what the provider actually runs -- a descriptor that
// compiles and is never referenced would pass a source-level check.
func kitContracts() map[string]struct {
	artifact string
	typeName string
	wires    []string
} {
	dns := dnsRecordKitSpec()
	return map[string]struct {
		artifact string
		typeName string
		wires    []string
	}{
		"dns_record": {artifact: "dns_record", typeName: dns.TypeName, wires: dns.WireNames()},
	}
}

// TestEveryMappedFieldIsInTheDescriptor is the check that decides what evidence
// can delete, and it fails in BOTH directions on purpose.
//
// A field in the mapping and absent from the descriptor is an attribute the
// schema serves and the resource never touches. A field in the descriptor and
// absent from the mapping is a value the provider sends that no policy licensed,
// which is the direction that reaches the controller.
func TestEveryMappedFieldIsInTheDescriptor(t *testing.T) {
	for name, contract := range kitContracts() {
		t.Run(name, func(t *testing.T) {
			mapping := readMapping(t, contract.artifact)

			// MANAGED ONLY. The mapping's provider_owned entries -- id, site,
			// timeouts -- are reached through the spec's own accessors rather
			// than through the field list, so counting them here would report
			// three permanent absences on every resource.
			want := map[string]bool{}
			for _, field := range mapping.Fields {
				if field.Disposition == "managed" {
					want[field.StructuralName] = true
				}
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

// TestTheTypeNameMatchesTheMapping covers the mistake nothing else can see.
//
// Metadata() returns the provider prefix plus the spec's TypeName, and that
// string is what Terraform matches a configuration block against. Get it wrong
// and the resource is simply absent -- no error, no diagnostic, a configuration
// that says "unifi_dns_record" finds nothing.
//
// evidence traced what would catch it today and the answer is nothing on a push:
// the generated artifacts take their type names from go:generate directive
// literals rather than from Metadata(), and the only path from Metadata() to a
// comparison is build, serve, dump and compare -- which is catalog-build-schema,
// which is event: manual.
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
