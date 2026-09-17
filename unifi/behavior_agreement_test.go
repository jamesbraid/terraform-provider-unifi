package unifi

// The SDK's behaviour artifact and the served schemas must agree: for every
// served resource whose SDK type has a measured writes entry, each
// required-on-create wire that is exposed as a managed attribute must be
// Required in the schema snapshot. The rule behind it is the same one the
// compiler enforces at generation time -- behaviour modeling may only be
// derived from the artifact -- and this closes the loop from the other end,
// over what the provider actually serves. Checked in both artifacts' own
// terms (behavior.json, resource-decisions.json, the mapping reports and the
// schema snapshot, all committed) rather than by asking the compiler, which
// would agree with itself by construction.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// behaviorArtifactDocument is the wrapper cmd/sdk-bootstrap commits to
// provider-codegen/bootstrap/behavior.json: the SDK's measured behaviour,
// stamped with the module it was resolved from.
type behaviorArtifactDocument struct {
	FormatVersion int `json:"format_version"`
	Source        struct {
		Repository string `json:"repository"`
		Version    string `json:"version"`
		Commit     string `json:"commit"`
	} `json:"source"`
	Behavior struct {
		ControllerVersion string `json:"controller_version"`
		Writes            map[string]struct {
			RequiredOnCreate []string `json:"required_on_create"`
		} `json:"writes"`
	} `json:"behavior"`
}

func loadBehaviorArtifact(t *testing.T) behaviorArtifactDocument {
	t.Helper()
	path := filepath.Join("..", "provider-codegen", "bootstrap", "behavior.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the behaviour artifact is this check's source of truth and it is unreadable: %v\n"+
			"    Regenerate it: cd provider-codegen && go generate ./...", err)
	}
	var document behaviorArtifactDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if document.FormatVersion != 1 {
		t.Fatalf("%s has format_version %d, want 1", path, document.FormatVersion)
	}
	// A renamed or dropped writes member would decode to an empty map and
	// skip this check forever without anyone deciding that; only a writes
	// member that is present and empty is a legitimate no-facts state.
	var shape struct {
		Behavior map[string]json.RawMessage `json:"behavior"`
	}
	if err := json.Unmarshal(raw, &shape); err != nil {
		t.Fatal(err)
	}
	if _, present := shape.Behavior["writes"]; !present {
		t.Fatalf("%s carries no writes member at all; the artifact's shape moved under this "+
			"check, which can no longer see write behaviour", path)
	}
	return document
}

func loadCommittedSchemaSnapshot(t *testing.T) providerSchemaSnapshot {
	t.Helper()
	raw, err := os.ReadFile(schemaSnapshotPath)
	if err != nil {
		t.Fatalf("the schema snapshot is this check's other source of truth and it is unreadable: %v\n"+
			"    Run UPDATE_SCHEMA_SNAPSHOT=1 go test ./unifi/ -run TestProviderSchemaSnapshot", err)
	}
	var snapshot providerSchemaSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatalf("parse %s: %v", schemaSnapshotPath, err)
	}
	if len(snapshot.Resources) == 0 {
		t.Fatalf("%s records no resources, so every lookup below would miss vacuously", schemaSnapshotPath)
	}
	return snapshot
}

// mappedWire is how one observed wire is served, read from the surface's
// committed mapping report.
type mappedWire struct {
	terraformName string
	disposition   string
	// providerFilled marks a required-on-create wire the compiler was told
	// the provider supplies (policy's provider_filled_wires): required on the
	// wire, but filled by the provider, so the served attribute is
	// deliberately not Required. Such a wire is not a disagreement.
	providerFilled bool
}

// requiredOnCreateFindings compares one surface's measured required wires
// against how they are served. A wire with no mapping row is omitted or
// otherwise unexposed -- that is a policy decision, not a finding; a
// non-managed row is provider-supplied at create, not practitioner-authored,
// so requiredness does not apply to it either. What must hold is: exposed as
// a managed attribute implies Required in the served schema.
func requiredOnCreateFindings(
	surface string,
	requiredWires []string,
	mapped map[string]mappedWire,
	attributes map[string]schemaSnapshotFact,
) []string {
	var findings []string
	for _, wire := range requiredWires {
		row, exposed := mapped[wire]
		if !exposed || row.disposition != "managed" {
			continue
		}
		// A managed wire the provider fills is served user-optional on
		// purpose; the mapping report records that decision, so it is not a
		// disagreement here either. Only wires the policy did not opt out
		// still have to be Required.
		if row.providerFilled {
			continue
		}
		fact, present := attributes[row.terraformName]
		if !present {
			findings = append(findings, fmt.Sprintf(
				"%s: required-on-create wire %q maps to attribute %q, which the served schema "+
					"snapshot does not carry", surface, wire, row.terraformName))
			continue
		}
		if !fact.Required {
			findings = append(findings, fmt.Sprintf(
				"%s: wire %q is required on create in the behaviour artifact, but attribute %q "+
					"is not Required in the served schema", surface, wire, row.terraformName))
		}
	}
	return findings
}

// TestEveryServedRequiredOnCreateWireIsRequiredInTheSchema walks every
// kit-served surface (resource-decisions.json, itself cross-checked against
// the kit in both directions by descriptor_policy_test.go), matches its
// sdk_type against the artifact's writes, and asserts agreement per wire.
// When the artifact's writes cover no served surface -- true today, since
// Nat and ContentFiltering are not served -- it says so as a skip rather
// than passing as if it had checked something.
func TestEveryServedRequiredOnCreateWireIsRequiredInTheSchema(t *testing.T) {
	artifact := loadBehaviorArtifact(t)
	policy := loadDescriptorPolicy(t)
	snapshot := loadCommittedSchemaSnapshot(t)

	surfaces := make([]string, 0, len(policy.Surfaces))
	for surface := range policy.Surfaces {
		surfaces = append(surfaces, surface)
	}
	sort.Strings(surfaces)

	var covered []string
	for _, surface := range surfaces {
		writes, measured := artifact.Behavior.Writes[policy.Surfaces[surface].SDKType]
		if !measured {
			continue
		}
		covered = append(covered, surface)
		mapping := readMapping(t, strings.TrimPrefix(surface, "unifi_"))
		mapped := make(map[string]mappedWire, len(mapping.Fields))
		for _, row := range mapping.Fields {
			mapped[row.StructuralName] = mappedWire{
				terraformName:  row.TerraformName,
				disposition:    row.Disposition,
				providerFilled: row.ProviderFillsRequiredWire,
			}
		}
		served, servedOK := snapshot.Resources[surface]
		if !servedOK {
			t.Errorf("%s has a writes entry (%s) and a kit policy entry but no surface in the "+
				"schema snapshot; the check cannot see what it serves",
				surface, policy.Surfaces[surface].SDKType)
			continue
		}
		for _, finding := range requiredOnCreateFindings(
			surface, writes.RequiredOnCreate, mapped, served.Attributes) {
			t.Error(finding)
		}
	}

	if len(covered) == 0 {
		writeTypes := make([]string, 0, len(artifact.Behavior.Writes))
		for name := range artifact.Behavior.Writes {
			writeTypes = append(writeTypes, name)
		}
		sort.Strings(writeTypes)
		t.Skipf("the behaviour artifact covers no served surface: writes names %v (controller %s) "+
			"and no served resource's sdk_type matches, so this check asserted nothing",
			writeTypes, artifact.Behavior.ControllerVersion)
	}
	t.Logf("checked required-on-create agreement for %v", covered)
}

// TestRequiredOnCreateFindingsDetectEveryDisagreement is the check's
// positive control: with no served surface covered today the walk above
// skips, so the comparison itself has to be shown going red -- and staying
// quiet exactly where policy is allowed to decide.
func TestRequiredOnCreateFindingsDetectEveryDisagreement(t *testing.T) {
	mapped := map[string]mappedWire{
		"protocol":       {terraformName: "protocol", disposition: "managed"},
		"source_filter":  {terraformName: "source_filter", disposition: "managed"},
		"vanished":       {terraformName: "vanished", disposition: "managed"},
		"supplied":       {terraformName: "supplied", disposition: "computed"},
		"provider_fills": {terraformName: "provider_fills", disposition: "managed", providerFilled: true},
	}
	attributes := map[string]schemaSnapshotFact{
		"protocol":       {Kind: "attribute", Optional: true, Computed: true},
		"source_filter":  {Kind: "attribute", Required: true},
		"provider_fills": {Kind: "attribute", Optional: true, Computed: true},
	}

	findings := requiredOnCreateFindings("unifi_probe",
		[]string{"protocol", "source_filter", "vanished", "supplied", "provider_fills", "omitted_wire"},
		mapped, attributes)

	// protocol is a genuine required-but-optional disagreement and must still
	// be caught; provider_fills is served optional on the same terms but is
	// opted out, so it must stay silent -- the opt-out suppresses only where
	// it is declared, and does not swallow protocol.
	if len(findings) != 2 {
		t.Fatalf("findings = %v, want exactly 2: a non-Required managed wire and a mapped "+
			"attribute the snapshot does not carry; the agreeing, provider-supplied, "+
			"provider-filled and unexposed wires must stay silent", findings)
	}
	for _, want := range []string{`"protocol"`, `"vanished"`} {
		var named bool
		for _, finding := range findings {
			if strings.Contains(finding, want) && strings.Contains(finding, "unifi_probe") {
				named = true
			}
		}
		if !named {
			t.Errorf("no finding names both unifi_probe and %s; a hit that does not say where "+
				"it is sends the reader grepping", want)
		}
	}
}
