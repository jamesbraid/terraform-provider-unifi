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
	Path       string
	Members    []string
	CustomType string
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
// unifi_firewall_policy, which has no managed acceptance test and sixteen uses
// on the fleet.
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

	// Direction two: no model field may be typed against a generated custom
	// value type. Nothing in the provider can produce one -- that is the defect
	// nested-custom-type-strip removed from the schema side -- so a field
	// declaring one is the same mismatch arriving from the other end.
	generated, err := schemamodel.GeneratedTypes("../internal/generated")
	if err != nil {
		t.Fatal(err)
	}
	if len(generated) == 0 {
		t.Fatal("no types found in the generated tree, so the check below would be vacuous")
	}
	for _, model := range index.Models {
		for tag, goType := range model.Fields {
			if _, isGenerated := generated[strings.TrimPrefix(goType, "*")]; isGenerated {
				t.Errorf("%s.%s in %s is declared %s, a type declared in the generated tree; "+
					"nothing in the provider produces one and the served schema no longer asks for it",
					model.Name, tag, model.File, goType)
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
		"unifi_traffic_route.source.clients":            "clientIdentityModel sourceClientModel",
		"unifi_traffic_route.source.networks":           "networkIdentityModel sourceNetworkModel trafficRouteIdentityModel vpnClientIdentityModel vpnServerIdentityModel",
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
	}
	for _, newDataSource := range provider.DataSources(ctx) {
		ds := newDataSource()
		var meta datasource.MetadataResponse
		ds.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "unifi"}, &meta)
		var got datasource.SchemaResponse
		ds.Schema(ctx, datasource.SchemaRequest{}, &got)
		walkDataSourceAttributes(ctx, "data."+meta.TypeName, got.Schema.Attributes, &found)
	}
	return found
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
				CustomType: customTypeName(attribute.CustomType),
			})
			walkResourceAttributes(ctx, path, attribute.NestedObject.Attributes, found)
		case rschema.SetNestedAttribute:
			*found = append(*found, nestedAttribute{
				Path: path, Members: attributeNames(attribute.NestedObject.Attributes),
				CustomType: customTypeName(attribute.CustomType),
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
				CustomType: customTypeName(attribute.CustomType),
			})
			walkDataSourceAttributes(ctx, path, attribute.NestedObject.Attributes, found)
		case dschema.SetNestedAttribute:
			*found = append(*found, nestedAttribute{
				Path: path, Members: dataSourceAttributeNames(attribute.NestedObject.Attributes),
				CustomType: customTypeName(attribute.CustomType),
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
