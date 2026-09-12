package unifi

// The doh section descriptor: replaces the hand-written writeDohSection /
// readDohSection (setting_sections.go) and their dohModelToSetting /
// dohSettingToModel mappers (deleted from setting_resource.go). The model
// types and attribute-type maps moved here too, from setting_resource.go:
// descriptor_mapping_test.go's loadDescriptors reads a descriptor's model
// tags from the same file the Spec literal is in. See
// setting_mgmt_descriptor.go for the shape every section descriptor
// follows; custom_servers is doh's own ObjectListField, the same kind
// mgmt's ssh_keys uses.
//
// settings.Doh's three members (custom_servers, server_names, state) carry
// no "x_" prefix and no pointer -- unlike mgmt/radius, none of them
// distinguishes "unset" from "the Go zero" at the wire, so every attribute
// needs the plan-conditioned null dohAfterReceive applies, matching every
// case the deleted dohSettingToModel conditioned on plan.IsNull().
//
// custom_servers diverges from mgmt's ssh_keys in exactly one respect:
// dohAfterReceive does NOT additionally null a configured-but-empty list
// (mgmt's AfterReceive does, in its own second switch case). The deleted
// dohSettingToModel built the list straight from setting.CustomServers
// whenever plan.CustomServers was configured, empty or not, and
// TestDohConfiguredEmptyCustomServersReadsBackAsEmptyList pins that: a
// configured `custom_servers = []` reads back as an empty, non-null list.

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// dohKitSpec maps every attribute of the generated doh schema
// (resource_setting/setting_resource_gen.go's "doh" SingleNestedAttribute)
// onto settings.Doh. Elide judgments follow resourcekit.ElideProblems'
// schema-driven rule: custom_servers and server_names are Optional+Computed
// list attributes (not schema.StringAttribute, so ElideProblems' own zero
// check never fires on them) and so want KeepZero, same as mgmt's ssh_keys;
// state is Optional+Computed with a OneOf("off","auto","manual","custom")
// validator that rejects "", so it wants NullZero, same rule
// util.StringValueOrNull applied by hand. The plan-conditioned nulls
// dohSettingToModel applied on top of that live in dohAfterReceive instead,
// attribute by attribute -- see its own comment.
func dohKitSpec() resourcekit.Spec[settingDohModel, settings.Doh] {
	return resourcekit.Spec[settingDohModel, settings.Doh]{
		TypeName: "setting_doh",
		Subject:  "DoH Setting",
		New:      func() *settings.Doh { return &settings.Doh{} },
		Fields: resourcekit.Override(settingDohGenFields(), []resourcekit.Field[settingDohModel, settings.Doh]{
			resourcekit.ObjectListField[settingDohModel, settings.Doh, settings.SettingDohCustomServers]{
				Wire:      "custom_servers",
				Model:     func(m *settingDohModel) *types.List { return &m.CustomServers },
				SDK:       func(s *settings.Doh) *[]settings.SettingDohCustomServers { return &s.CustomServers },
				AttrTypes: dohCustomServersAttrTypes,
				Encode:    dohCustomServerEncode,
				Decode:    dohCustomServerDecode,
				Elide:     resourcekit.KeepZero,
			},
		}),
	}
}

// dohCustomServerEncode mirrors the deleted dohModelToSetting's per-element
// loop: enabled defaults to true when the model leaves it null/unknown,
// which is what the generated schema's own Default: booldefault.StaticBool
// (true) already forces at plan time -- this is the same default applied
// defensively at the Encode boundary, matching the deleted mapper exactly.
func dohCustomServerEncode(
	ctx context.Context, object types.Object,
) (settings.SettingDohCustomServers, diag.Diagnostics) {
	var model dohCustomServersModel
	diags := object.As(ctx, &model, basetypes.ObjectAsOptions{})
	enabled := true
	if !model.Enabled.IsNull() && !model.Enabled.IsUnknown() {
		enabled = model.Enabled.ValueBool()
	}
	return settings.SettingDohCustomServers{
		Enabled:    enabled,
		SdnsStamp:  model.SdnsStamp.ValueString(),
		ServerName: model.ServerName.ValueString(),
	}, diags
}

func dohCustomServerDecode(
	ctx context.Context, element settings.SettingDohCustomServers,
) (types.Object, diag.Diagnostics) {
	return types.ObjectValueFrom(ctx, dohCustomServersAttrTypes, dohCustomServersModel{
		Enabled:    types.BoolValue(element.Enabled),
		SdnsStamp:  types.StringValue(element.SdnsStamp),
		ServerName: types.StringValue(element.ServerName),
	})
}

// dohAfterReceive reproduces today's dohSettingToModel: every doh attribute
// is plan-conditioned -- null unless the practitioner's own config (prior)
// set it, so an unmanaged doh attribute never drifts. Unlike mgmt's
// ssh_keys, a configured-but-empty custom_servers (or server_names) is left
// exactly as Spec.ToModel already decoded it -- a non-null empty list, not
// renulled -- because the deleted dohSettingToModel had no second
// len==0 case for either list, only the single plan.IsNull()/IsUnknown()
// guard reproduced below.
func dohAfterReceive(
	_ context.Context, _ *settings.Doh, model *settingDohModel, prior settingDohModel,
) diag.Diagnostics {
	if prior.State.IsNull() || prior.State.IsUnknown() {
		model.State = types.StringNull()
	}
	if prior.ServerNames.IsNull() || prior.ServerNames.IsUnknown() {
		model.ServerNames = types.ListNull(types.StringType)
	}
	if prior.CustomServers.IsNull() || prior.CustomServers.IsUnknown() {
		model.CustomServers = types.ListNull(types.ObjectType{AttrTypes: dohCustomServersAttrTypes})
	}
	return nil
}

// dohKitBackend binds dohKitSpec to a client: Read is GetSetting[*Doh],
// UpdateFields is the masked UpdateSettingFields -- naming only the fields
// the plan set instead of the read-modify-write whole-document PUT
// writeDohSection used.
func dohKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.Doh] {
	return resourcekit.Backend[settings.Doh]{
		Read: func(ctx context.Context, site, _ string) (*settings.Doh, error) {
			_, doh, err := ui.GetSetting[*settings.Doh](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return doh, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.Doh, fields ...string,
		) (*settings.Doh, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// dohKitSection builds the doh entry for settingResource's Sections, bound to
// client via settingKitSections, which calls it with r.client.ApiClient.
func dohKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := dohKitSpec()
	spec.Backend = dohKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingDohModel, settings.Doh]{
		SectionName:  "doh",
		Get:          func(m *settingResourceModel) *types.Object { return &m.Doh },
		Set:          func(m *settingResourceModel, o types.Object) { m.Doh = o },
		AttrTypes:    dohAttrTypes,
		Spec:         spec,
		AfterReceive: dohAfterReceive,
	}
}
