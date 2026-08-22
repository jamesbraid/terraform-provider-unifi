package unifi

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/schemamodel"
)

// nestedAttribute is one object-valued attribute in a served schema: the path a
// practitioner writes, the members it declares, and whether the schema binds it
// to a custom type.
type nestedAttribute struct {
	Path    string
	Members []string
	// CustomType is the type bound to the attribute itself.
	CustomType string
	// ElementCustomType is the type bound to a list or set attribute's ELEMENT
	// object, which is a separate binding site and the one a walk of attributes
	// alone does not see. Fifteen of the fifty-two live here, so reading only
	// CustomType found thirty-seven of them -- the same undercount, from the
	// same blind spot, that four earlier textual scans produced.
	ElementCustomType string
}

// TestServedSchemaAgreesWithItsRuntimeModel is the referee that did not exist.
//
// Every other referee here compares this provider against the RELEASED one.
// This compares the two halves of THIS one: the schema each surface serves
// against the model its code carries values in. Fifty-four controller
// regressions came through that gap, because an internal inconsistency is not a
// divergence from the old provider and no cross-version comparison can see it.
//
// It is also not bounded by test coverage, which is the point. The controller
// run found the defect on nine surfaces and could not find it on
// unifi_firewall_policy, which had no managed acceptance test and sixteen uses
// on the fleet.
//
// THAT SENTENCE WAS TRUE FOR FORTY-ONE MINUTES. It was written at 13:58 on
// 2026-08-15 in 76969e84, on a branch where unifi_firewall_policy genuinely had
// no managed acceptance test. 4ec8ab23 had added one at 12:22 the same day on a
// DIVERGENT line, and 84189fe7 merged the two at 14:39 -- so the claim became
// false without anyone editing this file or the tests it describes. Since then
// three managed acceptance tests exercise the surface:
// TestAccFirewallPolicyFramework_basic, TestAccFirewallPolicyScheduleIsManageable
// and the match-flags regression.
//
// It is kept in the past tense rather than deleted because it records WHY the
// controller run missed this surface, which is still the reason this check
// exists. Two people later read it as a present-tense fact and planned work
// around it.
//
// The general form, and it is worth more than the correction: A MERGE CAN
// FALSIFY A COMMENT IN A FILE IT DOES NOT TOUCH. Nothing diffs, nothing fails,
// and the sentence goes on reading as current. Prose that describes the tree
// outside its own file has no guard, so date it and name the commit.
//
// TWO CHECKS, and the second runs in both directions:
//
//	ATTRIBUTE SET  every object-valued attribute must resolve to a model whose
//	               tfsdk tags are exactly its members. No model, or several, is
//	               a finding and the failure names the nearest struct and the
//	               difference.
//
//	TYPE IDENTITY  a schema CustomType X requires the model field to be XValue,
//	               AND a model field typed XValue requires the schema to declare
//	               CustomType X. The reverse direction is why the generated
//	               <X>Value declarations can be left in place after
//	               nested-custom-type-strip: writing one into a model is caught
//	               here, which deleting them would only have prevented by one
//	               route.
func TestServedSchemaAgreesWithItsRuntimeModel(t *testing.T) {
	ctx := context.Background()
	index, err := schemamodel.IndexModels(".", "models")
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Models) == 0 {
		t.Fatal("no tfsdk-tagged models found, so every check below would be vacuous")
	}

	surfaces := servedNestedAttributes(ctx, t)
	if len(surfaces) == 0 {
		t.Fatal("no object-valued attributes found in any served schema, so this proves nothing")
	}

	ambiguous := map[string]string{}
	checked := 0
	for _, nested := range surfaces {
		checked++
		// ANY model with exactly this member set satisfies the shape. Several
		// is NOT a finding, and asserting otherwise was the first version of
		// this test: dhcpGuardingModel and dhcpRelayModel both carry {enabled,
		// servers}, five identity models all carry {id, name}, and none of that
		// is a defect. A referee whose loudest signal is benign is one people
		// learn to skip, which is worse than not having it.
		matches := index.Resolve(nested.Members)
		if len(matches) > 1 {
			names := make([]string, 0, len(matches))
			for _, model := range matches {
				names = append(names, model.Name)
			}
			sort.Strings(names)
			ambiguous[nested.Path] = strings.Join(names, " ")
		}
		if len(matches) == 0 {
			near, missing, extra := index.Nearest(nested.Members)
			if near.Name == "" {
				t.Errorf("%s: the schema serves %d members and no runtime model declares any of them",
					nested.Path, len(nested.Members))
				continue
			}
			t.Errorf("%s: no runtime model carries exactly these members; nearest is %s in %s, "+
				"which is missing %v and additionally declares %v",
				nested.Path, near.Name, near.File, missing, extra)
		}
	}

	// The generated type set, used by TYPE IDENTITY below.
	generated, err := schemamodel.GeneratedTypes("../internal/generated")
	if err != nil {
		t.Fatal(err)
	}
	if len(generated) == 0 {
		t.Fatal("no types found in the generated tree, so the check below would be vacuous")
	}

	// TYPE IDENTITY, and until this commit it was a header comment describing a
	// check that did not exist.
	//
	// The file's own line above says of the prohibition that CustomType "was
	// captured by the walk and never read" -- and that was true of the whole
	// file: nested.CustomType was read in exactly one place, inside the
	// prohibition, and compared against nothing. What stood here instead were
	// two PROHIBITIONS, one forbidding the schema to bind a generated type and
	// one forbidding a model to declare one. Together they pinned both ends to a
	// single permitted value, which is identity only in the degenerate sense.
	// Deleting either left nothing comparing the ends at all.
	//
	// A CHECK BUILT FOR A DEFECT, WHICH DOES NOT FIRE ON THAT DEFECT, IS WORTH
	// NOTHING -- this file's lesson, applied to itself one layer out.
	//
	// THE RULE WAS DESCRIBED HERE BEFORE IT WAS IMPLEMENTED, and the gap
	// survived a suite that was mutation-proven throughout. That is not an
	// oversight in the mutation testing: it exercises the checks you HAVE
	// against the code you have, so it is structurally blind to a check that
	// exists only in prose. Every mutation it ran tested something real.
	//
	// So a header is not coverage, and this file is now its own worked example
	// of the difference. Read the code -- including this comment's own claims.
	//
	// AND IT IS THE BACKSTOP FOR cmd/nested-custom-type-strip, which is a better
	// justification than the one it was written under.
	//
	// tfplugingen-framework binds a generated type to every nested object; the
	// strip removes all 52 immediately after generation, so in normal operation
	// nothing ever binds one and this check has nothing to compare. That is not
	// a check guarding an unreachable case -- it is what makes REMOVING THE
	// STRIP LOUD. Drop that go:generate line, reorder it, or let it fail
	// silently, and 52 bindings reappear against plain models; the framework
	// then refuses the mismatch at apply time, on every affected attribute, in
	// front of a practitioner. This fails first, in the suite, naming them.
	//
	// WHAT IT COMPARES. A schema binding CustomType X requires some model to
	// declare that attribute as XValue, and a model field declared XValue
	// requires some served attribute of that name to bind X.
	//
	// "SOME" RATHER THAN "THE", AND THAT BOUND IS REAL. Resolving which model
	// serves a given attribute needs dataflow through the ObjectValueFrom call
	// sites, for the reason the ambiguity note below gives. So this cannot catch
	// a surface binding X correctly while a DIFFERENT surface's identically
	// named attribute is wrong. It does catch every case where the pairing
	// exists nowhere, which is what a migration gets wrong.
	valueOf := func(customType string) string {
		return strings.TrimSuffix(customType, "Type") + "Value"
	}
	modelledAs := map[string]map[string]bool{} // tfsdk tag -> Go types declaring it
	for _, model := range index.Models {
		for tag, goType := range model.Fields {
			bare := strings.TrimPrefix(goType, "*")
			if modelledAs[tag] == nil {
				modelledAs[tag] = map[string]bool{}
			}
			modelledAs[tag][bare] = true
		}
	}

	boundSomewhere := map[string]bool{} // XValue name -> a schema binds its X
	for _, nested := range surfaces {
		name := nested.Path[strings.LastIndex(nested.Path, ".")+1:]
		for where, bound := range map[string]string{
			"":         nested.CustomType,
			" element": nested.ElementCustomType,
		} {
			if bound == "" {
				continue
			}
			if _, isGenerated := generated[bound]; !isGenerated {
				continue
			}
			want := valueOf(bound)
			boundSomewhere[want] = true
			if !modelledAs[name][want] {
				t.Errorf("%s%s binds custom type %s, but no runtime model declares %q "+
					"as %s -- the framework cannot convert the value and every apply "+
					"touching the attribute fails. Type the model field, or drop the "+
					"binding.", nested.Path, where, bound, name, want)
			}
		}
	}

	// The reverse. A model typed against a generated value type with no schema
	// asking for it is the same mismatch arriving from the other end, and it is
	// why the generated <X>Value declarations can be left in place: writing one
	// into a model is caught here.
	for _, model := range index.Models {
		for tag, goType := range model.Fields {
			bare := strings.TrimPrefix(goType, "*")
			if !strings.HasSuffix(bare, "Value") {
				continue
			}
			if _, isGenerated := generated[strings.TrimSuffix(bare, "Value")+"Type"]; !isGenerated {
				continue
			}
			if !boundSomewhere[bare] {
				t.Errorf("%s.%s in %s is declared %s, but no served schema binds %s for "+
					"an attribute named %q -- nothing asks for this type and the framework "+
					"has no converter for it.",
					model.Name, tag, model.File, bare,
					strings.TrimSuffix(bare, "Value")+"Type", tag)
			}
		}
	}

	// Direction three: a model that restates its own shape in an
	// AttributeTypes() method must restate it correctly. This check is here
	// because its ABSENCE was what made the other two unable to fail -- while
	// the restatements were indexed as independent shapes, every one of the
	// thirty-eight models carrying one had a twin that would satisfy the schema
	// after the struct itself had been broken.
	for _, model := range index.Disagreements() {
		t.Errorf("%s in %s declares tfsdk tags %v but its own AttributeTypes() declares %v; "+
			"the framework converts through both and they must be the same set",
			model.Name, model.File, model.Tags(), model.RestatedTags())
	}

	// WHERE THE ATTRIBUTE-SET CHECK CANNOT FAIL, NAMED.
	//
	// Several models carrying a member set is not a defect -- three unrelated
	// models legitimately declare {enabled, servers}, and five identity models
	// declare {id, name}. But it does mean the check above cannot fail for that
	// attribute: break the model that actually serves it and a sibling still
	// matches. Resolving WHICH model serves an attribute needs dataflow through
	// the ObjectValueFrom call sites, because a types.Object field does not
	// name its element model; that is not built, so these are declared instead.
	//
	// Compared as a SET, both directions. A new ambiguity must be added here
	// deliberately, and one that disappears must be removed -- a count would
	// let one silently replace another.
	//
	// unifi_firewall_policy.source and .destination are the ones that matter:
	// that surface has no managed acceptance test and sixteen uses on the
	// fleet, and its second candidate is a state-upgrader model for schema
	// version 0, which is not a rival the runtime can actually use. Closing
	// those two is the highest-value piece of work left here.
	declaredAmbiguous := map[string]string{
		"unifi_network.dhcp_guarding":                   "dhcpGuardingModel dhcpRelayModel vpnServerDNSModel",
		"unifi_network.dhcp_relay":                      "dhcpGuardingModel dhcpRelayModel vpnServerDNSModel",
		"unifi_setting.ips.suppression_alerts.tracking": "settingIpsTrackingModel settingIpsWhitelistModel",
		"unifi_setting.ips.suppression_whitelist":       "settingIpsTrackingModel settingIpsWhitelistModel",
		"unifi_vpn_server.dns":                          "dhcpGuardingModel dhcpRelayModel vpnServerDNSModel",
		"unifi_traffic_route.source.networks":           "sourceNetworkModel vpnServerIdentityModel",
		"data.unifi_network.dhcp_guarding":              "dhcpGuardingModel dhcpRelayModel vpnServerDNSModel",
		"data.unifi_network.dhcp_relay":                 "dhcpGuardingModel dhcpRelayModel vpnServerDNSModel",
	}
	for path, candidates := range ambiguous {
		declared, ok := declaredAmbiguous[path]
		switch {
		case !ok:
			t.Errorf("%s now resolves to several models (%s) and the check above can no longer fail "+
				"for it; either give it a distinct member set or declare it here with the others",
				path, candidates)
		case declared != candidates:
			t.Errorf("%s resolves to %s, declared as %s; the set of models sharing this shape moved",
				path, candidates, declared)
		}
	}
	for path := range declaredAmbiguous {
		if _, ok := ambiguous[path]; !ok {
			t.Errorf("%s is declared ambiguous but now resolves to one model; delete it from "+
				"declaredAmbiguous so the list keeps meaning what it says", path)
		}
	}

	t.Logf("checked %d object-valued attributes across the served schemas against %d runtime models; "+
		"%d of them resolve to several models and cannot fail this check",
		checked, len(index.Models), len(ambiguous))
}

// servedNestedAttributes walks every registered surface's schema to any depth.
func servedNestedAttributes(ctx context.Context, t *testing.T) []nestedAttribute {
	t.Helper()
	found := make([]nestedAttribute, 0)
	provider := &unifiProvider{}

	for _, newResource := range provider.Resources(ctx) {
		res := newResource()
		var meta resource.MetadataResponse
		res.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "unifi"}, &meta)
		var got resource.SchemaResponse
		res.Schema(ctx, resource.SchemaRequest{}, &got)
		walkResourceAttributes(ctx, meta.TypeName, got.Schema.Attributes, &found)
		walkResourceBlocks(ctx, meta.TypeName, got.Schema.Blocks, &found)
	}
	for _, newDataSource := range provider.DataSources(ctx) {
		ds := newDataSource()
		var meta datasource.MetadataResponse
		ds.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "unifi"}, &meta)
		var got datasource.SchemaResponse
		ds.Schema(ctx, datasource.SchemaRequest{}, &got)
		walkDataSourceAttributes(ctx, "data."+meta.TypeName, got.Schema.Attributes, &found)
		walkDataSourceBlocks(ctx, "data."+meta.TypeName, got.Schema.Blocks, &found)
	}
	return found
}

// walkResourceBlocks covers the half of the schema the first version of this
// test could not see at all.
//
// A nested object can be declared as an attribute or as a BLOCK, and the two
// are different Go types with different accessors. Walking only Attributes
// missed every block in the provider -- unifi_wlan's schedule and
// unifi_radius_profile's acct_server and auth_server among them. That was found
// by running this test against the pre-strip tree and having it report 49 of
// the 52 known bindings: the three it missed were all blocks. Without a known
// total to check against, the gap would have looked like a clean pass.
func walkResourceBlocks(
	ctx context.Context,
	prefix string,
	blocks map[string]rschema.Block,
	found *[]nestedAttribute,
) {
	names := sortedAttributeNames(len(blocks), func(yield func(string)) {
		for name := range blocks {
			yield(name)
		}
	})
	for _, name := range names {
		path := prefix + "." + name
		switch block := blocks[name].(type) {
		case rschema.SingleNestedBlock:
			*found = append(*found, nestedAttribute{
				Path: path, Members: resourceMembers(block.Attributes, block.Blocks),
				CustomType: customTypeName(block.CustomType),
			})
			walkResourceAttributes(ctx, path, block.Attributes, found)
			walkResourceBlocks(ctx, path, block.Blocks, found)
		case rschema.ListNestedBlock:
			*found = append(*found, nestedAttribute{
				Path:       path,
				Members:    resourceMembers(block.NestedObject.Attributes, block.NestedObject.Blocks),
				CustomType: customTypeName(block.CustomType),
				// The element object carries its own binding, exactly as a
				// list-nested ATTRIBUTE's does.
				ElementCustomType: customTypeName(block.NestedObject.CustomType),
			})
			walkResourceAttributes(ctx, path, block.NestedObject.Attributes, found)
			walkResourceBlocks(ctx, path, block.NestedObject.Blocks, found)
		case rschema.SetNestedBlock:
			*found = append(*found, nestedAttribute{
				Path:              path,
				Members:           resourceMembers(block.NestedObject.Attributes, block.NestedObject.Blocks),
				CustomType:        customTypeName(block.CustomType),
				ElementCustomType: customTypeName(block.NestedObject.CustomType),
			})
			walkResourceAttributes(ctx, path, block.NestedObject.Attributes, found)
			walkResourceBlocks(ctx, path, block.NestedObject.Blocks, found)
		}
	}
}

func walkDataSourceBlocks(
	ctx context.Context,
	prefix string,
	blocks map[string]dschema.Block,
	found *[]nestedAttribute,
) {
	names := sortedAttributeNames(len(blocks), func(yield func(string)) {
		for name := range blocks {
			yield(name)
		}
	})
	for _, name := range names {
		path := prefix + "." + name
		switch block := blocks[name].(type) {
		case dschema.SingleNestedBlock:
			*found = append(*found, nestedAttribute{
				Path: path, Members: dataSourceMembers(block.Attributes, block.Blocks),
				CustomType: customTypeName(block.CustomType),
			})
			walkDataSourceAttributes(ctx, path, block.Attributes, found)
			walkDataSourceBlocks(ctx, path, block.Blocks, found)
		case dschema.ListNestedBlock:
			*found = append(*found, nestedAttribute{
				Path:              path,
				Members:           dataSourceMembers(block.NestedObject.Attributes, block.NestedObject.Blocks),
				CustomType:        customTypeName(block.CustomType),
				ElementCustomType: customTypeName(block.NestedObject.CustomType),
			})
			walkDataSourceAttributes(ctx, path, block.NestedObject.Attributes, found)
			walkDataSourceBlocks(ctx, path, block.NestedObject.Blocks, found)
		case dschema.SetNestedBlock:
			*found = append(*found, nestedAttribute{
				Path:              path,
				Members:           dataSourceMembers(block.NestedObject.Attributes, block.NestedObject.Blocks),
				CustomType:        customTypeName(block.CustomType),
				ElementCustomType: customTypeName(block.NestedObject.CustomType),
			})
			walkDataSourceAttributes(ctx, path, block.NestedObject.Attributes, found)
			walkDataSourceBlocks(ctx, path, block.NestedObject.Blocks, found)
		}
	}
}

// resourceMembers is the member set of an object that can hold both, because a
// runtime model's tfsdk tags cover its nested blocks as well as its attributes.
func resourceMembers(attributes map[string]rschema.Attribute, blocks map[string]rschema.Block) []string {
	names := attributeNames(attributes)
	for name := range blocks {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func dataSourceMembers(attributes map[string]dschema.Attribute, blocks map[string]dschema.Block) []string {
	names := dataSourceAttributeNames(attributes)
	for name := range blocks {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func walkResourceAttributes(
	ctx context.Context,
	prefix string,
	attributes map[string]rschema.Attribute,
	found *[]nestedAttribute,
) {
	names := sortedAttributeNames(len(attributes), func(yield func(string)) {
		for name := range attributes {
			yield(name)
		}
	})
	for _, name := range names {
		path := prefix + "." + name
		switch attribute := attributes[name].(type) {
		case rschema.SingleNestedAttribute:
			// timeouts is grafted by the provider and backed by the framework's
			// own type; it has no model of ours and never had one.
			if name == "timeouts" {
				continue
			}
			*found = append(*found, nestedAttribute{
				Path: path, Members: attributeNames(attribute.Attributes),
				CustomType: customTypeName(attribute.CustomType),
			})
			walkResourceAttributes(ctx, path, attribute.Attributes, found)
		case rschema.ListNestedAttribute:
			*found = append(*found, nestedAttribute{
				Path: path, Members: attributeNames(attribute.NestedObject.Attributes),
				CustomType:        customTypeName(attribute.CustomType),
				ElementCustomType: customTypeName(attribute.NestedObject.CustomType),
			})
			walkResourceAttributes(ctx, path, attribute.NestedObject.Attributes, found)
		case rschema.SetNestedAttribute:
			*found = append(*found, nestedAttribute{
				Path: path, Members: attributeNames(attribute.NestedObject.Attributes),
				CustomType:        customTypeName(attribute.CustomType),
				ElementCustomType: customTypeName(attribute.NestedObject.CustomType),
			})
			walkResourceAttributes(ctx, path, attribute.NestedObject.Attributes, found)
		}
	}
}

func walkDataSourceAttributes(
	ctx context.Context,
	prefix string,
	attributes map[string]dschema.Attribute,
	found *[]nestedAttribute,
) {
	names := sortedAttributeNames(len(attributes), func(yield func(string)) {
		for name := range attributes {
			yield(name)
		}
	})
	for _, name := range names {
		path := prefix + "." + name
		switch attribute := attributes[name].(type) {
		case dschema.SingleNestedAttribute:
			if name == "timeouts" {
				continue
			}
			*found = append(*found, nestedAttribute{
				Path: path, Members: dataSourceAttributeNames(attribute.Attributes),
				CustomType: customTypeName(attribute.CustomType),
			})
			walkDataSourceAttributes(ctx, path, attribute.Attributes, found)
		case dschema.ListNestedAttribute:
			*found = append(*found, nestedAttribute{
				Path: path, Members: dataSourceAttributeNames(attribute.NestedObject.Attributes),
				CustomType:        customTypeName(attribute.CustomType),
				ElementCustomType: customTypeName(attribute.NestedObject.CustomType),
			})
			walkDataSourceAttributes(ctx, path, attribute.NestedObject.Attributes, found)
		case dschema.SetNestedAttribute:
			*found = append(*found, nestedAttribute{
				Path: path, Members: dataSourceAttributeNames(attribute.NestedObject.Attributes),
				CustomType:        customTypeName(attribute.CustomType),
				ElementCustomType: customTypeName(attribute.NestedObject.CustomType),
			})
			walkDataSourceAttributes(ctx, path, attribute.NestedObject.Attributes, found)
		}
	}
}

func sortedAttributeNames(size int, each func(func(string))) []string {
	names := make([]string, 0, size)
	each(func(name string) { names = append(names, name) })
	sort.Strings(names)
	return names
}

func attributeNames(attributes map[string]rschema.Attribute) []string {
	names := make([]string, 0, len(attributes))
	for name := range attributes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func dataSourceAttributeNames(attributes map[string]dschema.Attribute) []string {
	names := make([]string, 0, len(attributes))
	for name := range attributes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func customTypeName(custom any) string {
	if custom == nil {
		return ""
	}
	return fmt.Sprintf("%T", custom)
}
