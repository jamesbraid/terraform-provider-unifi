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
	// object -- a separate binding site from CustomType.
	ElementCustomType string
}

// TestServedSchemaAgreesWithItsRuntimeModel checks that every object-valued
// attribute in a served schema resolves to a model whose tfsdk tags are
// exactly its members, that no generated CustomType/Value pair has
// reappeared, and that each model's own AttributeTypes() restates its tags
// correctly.
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
		// Several models sharing a member set is not itself a finding --
		// handled via declaredAmbiguous below.
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

	generated, err := schemamodel.GeneratedTypes("../internal/generated")
	if err != nil {
		t.Fatal(err)
	}
	if len(generated) != 0 {
		names := make([]string, 0, len(generated))
		for name := range generated {
			names = append(names, name)
		}
		sort.Strings(names)
		t.Errorf("the generated tree declares %d value type(s) again: %v.\n\n"+
			"cmd/generated-value-strip removes them after generation, so one existing "+
			"means that step stopped running -- and with it the CustomType bindings "+
			"they pair with, which the framework refuses at apply time. Restore the "+
			"go:generate line, and restore the pairing check this replaced.",
			len(generated), names)
	}

	// A model that restates its own shape via AttributeTypes() must restate
	// it correctly; the framework converts through both paths.
	for _, model := range index.Disagreements() {
		t.Errorf("%s in %s declares tfsdk tags %v but its own AttributeTypes() declares %v; "+
			"the framework converts through both and they must be the same set",
			model.Name, model.File, model.Tags(), model.RestatedTags())
	}

	// A shared member set across unrelated models is not a defect, but it does
	// mean the check above can't fail for that attribute -- break the model
	// that actually serves it and a sibling still matches. Declared here and
	// compared as a set so a new or resolved ambiguity can't pass unnoticed.
	declaredAmbiguous := map[string]string{
		"unifi_network.dhcp_guarding":                    "dhcpGuardingModel dhcpRelayModel vpnServerDNSModel",
		"unifi_network.dhcp_relay":                       "dhcpGuardingModel dhcpRelayModel vpnServerDNSModel",
		"unifi_setting.ether_lighting.network_overrides": "settingEtherLightingNetworkOverrideModel settingEtherLightingSpeedOverrideModel",
		"unifi_setting.ether_lighting.speed_overrides":   "settingEtherLightingNetworkOverrideModel settingEtherLightingSpeedOverrideModel",
		"unifi_setting.ips.suppression_alerts.tracking":  "settingIpsTrackingModel settingIpsWhitelistModel",
		"unifi_setting.ips.suppression_whitelist":        "settingIpsTrackingModel settingIpsWhitelistModel",
		"unifi_vpn_server.dns":                           "dhcpGuardingModel dhcpRelayModel vpnServerDNSModel",
		"data.unifi_network.dhcp_guarding":               "dhcpGuardingModel dhcpRelayModel vpnServerDNSModel",
		"data.unifi_network.dhcp_relay":                  "dhcpGuardingModel dhcpRelayModel vpnServerDNSModel",
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
		walkResourceAttributes(meta.TypeName, got.Schema.Attributes, &found)
		walkResourceBlocks(meta.TypeName, got.Schema.Blocks, &found)
	}
	for _, newDataSource := range provider.DataSources(ctx) {
		ds := newDataSource()
		var meta datasource.MetadataResponse
		ds.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "unifi"}, &meta)
		var got datasource.SchemaResponse
		ds.Schema(ctx, datasource.SchemaRequest{}, &got)
		walkDataSourceAttributes("data."+meta.TypeName, got.Schema.Attributes, &found)
		walkDataSourceBlocks("data."+meta.TypeName, got.Schema.Blocks, &found)
	}
	return found
}

// walkResourceBlocks walks nested schema declared as a BLOCK -- a separate
// Go type from Attributes, with its own accessors, that a walk of
// attributes alone does not see.
func walkResourceBlocks(
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
			walkResourceAttributes(path, block.Attributes, found)
			walkResourceBlocks(path, block.Blocks, found)
		case rschema.ListNestedBlock:
			*found = append(*found, nestedAttribute{
				Path:       path,
				Members:    resourceMembers(block.NestedObject.Attributes, block.NestedObject.Blocks),
				CustomType: customTypeName(block.CustomType),
				// The element object carries its own binding, exactly as a
				// list-nested attribute's does.
				ElementCustomType: customTypeName(block.NestedObject.CustomType),
			})
			walkResourceAttributes(path, block.NestedObject.Attributes, found)
			walkResourceBlocks(path, block.NestedObject.Blocks, found)
		case rschema.SetNestedBlock:
			*found = append(*found, nestedAttribute{
				Path:              path,
				Members:           resourceMembers(block.NestedObject.Attributes, block.NestedObject.Blocks),
				CustomType:        customTypeName(block.CustomType),
				ElementCustomType: customTypeName(block.NestedObject.CustomType),
			})
			walkResourceAttributes(path, block.NestedObject.Attributes, found)
			walkResourceBlocks(path, block.NestedObject.Blocks, found)
		}
	}
}

func walkDataSourceBlocks(
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
			walkDataSourceAttributes(path, block.Attributes, found)
			walkDataSourceBlocks(path, block.Blocks, found)
		case dschema.ListNestedBlock:
			*found = append(*found, nestedAttribute{
				Path:              path,
				Members:           dataSourceMembers(block.NestedObject.Attributes, block.NestedObject.Blocks),
				CustomType:        customTypeName(block.CustomType),
				ElementCustomType: customTypeName(block.NestedObject.CustomType),
			})
			walkDataSourceAttributes(path, block.NestedObject.Attributes, found)
			walkDataSourceBlocks(path, block.NestedObject.Blocks, found)
		case dschema.SetNestedBlock:
			*found = append(*found, nestedAttribute{
				Path:              path,
				Members:           dataSourceMembers(block.NestedObject.Attributes, block.NestedObject.Blocks),
				CustomType:        customTypeName(block.CustomType),
				ElementCustomType: customTypeName(block.NestedObject.CustomType),
			})
			walkDataSourceAttributes(path, block.NestedObject.Attributes, found)
			walkDataSourceBlocks(path, block.NestedObject.Blocks, found)
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
			walkResourceAttributes(path, attribute.Attributes, found)
		case rschema.ListNestedAttribute:
			*found = append(*found, nestedAttribute{
				Path: path, Members: attributeNames(attribute.NestedObject.Attributes),
				CustomType:        customTypeName(attribute.CustomType),
				ElementCustomType: customTypeName(attribute.NestedObject.CustomType),
			})
			walkResourceAttributes(path, attribute.NestedObject.Attributes, found)
		case rschema.SetNestedAttribute:
			*found = append(*found, nestedAttribute{
				Path: path, Members: attributeNames(attribute.NestedObject.Attributes),
				CustomType:        customTypeName(attribute.CustomType),
				ElementCustomType: customTypeName(attribute.NestedObject.CustomType),
			})
			walkResourceAttributes(path, attribute.NestedObject.Attributes, found)
		}
	}
}

func walkDataSourceAttributes(
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
			walkDataSourceAttributes(path, attribute.Attributes, found)
		case dschema.ListNestedAttribute:
			*found = append(*found, nestedAttribute{
				Path: path, Members: dataSourceAttributeNames(attribute.NestedObject.Attributes),
				CustomType:        customTypeName(attribute.CustomType),
				ElementCustomType: customTypeName(attribute.NestedObject.CustomType),
			})
			walkDataSourceAttributes(path, attribute.NestedObject.Attributes, found)
		case dschema.SetNestedAttribute:
			*found = append(*found, nestedAttribute{
				Path: path, Members: dataSourceAttributeNames(attribute.NestedObject.Attributes),
				CustomType:        customTypeName(attribute.CustomType),
				ElementCustomType: customTypeName(attribute.NestedObject.CustomType),
			})
			walkDataSourceAttributes(path, attribute.NestedObject.Attributes, found)
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
