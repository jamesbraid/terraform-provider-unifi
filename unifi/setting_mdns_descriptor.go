package unifi

// The mdns section descriptor: shaped like setting_doh_descriptor.go for
// its String discriminator (doh's state, mdns's mode), but mdns needs two
// things doh's own doesn't -- see mdnsAfterReceive's own comment and
// setting_mdns_validate.go's validateMdnsConfig for the measured reasons.
// custom_services and predefined_services are mdns's own ObjectListFields,
// the same kind doh's custom_servers uses.
//
// mode's three values ("all", "auto", "custom") gate whether the controller
// consults custom_services/predefined_services at all. settings.Mdns's own
// generated comment on the Mode field only gives the enum
// ("all|auto|custom"); it says nothing about what each value does with the
// two lists. That behavior was measured directly, with a raw
// UpdateSettingFields/GetSetting probe against the pinned simulation
// controller, bypassing this descriptor entirely: only "custom" honors
// either list. Two consequences follow from that, at two different layers:
//
//   - Read side: predefined_services does not read back empty under "auto"
//     -- the controller returns its full catalog of known predefined
//     services instead, a value it invented, not one the plan set.
//     mdnsAfterReceive normalizes both lists to empty whenever the current
//     mode isn't "custom", so state doesn't echo controller-invented
//     content back as if it were live config.
//   - Plan side: a config that sets mode != "custom" while leaving either
//     list non-empty asks the provider to apply a value the controller
//     will never actually store. validateMdnsConfig
//     (setting_mdns_validate.go) rejects that at plan time instead of
//     letting it reach apply as an opaque "Provider produced inconsistent
//     result after apply".
import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// settingMdnsCustomServiceModel is one element of mdns's custom_services
// list.
type settingMdnsCustomServiceModel struct {
	Address types.String `tfsdk:"address"`
	Name    types.String `tfsdk:"name"`
}

// settingMdnsPredefinedServiceModel is one element of mdns's
// predefined_services list. settings.SettingMdnsPredefinedServices carries
// only "code" on the wire, so this list_nested element mirrors that single
// field rather than flattening to a bare string list -- the same
// one-field-object shape dashboard's widgets and ether_lighting's overrides
// already use for their own multi-field elements.
type settingMdnsPredefinedServiceModel struct {
	Code types.String `tfsdk:"code"`
}

// settingMdnsModel is mdns's own section model, decoded out of
// settingResourceModel.Mdns.
type settingMdnsModel struct {
	Mode               types.String `tfsdk:"mode"`
	CustomServices     types.List   `tfsdk:"custom_services"`
	PredefinedServices types.List   `tfsdk:"predefined_services"`
}

// mdnsCustomServiceAttrTypes, mdnsPredefinedServiceAttrTypes and
// mdnsAttrTypes type mdns's two lists' elements and mdns's own object in
// state; all three must match the generated schema exactly.
var (
	mdnsCustomServiceAttrTypes = map[string]attr.Type{
		"address": types.StringType,
		"name":    types.StringType,
	}
	mdnsPredefinedServiceAttrTypes = map[string]attr.Type{
		"code": types.StringType,
	}
	mdnsAttrTypes = map[string]attr.Type{
		"mode": types.StringType,
		"custom_services": types.ListType{
			ElemType: types.ObjectType{AttrTypes: mdnsCustomServiceAttrTypes},
		},
		"predefined_services": types.ListType{
			ElemType: types.ObjectType{AttrTypes: mdnsPredefinedServiceAttrTypes},
		},
	}
)

// mdnsKitSpec maps every attribute of the generated mdns schema
// (resource_setting/setting_resource_gen.go's "mdns" SingleNestedAttribute)
// onto settings.Mdns. mode carries a OneOf("all", "auto", "custom")
// validator that rejects "", so it wants NullZero; both lists are
// Optional+Computed with no validator on the list attribute itself (only
// their elements carry one), so they want KeepZero, same as doh's
// custom_servers and dashboard's widgets.
func mdnsKitSpec() resourcekit.Spec[settingMdnsModel, settings.Mdns] {
	return resourcekit.Spec[settingMdnsModel, settings.Mdns]{
		TypeName: "setting_mdns",
		Subject:  "mDNS Setting",
		New:      func() *settings.Mdns { return &settings.Mdns{} },
		Fields: []resourcekit.Field[settingMdnsModel, settings.Mdns]{
			resourcekit.StringField[settingMdnsModel, settings.Mdns]{
				Wire:  "mode",
				Model: func(m *settingMdnsModel) *types.String { return &m.Mode },
				SDK:   func(s *settings.Mdns) *string { return &s.Mode },
				Elide: resourcekit.NullZero,
			},
			resourcekit.ObjectListField[settingMdnsModel, settings.Mdns, settings.SettingMdnsCustomServices]{
				Wire:      "custom_services",
				Model:     func(m *settingMdnsModel) *types.List { return &m.CustomServices },
				SDK:       func(s *settings.Mdns) *[]settings.SettingMdnsCustomServices { return &s.CustomServices },
				AttrTypes: mdnsCustomServiceAttrTypes,
				Encode:    mdnsCustomServiceEncode,
				Decode:    mdnsCustomServiceDecode,
				Elide:     resourcekit.KeepZero,
			},
			resourcekit.ObjectListField[settingMdnsModel, settings.Mdns, settings.SettingMdnsPredefinedServices]{
				Wire:  "predefined_services",
				Model: func(m *settingMdnsModel) *types.List { return &m.PredefinedServices },
				SDK: func(s *settings.Mdns) *[]settings.SettingMdnsPredefinedServices {
					return &s.PredefinedServices
				},
				AttrTypes: mdnsPredefinedServiceAttrTypes,
				Encode:    mdnsPredefinedServiceEncode,
				Decode:    mdnsPredefinedServiceDecode,
				Elide:     resourcekit.KeepZero,
			},
		},
	}
}

func mdnsCustomServiceEncode(
	ctx context.Context, object types.Object,
) (settings.SettingMdnsCustomServices, diag.Diagnostics) {
	var model settingMdnsCustomServiceModel
	diags := object.As(ctx, &model, basetypes.ObjectAsOptions{})
	return settings.SettingMdnsCustomServices{
		Address: model.Address.ValueString(),
		Name:    model.Name.ValueString(),
	}, diags
}

func mdnsCustomServiceDecode(
	ctx context.Context, element settings.SettingMdnsCustomServices,
) (types.Object, diag.Diagnostics) {
	return types.ObjectValueFrom(ctx, mdnsCustomServiceAttrTypes, settingMdnsCustomServiceModel{
		Address: types.StringValue(element.Address),
		Name:    types.StringValue(element.Name),
	})
}

func mdnsPredefinedServiceEncode(
	ctx context.Context, object types.Object,
) (settings.SettingMdnsPredefinedServices, diag.Diagnostics) {
	var model settingMdnsPredefinedServiceModel
	diags := object.As(ctx, &model, basetypes.ObjectAsOptions{})
	return settings.SettingMdnsPredefinedServices{
		Code: model.Code.ValueString(),
	}, diags
}

func mdnsPredefinedServiceDecode(
	ctx context.Context, element settings.SettingMdnsPredefinedServices,
) (types.Object, diag.Diagnostics) {
	return types.ObjectValueFrom(ctx, mdnsPredefinedServiceAttrTypes, settingMdnsPredefinedServiceModel{
		Code: types.StringValue(element.Code),
	})
}

// mdnsAfterReceive normalizes custom_services/predefined_services to an
// empty (not null) list whenever the section's own mode, as just read back
// from the controller, is not "custom" -- overriding whatever ToModel
// already decoded from the SDK response for those two fields. Gated on
// model.Mode (the value now in effect after this write or read), not
// prior.Mode (what the plan asked for): the two agree on every ordinary
// apply, but this is the one that is actually true right after a
// mode-changing write.
//
// Measured, not assumed: TestAccSettingResource_mdns's "custom" -> "auto"
// step configures predefined_services = [] and custom_services = [], and
// against the pinned controller, predefined_services comes back holding
// every predefined service code the controller knows about (25 elements)
// -- not empty, not the prior "custom" list. custom_services was observed
// empty in this same run, but both attributes are normalized here rather
// than only the one that showed the problem: sending the controller's own
// synthesized content back as if it were the practitioner's config is the
// class of bug, not a fact specific to predefined_services, and the
// symmetry costs nothing since resourcekit's masked write never sends a
// list the plan didn't set regardless of what this hook decodes.
func mdnsAfterReceive(
	_ context.Context, _ *settings.Mdns, model *settingMdnsModel, _ settingMdnsModel,
) diag.Diagnostics {
	if model.Mode.ValueString() == "custom" {
		return nil
	}
	var diags diag.Diagnostics
	customServices, d := types.ListValue(types.ObjectType{AttrTypes: mdnsCustomServiceAttrTypes}, []attr.Value{})
	diags.Append(d...)
	predefinedServices, d := types.ListValue(types.ObjectType{AttrTypes: mdnsPredefinedServiceAttrTypes}, []attr.Value{})
	diags.Append(d...)
	model.CustomServices = customServices
	model.PredefinedServices = predefinedServices
	return diags
}

// mdnsNestedSchema is the mdns SingleNestedAttribute's own Attributes,
// wrapped as a schema.Schema so resourcekit's conformance checks -- built
// for a whole resource's top-level schema -- can run against one section of
// unifi_setting instead.
func mdnsNestedSchema(ctx context.Context) schema.Schema {
	built := resource_setting.SettingResourceSchema(ctx)
	mdns := built.Attributes["mdns"].(schema.SingleNestedAttribute) //nolint:forcetypeassert // mdns is declared as SingleNestedAttribute in the generated schema; a mismatch here is a generator regression this is meant to catch loudly.
	return schema.Schema{Attributes: mdns.Attributes}
}

// mdnsKitBackend binds mdnsKitSpec to a client: Read is GetSetting[*Mdns],
// UpdateFields is the masked UpdateSettingFields -- naming only the fields
// the plan set.
func mdnsKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.Mdns] {
	return resourcekit.Backend[settings.Mdns]{
		Read: func(ctx context.Context, site, _ string) (*settings.Mdns, error) {
			_, mdns, err := ui.GetSetting[*settings.Mdns](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return mdns, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.Mdns, fields ...string,
		) (*settings.Mdns, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// mdnsKitSection builds the mdns entry for settingResource's Sections,
// bound to client via settingKitSections, which calls it with
// r.client.ApiClient.
func mdnsKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := mdnsKitSpec()
	spec.Backend = mdnsKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingMdnsModel, settings.Mdns]{
		SectionName:  "mdns",
		Get:          func(m *settingResourceModel) *types.Object { return &m.Mdns },
		Set:          func(m *settingResourceModel, o types.Object) { m.Mdns = o },
		AttrTypes:    mdnsAttrTypes,
		Spec:         spec,
		AfterReceive: mdnsAfterReceive,
	}
}
