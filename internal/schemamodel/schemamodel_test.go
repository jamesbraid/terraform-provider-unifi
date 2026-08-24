package schemamodel

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func write(t *testing.T, dir, name, source string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestResolvesAModelDeclaredInAnotherFile: dhcpServerModel is declared in
// network_resource.go but used from network_data_source.go, so resolution
// must not be scoped to the file that serves the schema.
func TestResolvesAModelDeclaredInAnotherFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "network_resource.go", `package unifi

type dhcpServerModel struct {
	Boot types.Object `+"`tfsdk:\"boot\"`"+`
	Wins types.Object `+"`tfsdk:\"wins\"`"+`
}
`)
	write(t, dir, "network_data_source.go", `package unifi

type networkDataSourceModel struct {
	DhcpServer types.Object `+"`tfsdk:\"dhcp_server\"`"+`
}
`)
	index, err := IndexModels(dir)
	if err != nil {
		t.Fatal(err)
	}
	matches := index.Resolve([]string{"boot", "wins"})
	if len(matches) != 1 || matches[0].Name != "dhcpServerModel" {
		t.Fatalf("resolved %+v, want the shared dhcpServerModel from the other file", matches)
	}
	if matches[0].File != "network_resource.go" {
		t.Errorf("model file = %q, want the file that DECLARES it", matches[0].File)
	}
}

// TestReadsTheAttributeNameOutOfATagWithOptions: a tag written
// `tfsdk:"ip_address_pool,omitempty"` names the attribute ip_address_pool,
// not the raw tag value.
func TestReadsTheAttributeNameOutOfATagWithOptions(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "network_resource.go", `package unifi

type natOutboundIPAddressesModel struct {
	IPAddress     types.String `+"`tfsdk:\"ip_address,omitempty\"`"+`
	IPAddressPool types.List   `+"`tfsdk:\"ip_address_pool,omitempty\"`"+`
	Mode          types.String `+"`tfsdk:\"mode,omitempty\"`"+`
}
`)
	index, err := IndexModels(dir)
	if err != nil {
		t.Fatal(err)
	}
	if matches := index.Resolve([]string{"ip_address", "ip_address_pool", "mode"}); len(matches) != 1 {
		t.Fatalf("resolved %d models, want 1; tag options are not part of the attribute name", len(matches))
	}
}

// TestIndexesAShapeDeclaredAsAnAttributeTypeMap: a shape can be declared as
// a map[string]attr.Type returned from a function, with no backing struct.
func TestIndexesAShapeDeclaredAsAnAttributeTypeMap(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "power_supervisor_resource.go", `package unifi

func powerSourceAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"power_source_index": types.Int64Type,
		"power_source_mac":   types.StringType,
	}
}
`)
	index, err := IndexModels(dir)
	if err != nil {
		t.Fatal(err)
	}
	matches := index.Resolve([]string{"power_source_index", "power_source_mac"})
	if len(matches) != 1 || matches[0].Name != "powerSourceAttrTypes()" {
		t.Fatalf("resolved %+v, want the attr.Type map shape", matches)
	}
}

// TestAModelsOwnAttributeTypesIsNotASecondCandidate: a struct's own
// AttributeTypes() method must not be indexed as an independent, rival
// candidate for its tag set.
func TestAModelsOwnAttributeTypesIsNotASecondCandidate(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "network_resource.go", `package unifi

type dhcpServerModel struct {
	Boot types.Object `+"`tfsdk:\"boot\"`"+`
	Wins types.Object `+"`tfsdk:\"wins\"`"+`
}

func (m dhcpServerModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"boot": types.ObjectType{},
		"wins": types.ObjectType{},
	}
}
`)
	index, err := IndexModels(dir)
	if err != nil {
		t.Fatal(err)
	}
	matches := index.Resolve([]string{"boot", "wins"})
	if len(matches) != 1 {
		t.Fatalf("resolved %d shapes for one struct-plus-its-own-method; "+
			"a restatement is not an alternative to the thing it restates", len(matches))
	}
	if matches[0].Name != "dhcpServerModel" {
		t.Errorf("resolved %q, want the struct itself", matches[0].Name)
	}
	if len(index.Disagreements()) != 0 {
		t.Errorf("a model that agrees with its own AttributeTypes() was reported as disagreeing")
	}
}

// TestDisagreementBetweenAStructAndItsOwnAttributeTypes: a struct and its own
// AttributeTypes() method declaring different member sets is itself a defect.
func TestDisagreementBetweenAStructAndItsOwnAttributeTypes(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "network_resource.go", `package unifi

type dhcpServerModel struct {
	Boot types.Object `+"`tfsdk:\"boot\"`"+`
	Wins types.Object `+"`tfsdk:\"wins_renamed\"`"+`
}

func (m dhcpServerModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"boot": types.ObjectType{},
		"wins": types.ObjectType{},
	}
}
`)
	index, err := IndexModels(dir)
	if err != nil {
		t.Fatal(err)
	}
	disagreements := index.Disagreements()
	if len(disagreements) != 1 {
		t.Fatalf("found %d disagreements, want 1", len(disagreements))
	}
	if got := disagreements[0].RestatedTags(); !reflect.DeepEqual(got, []string{"boot", "wins"}) {
		t.Errorf("restated tags = %v, want [boot wins]", got)
	}
	if got := disagreements[0].Tags(); !reflect.DeepEqual(got, []string{"boot", "wins_renamed"}) {
		t.Errorf("struct tags = %v, want [boot wins_renamed]", got)
	}
}

// TestAModelReachedOnlyFromUpgradeStateIsNotACandidate: reachability is
// transitive on purpose here — the V0 model is named only in a helper
// UpgradeState calls, not in UpgradeState itself.
func TestAModelReachedOnlyFromUpgradeStateIsNotACandidate(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "firewall_policy_resource.go", `package unifi

type endpointModel struct {
	ZoneID types.String `+"`tfsdk:\"zone_id\"`"+`
}

type endpointModelV0 struct {
	ZoneID types.String `+"`tfsdk:\"zone_id\"`"+`
}

func (r *firewallPolicyResource) UpgradeState(ctx context.Context) {
	upgradeEndpointV0(ctx)
}

func upgradeEndpointV0(ctx context.Context) {
	var v0 endpointModelV0
	_ = v0
}

func (r *firewallPolicyResource) Read(ctx context.Context) {
	var live endpointModel
	_ = live
}
`)
	index, err := IndexModels(dir)
	if err != nil {
		t.Fatal(err)
	}
	matches := index.Resolve([]string{"zone_id"})
	if len(matches) != 1 {
		names := make([]string, 0, len(matches))
		for _, model := range matches {
			names = append(names, model.Name)
		}
		sort.Strings(names)
		t.Fatalf("resolved %v; a model reachable only from UpgradeState is not a candidate", names)
	}
	if matches[0].Name != "endpointModel" {
		t.Errorf("resolved %q, want the live model", matches[0].Name)
	}
	// Nearest must apply the same exclusion as Resolve.
	if near, missing, extra := index.Nearest([]string{"zone_id", "absent"}); near.Name == "endpointModelV0" {
		t.Errorf("Nearest returned the upgrade-only model (missing %v, extra %v)", missing, extra)
	}
}

// TestAModelUsedLiveAndInAnUpgradeStaysACandidate: a shape used by both a
// live path and an upgrader is a live shape, so the exclusion must not drop it.
func TestAModelUsedLiveAndInAnUpgradeStaysACandidate(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.go", `package unifi

type sharedModel struct {
	ZoneID types.String `+"`tfsdk:\"zone_id\"`"+`
}

func (r *res) UpgradeState(ctx context.Context) {
	var m sharedModel
	_ = m
}

func (r *res) Read(ctx context.Context) {
	var m sharedModel
	_ = m
}
`)
	index, err := IndexModels(dir)
	if err != nil {
		t.Fatal(err)
	}
	if matches := index.Resolve([]string{"zone_id"}); len(matches) != 1 {
		t.Fatalf("resolved %d models; a shape used on a live path is live even if an upgrader also uses it",
			len(matches))
	}
}

// TestResolveRejectsASubsetOrSuperset checks that only an exact tag-set match
// resolves, not a subset or a superset.
func TestResolveRejectsASubsetOrSuperset(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.go", `package unifi

type wider struct {
	A types.String `+"`tfsdk:\"a\"`"+`
	B types.String `+"`tfsdk:\"b\"`"+`
	C types.String `+"`tfsdk:\"c\"`"+`
}
`)
	index, err := IndexModels(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, attributes := range [][]string{{"a", "b"}, {"a", "b", "c", "d"}} {
		if matches := index.Resolve(attributes); len(matches) != 0 {
			t.Errorf("Resolve(%v) matched %d models; only an exact member set identifies one",
				attributes, len(matches))
		}
	}
}

// TestNearestNamesTheDifference checks that a near-miss reports which struct
// and how it differs, not just that nothing matched.
func TestNearestNamesTheDifference(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.go", `package unifi

type closeEnough struct {
	A     types.String `+"`tfsdk:\"a\"`"+`
	Extra types.String `+"`tfsdk:\"extra\"`"+`
}
`)
	index, err := IndexModels(dir)
	if err != nil {
		t.Fatal(err)
	}
	model, missing, extra := index.Nearest([]string{"a", "b"})
	if model.Name != "closeEnough" {
		t.Fatalf("nearest = %q, want closeEnough", model.Name)
	}
	if !reflect.DeepEqual(missing, []string{"b"}) {
		t.Errorf("missing = %v, want [b]", missing)
	}
	if !reflect.DeepEqual(extra, []string{"extra"}) {
		t.Errorf("extra = %v, want [extra]", extra)
	}
}

// TestIgnoresTestFileModels keeps a fixture struct in a _test.go file from
// becoming a second candidate for a real shape.
func TestIgnoresTestFileModels(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.go", `package unifi

type real struct {
	A types.String `+"`tfsdk:\"a\"`"+`
}
`)
	write(t, dir, "a_test.go", `package unifi

type fixture struct {
	A types.String `+"`tfsdk:\"a\"`"+`
}
`)
	index, err := IndexModels(dir)
	if err != nil {
		t.Fatal(err)
	}
	matches := index.Resolve([]string{"a"})
	if len(matches) != 1 || matches[0].Name != "real" {
		names := make([]string, 0, len(matches))
		for _, model := range matches {
			names = append(names, model.Name)
		}
		sort.Strings(names)
		t.Fatalf("resolved %v, want only the non-test model", names)
	}
}
