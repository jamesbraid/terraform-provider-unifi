package main

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	resource_dhcp_option "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_dhcp_option"
	resource_dpi_group "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_dpi_group"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	resource_wlan_group "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_wlan_group"
)

// surface is one descriptor the emitter serves. Everything else about it is
// derived: the SDK struct from the mapping artifact, the wires from the
// mapping's managed fields, the model from the schema's attributes.
type surface struct {
	// name is the shared basename: <name>.mapping.json,
	// <name>_descriptor.go and <name>_descriptor_gen.go.
	name string
	// schema builds the surface's generated schema. Nil for a settings
	// section, whose schema is the named SingleNestedAttribute of
	// unifi_setting's.
	schema func(context.Context) schema.Schema
	// section names the resource_setting attribute a section descriptor
	// serves; empty for a managed resource.
	section string
}

func (s surface) built(ctx context.Context) schema.Schema {
	if s.section == "" {
		return s.schema(ctx)
	}
	built := resource_setting.SettingResourceSchema(ctx)
	nested := built.Attributes[s.section].(schema.SingleNestedAttribute) //nolint:forcetypeassert // every section is a SingleNestedAttribute in the generated schema; a mismatch is a generator regression to fail loudly on.
	return schema.Schema{Attributes: nested.Attributes}
}

var surfaces = []surface{
	{name: "dhcp_option", schema: resource_dhcp_option.DhcpOptionResourceSchema},
	{name: "dpi_group", schema: resource_dpi_group.DpiGroupResourceSchema},
	{name: "wlan_group", schema: resource_wlan_group.WlanGroupResourceSchema},
}
