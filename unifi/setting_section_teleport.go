package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/validators"
)

// teleportSubnetCidrValidator validates subnet_cidr as either an empty
// string or a valid CIDR notation. The wire regex (teleport.generated.go,
// settings.Teleport.SubnetCidr's struct comment) is
// "^(...)\/([8-9]|[1-2][0-9]|3[0-2])$|^$" — the trailing "|^$" alternative
// explicitly allows an empty string regardless of enabled's value. A bare
// validators.CIDRValidator() reuse would reject "" (its ValidateString only
// skips on IsNull()/IsUnknown(); net.ParseCIDR("") errors), so this section
// needs its own empty-tolerant wrapper rather than reusing CIDRValidator()
// unmodified — CIDRValidator() itself must stay strict for its other
// callers (e.g. static_route_resource.go), where empty is not wire-legal.
type teleportSubnetCidrValidator struct {
	delegate validator.String
}

func newTeleportSubnetCidrValidator() teleportSubnetCidrValidator {
	return teleportSubnetCidrValidator{delegate: validators.CIDRValidator()}
}

func (teleportSubnetCidrValidator) Description(ctx context.Context) string {
	return `value must be empty or a valid CIDR notation`
}

func (v teleportSubnetCidrValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v teleportSubnetCidrValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if req.ConfigValue.ValueString() == "" {
		return
	}
	v.delegate.ValidateString(ctx, req, resp)
}

// teleportSection is the settingSection implementation for the "teleport"
// settings section: two scalar leaves (enabled, subnet_cidr) with a weak
// coupling the design spec deliberately does NOT enforce at the schema
// level (no AlsoRequires/ConflictsWith) — the wire format's own tolerance
// for an empty subnet_cidr regardless of enabled says the controller does
// not hard-require the pairing. subnet_cidr's format validator is
// teleportSubnetCidrValidator (empty-or-CIDR), not a bare
// validators.CIDRValidator() reuse — see that type's doc comment.
type teleportSection struct{}

func init() {
	registerSection(teleportSection{})
}

func (teleportSection) key() string      { return "teleport" }
func (teleportSection) attrName() string { return "teleport" }

// schemaAttribute returns the "teleport" SingleNestedAttribute. No soft
// warning validator is added for "subnet_cidr configured while enabled =
// false" — the design spec's default is no validator here (preserve-by-
// default philosophy: don't invent validation the controller doesn't
// enforce), documented here rather than silently omitted.
func (teleportSection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Teleport (personal VPN) settings.",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether Teleport is enabled.",
				Optional:            true,
				Computed:            true,
			},
			"subnet_cidr": schema.StringAttribute{
				MarkdownDescription: "Teleport subnet, in CIDR notation (/8-/32), or an empty " +
					"string to clear it. Not required to be set when `enabled = true`, and not " +
					"required to be empty when `enabled = false` — the controller's own wire format " +
					"tolerates any combination, so no cross-field validation is enforced here " +
					"(a deliberate decision, not an oversight).",
				Optional: true,
				Computed: true,
				Validators: []validator.String{
					newTeleportSubnetCidrValidator(),
				},
			},
		},
	}
}

// decode populates model.Teleport from snap's "teleport" section data. Plain
// decodeBool/decodeString, no coupling logic — reading state never needs to
// reconcile enabled/subnet_cidr.
func (s teleportSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	var priorModel settingTeleportModel
	if !prior.Teleport.IsNull() && !prior.Teleport.IsUnknown() {
		diags.Append(prior.Teleport.As(ctx, &priorModel, basetypes.ObjectAsOptions{})...)
	}

	sec, _ := snap.section(s.key())
	data := sec.Data

	enabled, d := decodeBool(data, "enabled", priorModel.Enabled)
	diags.Append(d...)
	subnetCidr, d := decodeString(data, "subnet_cidr", priorModel.SubnetCidr)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	m := settingTeleportModel{
		Enabled:    enabled,
		SubnetCidr: subnetCidr,
	}

	obj, objDiags := types.ObjectValueFrom(ctx, teleportAttrTypes, m)
	diags.Append(objDiags...)
	if diags.HasError() {
		return diags
	}

	model.Teleport = obj
	return diags
}

// overlay computes the "teleport" PUT body from model.Teleport, starting
// from a deep copy of the snapshot's current section data so any unmodeled
// key is preserved (RMW). Ordinary Managed-class fields, no special-casing:
// cfg-null on either omits it from the PUT; cfg-empty on subnet_cidr sends
// "" (an explicit clear, wire-blessed via the |^$ regex alternative).
// Returns configured == false (no write) when the section is not configured
// (null/unknown) in model.
func (s teleportSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.Teleport.IsNull() || model.Teleport.IsUnknown() {
		return settings.RawSetting{}, false, diags
	}

	var m settingTeleportModel
	diags.Append(model.Teleport.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	base := snap.dataCopy(s.key())
	overlayBool(base, "enabled", m.Enabled)
	overlayString(base, "subnet_cidr", m.SubnetCidr)

	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: s.key()},
		Data:        base,
	}
	return rs, true, diags
}

// carryBestEffort copies the plan's teleport value onto dst. This section
// holds no secret leaves, so it is a straight copy with no per-leaf
// plan/prior choice needed.
func (teleportSection) carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics {
	dst.Teleport = plan.Teleport
	return nil
}

// isConfigured reports whether m.Teleport is set (non-null, non-unknown),
// gating whether Create/Update push this section to the controller at all.
func (teleportSection) isConfigured(m settingResourceModel) bool {
	return !m.Teleport.IsNull() && !m.Teleport.IsUnknown()
}
