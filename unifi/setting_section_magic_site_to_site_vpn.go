package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// settingMagicSiteToSiteVpnModel is the nested magic_site_to_site_vpn block:
// UniFi's Site Magic WireGuard mesh. `enabled` aligns with filipowm's
// unifi_setting_magic_site_to_site_vpn; the key pair is our superset.
// go-unifi's typed struct models only `enabled`, so this section reads the
// raw settings document to surface the controller-generated key material.
type settingMagicSiteToSiteVpnModel struct {
	Enabled    types.Bool   `tfsdk:"enabled"`
	PublicKey  types.String `tfsdk:"public_key"`
	PrivateKey types.String `tfsdk:"private_key"`
}

var magicSiteToSiteVpnAttrTypes = map[string]attr.Type{
	"enabled":     types.BoolType,
	"public_key":  types.StringType,
	"private_key": types.StringType,
}

type magicSiteToSiteVpnSection struct{}

func (magicSiteToSiteVpnSection) key() string { return "magic_site_to_site_vpn" }

func (magicSiteToSiteVpnSection) attrTypes() map[string]attr.Type {
	return magicSiteToSiteVpnAttrTypes
}

func (magicSiteToSiteVpnSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Site Magic site-to-site VPN (WireGuard mesh) " +
			"settings. The controller generates the key pair when the " +
			"feature is first enabled; leave `private_key` unset to keep " +
			"the controller-managed keys.",
		Optional: true,
		Computed: true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the Site Magic site-to-site VPN is enabled.",
				Optional:            true,
				Computed:            true,
			},
			"public_key": schema.StringAttribute{
				MarkdownDescription: "WireGuard public key, derived by the " +
					"controller from the private key. Read-only.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"private_key": schema.StringAttribute{
				MarkdownDescription: "WireGuard private key. Controller-" +
					"generated unless explicitly set; never required. " +
					"The key persists in Terraform state and is resent " +
					"to the controller on subsequent applies (a " +
					"controller no-op); protect your state file " +
					"accordingly.",
				Optional:  true,
				Computed:  true,
				Sensitive: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (magicSiteToSiteVpnSection) get(m *settingResourceModel) types.Object {
	return m.MagicSiteToSiteVpn
}

func (magicSiteToSiteVpnSection) set(m *settingResourceModel, obj types.Object) {
	m.MagicSiteToSiteVpn = obj
}

func (magicSiteToSiteVpnSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingMagicSiteToSiteVpnModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	magicSiteToSiteVpnModelToData(&m, data)
	return diags
}

// magicSiteToSiteVpnModelToData writes only the user-set fields into the raw
// section document. public_key is controller-derived and is NEVER written;
// x_private_key is written only when set (either by the user, or read back
// into state on a previous apply — re-sending the value the GET just
// returned is a no-op for the controller).
func magicSiteToSiteVpnModelToData(
	m *settingMagicSiteToSiteVpnModel,
	data map[string]any,
) {
	if !m.Enabled.IsNull() && !m.Enabled.IsUnknown() {
		data["enabled"] = m.Enabled.ValueBool()
	}
	if !m.PrivateKey.IsNull() && !m.PrivateKey.IsUnknown() {
		data["x_private_key"] = m.PrivateKey.ValueString()
	}
}

// read uses the raw settings list rather than the typed struct: go-unifi's
// MagicSiteToSiteVpn models only `enabled`, but the schema's computed
// public_key/private_key must be populated from the live document.
// TODO(go-unifi): switch to the typed getter once settings.MagicSiteToSiteVpn
// models public_key/x_private_key.
func (magicSiteToSiteVpnSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	raws, err := client.ListSettings(ctx, site)
	if err != nil {
		diags.AddError("Error Reading Magic Site-to-Site VPN Setting", err.Error())
		return types.ObjectNull(magicSiteToSiteVpnAttrTypes), diags
	}
	for _, raw := range raws {
		if raw.GetKey() != "magic_site_to_site_vpn" {
			continue
		}
		return types.ObjectValueFrom(ctx, magicSiteToSiteVpnAttrTypes,
			magicSiteToSiteVpnDataToModel(raw.Data))
	}
	return types.ObjectNull(magicSiteToSiteVpnAttrTypes), diags
}

func magicSiteToSiteVpnDataToModel(
	data map[string]any,
) settingMagicSiteToSiteVpnModel {
	enabled, _ := data["enabled"].(bool)
	publicKey, _ := data["public_key"].(string)
	privateKey, _ := data["x_private_key"].(string)
	return settingMagicSiteToSiteVpnModel{
		Enabled:    types.BoolValue(enabled),
		PublicKey:  util.StringValueOrNull(publicKey),
		PrivateKey: util.StringValueOrNull(privateKey),
	}
}
