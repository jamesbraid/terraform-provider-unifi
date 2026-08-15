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

// TestResolvesAModelDeclaredInAnotherFile is the case the per-file version of
// this got wrong, and it got it wrong confidently: it reported that the network
// data source had no field for boot or wins, about code that assigns to exactly
// those fields and therefore compiles.
//
// dhcpServerModel is declared in network_resource.go and used by
// network_data_source.go. Any lookup scoped to the file that SERVES a schema
// reports every shared model as missing, and shared models are the norm here
// rather than the exception.
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

// TestReadsTheAttributeNameOutOfATagWithOptions covers the second bug: a tag
// written `tfsdk:"ip_address_pool,omitempty"` names the attribute
// ip_address_pool, and reading the whole tag value meant natOutboundIPAddresses
// never matched anything.
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

// TestIndexesAShapeDeclaredAsAnAttributeTypeMap covers the third: six shapes in
// this codebase are a map[string]attr.Type returned from a function, with no
// struct at all. A referee that only knows tagged structs calls all six missing.
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

// TestAModelsOwnAttributeTypesIsNotASecondCandidate is the regression test for
// the bug that made this whole referee unable to fail.
//
// Thirty-eight models restate their member set in an AttributeTypes() method.
// While those restatements were indexed as independent shapes, every one of
// those models had a twin: break the struct and the method still satisfies the
// schema, so the referee resolved its one model and stayed green. That was
// measured -- renaming dhcpServerModel's wins tag left the entire suite
// passing, which is how the hole was found rather than argued.
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

// TestDisagreementBetweenAStructAndItsOwnAttributeTypes is the check that
// replaced the masking. The framework converts values through both the tags
// and the method, so the two declaring different member sets is a defect on
// its own -- and it is the half that catches a broken struct now that the
// method can no longer stand in for it.
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

// TestResolveRejectsASubsetOrSuperset is the half that makes the check mean
// something. A model carrying MORE than the attribute set is not a model for
// that attribute, and neither is one carrying less.
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

// TestNearestNamesTheDifference is why a failure is actionable. "No model
// found" sends the reader to redo the search by hand.
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
