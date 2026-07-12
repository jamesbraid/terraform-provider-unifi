package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// igmpSnoopingSection is the settingSection implementation for the
// "igmp_snooping" settings section. It is a small scalar+list section (two
// leaves: enabled bool, network_ids string-list) — structurally like syslog
// — but it is the first read-modify-write (RMW) section: the controller's
// stored data for this key carries fields the model does not expose at all
// (querier_mode, switches, flood_known_protocols,
// forward_unknown_mcast_router_ports). overlay() starts from a copy of the
// snapshot's current section data so those unmodeled fields survive the
// merge untouched.
type igmpSnoopingSection struct{}

func init() {
	registerSection(igmpSnoopingSection{})
}

func (igmpSnoopingSection) key() string      { return "igmp_snooping" }
func (igmpSnoopingSection) attrName() string { return "igmp_snooping" }

// schemaAttribute is byte-identical to the inline "igmp_snooping" block in
// setting_resource.go's schema. Unlike every other section, the parent
// SingleNestedAttribute here is Optional-ONLY: no Computed, no
// PlanModifiers. Do not add either to "match" other sections.
func (igmpSnoopingSection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Site-level IGMP snooping setting. On UniFi Network 10.3.x+ the effective IGMP snooping toggle lives here rather than on each network. Advanced querier/flood options configured in the UI are preserved across updates.",
		Optional:            true,
		Attributes: map[string]schema.Attribute{
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether IGMP snooping is enabled for the site.",
				Optional:            true,
				Computed:            true,
			},
			"network_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of the networks IGMP snooping applies to.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

// decode populates model.IgmpSnooping from snap's "igmp_snooping" section
// data. Only the two modeled leaves (enabled, network_ids) are read; the
// unmodeled fields the controller may also store there (querier_mode,
// switches, ...) are simply not decoded because they are not in the model.
func (s igmpSnoopingSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	var priorModel settingIgmpSnoopingModel
	if !prior.IgmpSnooping.IsNull() && !prior.IgmpSnooping.IsUnknown() {
		diags.Append(prior.IgmpSnooping.As(ctx, &priorModel, basetypes.ObjectAsOptions{})...)
	}

	sec, _ := snap.section(s.key())
	data := sec.Data

	enabled, d := decodeBool(data, "enabled", priorModel.Enabled)
	diags.Append(d...)
	networkIDs, d := decodeStringList(ctx, data, "network_ids", priorModel.NetworkIDs)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	m := settingIgmpSnoopingModel{
		Enabled:    enabled,
		NetworkIDs: networkIDs,
	}

	obj, objDiags := types.ObjectValueFrom(ctx, igmpSnoopingAttrTypes, m)
	diags.Append(objDiags...)
	if diags.HasError() {
		return diags
	}

	model.IgmpSnooping = obj
	return diags
}

// overlay computes the "igmp_snooping" PUT body from model.IgmpSnooping,
// starting from a deep copy of the snapshot's current section data so any
// unmodeled key already present on the controller (querier_mode, switches,
// flood_known_protocols, forward_unknown_mcast_router_ports) is preserved.
// Returns configured == false (no write) when the section is not configured
// (null/unknown) in model.
func (s igmpSnoopingSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.IgmpSnooping.IsNull() || model.IgmpSnooping.IsUnknown() {
		return settings.RawSetting{}, false, diags
	}

	var m settingIgmpSnoopingModel
	diags.Append(model.IgmpSnooping.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	base := snap.dataCopy(s.key())
	overlayBool(base, "enabled", m.Enabled)
	diags.Append(overlayStringList(ctx, base, "network_ids", m.NetworkIDs)...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: s.key()},
		Data:        base,
	}
	return rs, true, diags
}

func (s igmpSnoopingSection) capability(snap rawSettings) capabilityState {
	return sectionCapability(snap, s.key())
}

// carryBestEffort copies the plan's igmp_snooping value onto dst. This
// section holds no secret leaves, so it is a straight copy with no per-leaf
// plan/prior choice needed.
func (igmpSnoopingSection) carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics {
	dst.IgmpSnooping = plan.IgmpSnooping
	return nil
}

func (igmpSnoopingSection) isConfigured(m settingResourceModel) bool {
	return !m.IgmpSnooping.IsNull() && !m.IgmpSnooping.IsUnknown()
}
