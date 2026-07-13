package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// magicSiteToSiteVpnSection is the settingSection implementation for the
// "magic_site_to_site_vpn" settings section. Only "enabled" is modeled,
// decoded/overlaid via plain decodeBool/overlayBool — no different from
// teleport.enabled or any B1 scalar. Everything else in the section's wire
// data (including a generated secret/PSK, IF one exists — see
// settingMagicSiteToSiteVpnModel's doc comment for why none is added here
// without evidence) passes through untouched via overlay()'s
// snap.dataCopy(key()) RMW base. This is intentional preservation-by-
// omission, not an oversight: C1's PreservedUnmanaged class applied by
// omission rather than special code, since overlay() never builds a PUT
// body from the model alone.
type magicSiteToSiteVpnSection struct{}

func init() {
	registerSection(magicSiteToSiteVpnSection{})
}

func (magicSiteToSiteVpnSection) key() string      { return "magic_site_to_site_vpn" }
func (magicSiteToSiteVpnSection) attrName() string { return "magic_site_to_site_vpn" }

// schemaAttribute returns the "magic_site_to_site_vpn" SingleNestedAttribute
// with its single modeled leaf, "enabled".
func (magicSiteToSiteVpnSection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Magic Site-to-Site VPN (mesh) settings. Only `enabled` is modeled — " +
			"the pinned go-unifi SDK exposes no other field for this section. Any additional " +
			"controller-managed value (e.g. a generated key, if one exists — unconfirmed) is " +
			"preserved untouched across updates via the standard read-modify-write mechanism.",
		Optional: true,
		Computed: true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether Magic Site-to-Site VPN is enabled.",
				Optional:            true,
				Computed:            true,
			},
		},
	}
}

// decode populates model.MagicSiteToSiteVpn from snap's
// "magic_site_to_site_vpn" section data.
func (s magicSiteToSiteVpnSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	var priorModel settingMagicSiteToSiteVpnModel
	if !prior.MagicSiteToSiteVpn.IsNull() && !prior.MagicSiteToSiteVpn.IsUnknown() {
		diags.Append(prior.MagicSiteToSiteVpn.As(ctx, &priorModel, basetypes.ObjectAsOptions{})...)
	}

	sec, _ := snap.section(s.key())
	data := sec.Data

	enabled, d := decodeBool(data, "enabled", priorModel.Enabled)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	m := settingMagicSiteToSiteVpnModel{Enabled: enabled}

	obj, objDiags := types.ObjectValueFrom(ctx, magicSiteToSiteVpnAttrTypes, m)
	diags.Append(objDiags...)
	if diags.HasError() {
		return diags
	}

	model.MagicSiteToSiteVpn = obj
	return diags
}

// overlay computes the "magic_site_to_site_vpn" PUT body from
// model.MagicSiteToSiteVpn, starting from a deep copy of the snapshot's
// current section data (snap.dataCopy(s.key())) so any unmodeled field the
// controller stores here — including a generated secret/PSK, if one exists
// — passes through untouched on every update: this is C1's
// PreservedUnmanaged class applied by omission, not by a special code path.
// Returns configured == false (no write) when the section is not configured
// (null/unknown) in model.
func (s magicSiteToSiteVpnSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.MagicSiteToSiteVpn.IsNull() || model.MagicSiteToSiteVpn.IsUnknown() {
		return settings.RawSetting{}, false, diags
	}

	var m settingMagicSiteToSiteVpnModel
	diags.Append(model.MagicSiteToSiteVpn.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	base := snap.dataCopy(s.key()) // any unmodeled field, incl. a real generated
	// secret if one exists on the wire, survives
	// here untouched — no special-case code.
	overlayBool(base, "enabled", m.Enabled)

	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: s.key()},
		Data:        base,
	}
	return rs, true, diags
}

// carryBestEffort copies the plan's magic_site_to_site_vpn value onto dst.
// This section holds no secret leaves, so it is a straight copy with no
// per-leaf plan/prior choice needed.
func (magicSiteToSiteVpnSection) carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics {
	dst.MagicSiteToSiteVpn = plan.MagicSiteToSiteVpn
	return nil
}

// isConfigured reports whether m.MagicSiteToSiteVpn is set (non-null,
// non-unknown), gating whether Create/Update push this section to the
// controller at all.
func (magicSiteToSiteVpnSection) isConfigured(m settingResourceModel) bool {
	return !m.MagicSiteToSiteVpn.IsNull() && !m.MagicSiteToSiteVpn.IsUnknown()
}
