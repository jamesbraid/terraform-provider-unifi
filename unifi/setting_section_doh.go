package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// dohSection is the settingSection implementation for the "doh" (Encrypted
// DNS / DNS-over-HTTPS) settings section. It is the nested-list worked
// template (Task 17): a scalar leaf (state), a plain string list
// (server_names), and a ListNested with a bool leaf (custom_servers),
// decoded/overlaid through the generalized nested codec
// (decodeObjectList/overlayObjectList, Task 16b).
//
// Unlike syslog, there is no wire-key remap here: the controller stores
// this section under "doh" and the Terraform attribute is also "doh", so
// key() and attrName() both return "doh".
type dohSection struct{}

func init() {
	registerSection(dohSection{})
}

func (dohSection) key() string      { return "doh" }
func (dohSection) attrName() string { return "doh" }

// schemaAttribute is byte-identical to the inline "doh" block in
// setting_resource.go's schema (Task 24a enforces this with a
// schema-equivalence golden).
func (dohSection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Encrypted DNS (DNS-over-HTTPS) settings.",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"state": schema.StringAttribute{
				MarkdownDescription: "Encrypted DNS state: off, auto, manual, or custom.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("off", "auto", "manual", "custom"),
				},
			},
			"server_names": schema.ListAttribute{
				MarkdownDescription: "Predefined DNS provider names (e.g. \"cloudflare\", \"google\").",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"custom_servers": schema.ListNestedAttribute{
				MarkdownDescription: "Custom DNS servers specified via DNS stamp.",
				Optional:            true,
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"enabled": schema.BoolAttribute{
							MarkdownDescription: "Enable this custom server. Defaults to true.",
							Optional:            true,
							Computed:            true,
							Default:             booldefault.StaticBool(true),
						},
						"sdns_stamp": schema.StringAttribute{
							MarkdownDescription: "DNS stamp (sdns://) for the custom resolver.",
							Required:            true,
						},
						"server_name": schema.StringAttribute{
							MarkdownDescription: "Human-readable name for this custom server.",
							Required:            true,
						},
					},
				},
			},
		},
	}
}

// ownership is the FULL dotted-path map: state, server_names, and each
// custom_servers leaf. custom_servers itself is a container, not a leaf, so
// it has no entry here - the gate-10 leafPaths walker only produces the
// dotted child paths, and an extra bare "custom_servers" key would fail the
// coverage test.
func (dohSection) ownership() map[string]ownershipClass {
	return map[string]ownershipClass{
		"state":                      ownerManaged,
		"server_names":               ownerManaged,
		"custom_servers.enabled":     ownerManaged,
		"custom_servers.sdns_stamp":  ownerManaged,
		"custom_servers.server_name": ownerManaged,
	}
}

// decode populates model.Doh from snap's "doh" section data, falling back
// to prior.Doh's matching leaf for any field whose ownership class does not
// read from the API (none, here - all leaves are ownerManaged).
// custom_servers is decoded through the generalized nested-object-list
// codec (decodeObjectList), which type-dispatches its bool/string leaves
// and looks up ownership by dotted path under the "custom_servers" prefix.
func (s dohSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	own := s.ownership()

	var priorModel settingDohModel
	if !prior.Doh.IsNull() && !prior.Doh.IsUnknown() {
		diags.Append(prior.Doh.As(ctx, &priorModel, basetypes.ObjectAsOptions{})...)
	}

	sec, _ := snap.section(s.key())
	data := sec.Data

	state, d := decodeString(data, "state", own["state"], priorModel.State)
	diags.Append(d...)
	serverNames, d := decodeStringList(ctx, data, "server_names", own["server_names"], priorModel.ServerNames)
	diags.Append(d...)
	customServers, d := decodeObjectList(ctx, data, "custom_servers", own, "custom_servers", priorModel.CustomServers, types.ObjectType{AttrTypes: dohCustomServerAttrTypes})
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	m := settingDohModel{
		State:         state,
		ServerNames:   serverNames,
		CustomServers: customServers,
	}

	obj, objDiags := types.ObjectValueFrom(ctx, dohAttrTypes, m)
	diags.Append(objDiags...)
	if diags.HasError() {
		return diags
	}

	model.Doh = obj
	return diags
}

// overlay computes the "doh" PUT body from model.Doh, starting from a deep
// copy of the snapshot's current section data so any unmodeled key already
// present on the controller is preserved. Returns configured == false (no
// write) when the section is not configured (null/unknown) in model.
func (s dohSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.Doh.IsNull() || model.Doh.IsUnknown() {
		return settings.RawSetting{}, false, diags
	}

	own := s.ownership()

	var m settingDohModel
	diags.Append(model.Doh.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	base := snap.dataCopy(s.key())
	overlayString(base, "state", own["state"], m.State)
	diags.Append(overlayStringList(ctx, base, "server_names", own["server_names"], m.ServerNames)...)
	diags.Append(overlayObjectList(ctx, base, "custom_servers", own, "custom_servers", m.CustomServers)...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: s.key()},
		Data:        base,
	}
	return rs, true, diags
}

func (s dohSection) capability(snap rawSettings) capabilityState {
	return sectionCapability(snap, s.key())
}

// carryBestEffort copies the plan's doh value onto dst. This section holds
// no secret leaves, so it is a straight copy with no per-leaf plan/prior
// choice needed.
func (dohSection) carryBestEffort(dst *settingResourceModel, plan, prior settingResourceModel) diag.Diagnostics {
	dst.Doh = plan.Doh
	return nil
}

func (dohSection) isConfigured(m settingResourceModel) bool {
	return !m.Doh.IsNull() && !m.Doh.IsUnknown()
}
