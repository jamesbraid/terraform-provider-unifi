package unifi

import (
	"context"
	"fmt"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// mdnsPredefinedServiceCodes is the closed set of predefined_services codes
// per settings.SettingMdnsPredefinedServices.Code's struct comment
// (mdns.generated.go): 24 values.
var mdnsPredefinedServiceCodes = []string{
	"amazon_devices", "android_tv_remote", "apple_airDrop", "apple_airPlay",
	"apple_file_sharing", "apple_iChat", "apple_iTunes", "aqara", "bose",
	"dns_service_discovery", "ftp_servers", "google_chromecast", "homeKit",
	"matter_network", "philips_hue", "printers", "roku", "scanners", "sonos",
	"spotify_connect", "ssh_servers", "time_capsule", "web_servers",
	"windows_file_sharing_samba",
}

// mdnsCustomServiceAddressPattern matches settings.SettingMdnsCustomServices.
// Address's struct comment (mdns.generated.go): an mDNS service-type string.
var mdnsCustomServiceAddressPattern = regexp.MustCompile(`^_[a-zA-Z0-9._-]+\._(tcp|udp)(\.local)?$`)

// mdnsSection is the settingSection implementation for the "mdns" settings
// section: a mode discriminator (all|auto|custom) gates whether
// predefined_services/custom_services are live, user-authoritative config or
// normalized to empty. See the design spec's "The discriminator (C4)"
// section for the full contract:
//
//  1. Contradictory config (predefined_services/custom_services configured
//     while mode != "custom") is rejected at plan time by mdnsObjectValidator.
//  2. Stale prior-state children are cleared to an explicit empty list by
//     mdnsStaleChildrenPlanModifier when mode is changing away from
//     "custom", before the validator above runs.
//  3. overlay() never sends predefined_services/custom_services from
//     model when mode != "custom", regardless of what's configured
//     (belt-and-suspenders past the schema layer).
//  4. decode() reads predefined_services/custom_services from the wire only
//     when mode == "custom"; otherwise it normalizes state to an empty
//     (not null) list even if the controller's snapshot still holds a
//     stale array from a prior "custom" period.
//
// predefined_services' wire shape is []{code: string} — a single-field
// object list — but is modeled here as a flat List[String] of codes for
// ergonomic HCL (["apple_airPlay", ...] rather than [{code =
// "apple_airPlay"}, ...]); this is a flagged modeling decision (see the
// design spec), hand-glued at the codec boundary below rather than going
// through decodeObjectList/overlayObjectList (which assume a multi-field
// object worth flattening, not a single-field wrapper). custom_services is
// a genuine 2-field object (address, name) and uses decodeObjectList/
// overlayObjectList directly.
type mdnsSection struct{}

func init() {
	registerSection(mdnsSection{})
}

func (mdnsSection) key() string      { return "mdns" }
func (mdnsSection) attrName() string { return "mdns" }

// schemaAttribute returns the "mdns" SingleNestedAttribute: mode is a
// discriminator (stringvalidator.OneOf); predefined_services/
// custom_services are only authoritative when mode == "custom" — enforced
// by mdnsObjectValidator (contradictory config) and
// mdnsStaleChildrenPlanModifier (stale-state clearing on transition).
func (mdnsSection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "mDNS (multicast DNS / Bonjour) settings. `mode` is a discriminator: " +
			"`predefined_services`/`custom_services` are only live/authoritative when `mode = \"custom\"` " +
			"— configuring either while `mode` is `\"all\"` or `\"auto\"` is rejected at plan time, and " +
			"both are normalized to an empty list on read/write when `mode` is not `\"custom\"`.",
		Optional: true,
		Computed: true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
			mdnsStaleChildrenPlanModifier{},
		},
		Validators: []validator.Object{
			mdnsObjectValidator{},
		},
		Attributes: map[string]schema.Attribute{
			"mode": schema.StringAttribute{
				MarkdownDescription: "Service discovery mode: `all` (broadcast every predefined service), " +
					"`auto` (controller-driven discovery), or `custom` (only `predefined_services`/" +
					"`custom_services` are broadcast).",
				Optional: true,
				Computed: true,
				Validators: []validator.String{
					stringvalidator.OneOf("all", "auto", "custom"),
				},
			},
			"predefined_services": schema.ListAttribute{
				MarkdownDescription: "Predefined service codes to broadcast. Only authoritative when " +
					"`mode = \"custom\"`.",
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.List{
					listvalidator.ValueStringsAre(stringvalidator.OneOf(mdnsPredefinedServiceCodes...)),
				},
			},
			"custom_services": schema.ListNestedAttribute{
				MarkdownDescription: "Custom mDNS services to broadcast. Only authoritative when " +
					"`mode = \"custom\"`.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"address": schema.StringAttribute{
							MarkdownDescription: "mDNS service type, e.g. `_myservice._tcp` or " +
								"`_myservice._tcp.local`.",
							Required: true,
							Validators: []validator.String{
								stringvalidator.RegexMatches(
									mdnsCustomServiceAddressPattern,
									"must be an mDNS service type matching ^_[a-zA-Z0-9._-]+\\._(tcp|udp)(\\.local)?$",
								),
							},
						},
						"name": schema.StringAttribute{
							MarkdownDescription: "Display name for the custom service.",
							Required:            true,
						},
					},
				},
			},
		},
	}
}

// decode populates model.Mdns from snap's "mdns" section data. mode is
// always read normally; predefined_services/custom_services are read from
// the wire only when mode == "custom" — otherwise state is normalized to an
// empty (not null) list, even if the controller's snapshot still holds a
// stale array from a prior "custom" period (see the section doc comment,
// rule 4).
func (s mdnsSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	var priorModel settingMdnsModel
	if !prior.Mdns.IsNull() && !prior.Mdns.IsUnknown() {
		diags.Append(prior.Mdns.As(ctx, &priorModel, basetypes.ObjectAsOptions{})...)
	}

	sec, _ := snap.section(s.key())
	data := sec.Data

	mode, d := decodeString(data, "mode", priorModel.Mode)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	var predefined, custom types.List
	if mode.ValueString() == "custom" {
		predefined, d = decodeMdnsPredefinedServices(data, "predefined_services", priorModel.PredefinedServices)
		diags.Append(d...)
		custom, d = decodeObjectList(ctx, data, "custom_services", priorModel.CustomServices, types.ObjectType{AttrTypes: mdnsCustomServiceAttrTypes})
		diags.Append(d...)
	} else {
		predefined = types.ListValueMust(types.StringType, []attr.Value{})
		custom = types.ListValueMust(types.ObjectType{AttrTypes: mdnsCustomServiceAttrTypes}, []attr.Value{})
	}
	if diags.HasError() {
		return diags
	}

	m := settingMdnsModel{
		Mode:               mode,
		PredefinedServices: predefined,
		CustomServices:     custom,
	}

	obj, objDiags := types.ObjectValueFrom(ctx, mdnsAttrTypes, m)
	diags.Append(objDiags...)
	if diags.HasError() {
		return diags
	}

	model.Mdns = obj
	return diags
}

// overlay computes the "mdns" PUT body from model.Mdns, starting from a deep
// copy of the snapshot's current section data so any unmodeled key is
// preserved (RMW). When mode == "custom", predefined_services/
// custom_services are written from the model. Otherwise both are written as
// an explicit empty array regardless of what the model holds — the
// codec-layer normalization that is the last line of defense for rule 3 in
// the section doc comment (belt-and-suspenders past the schema-level
// validator/plan-modifier). Returns configured == false (no write) when the
// section is not configured (null/unknown) in model.
func (s mdnsSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.Mdns.IsNull() || model.Mdns.IsUnknown() {
		return settings.RawSetting{}, false, diags
	}

	var m settingMdnsModel
	diags.Append(model.Mdns.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	base := snap.dataCopy(s.key())
	overlayString(base, "mode", m.Mode)

	if m.Mode.ValueString() == "custom" {
		diags.Append(overlayMdnsPredefinedServices(base, "predefined_services", m.PredefinedServices)...)
		diags.Append(overlayObjectList(ctx, base, "custom_services", m.CustomServices)...)
	} else {
		base["predefined_services"] = []any{}
		base["custom_services"] = []any{}
	}
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: s.key()},
		Data:        base,
	}
	return rs, true, diags
}

// carryBestEffort copies the plan's mdns value onto dst. This section holds
// no secret leaves, so it is a straight copy with no per-leaf plan/prior
// choice needed.
func (mdnsSection) carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics {
	dst.Mdns = plan.Mdns
	return nil
}

// isConfigured reports whether m.Mdns is set (non-null, non-unknown),
// gating whether Create/Update push this section to the controller at all.
func (mdnsSection) isConfigured(m settingResourceModel) bool {
	return !m.Mdns.IsNull() && !m.Mdns.IsUnknown()
}

// decodeMdnsPredefinedServices reads mdns' predefined_services wire field
// (an array of {code: string} objects) directly into a flat List[String] of
// codes — hand-glued rather than routed through decodeObjectList, since the
// wire's single-field object wrapper isn't worth flattening generically (see
// the section doc comment's flagged modeling decision). Absent/null decodes
// to a null list; present-empty decodes to an empty (non-null) list; an
// element missing/malformed "code" is remote type drift, warned and
// retaining prior wholesale (never partially decoded), matching every other
// codec function's drift contract in this file/package.
func decodeMdnsPredefinedServices(data map[string]any, key string, prior types.List) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics

	raw, ok := data[key]
	if !ok || raw == nil {
		return types.ListNull(types.StringType), diags
	}
	items, ok := raw.([]any)
	if !ok {
		diags.AddWarning(
			"Settings value type drift",
			fmt.Sprintf("field %q: expected array, got %T; retaining last-known value", key, raw),
		)
		return prior, diags
	}

	elems := make([]attr.Value, 0, len(items))
	for i, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			diags.AddWarning(
				"Settings value type drift",
				fmt.Sprintf("field %q: element %d: expected object, got %T; retaining last-known value", key, i, item),
			)
			return prior, diags
		}
		code, ok := obj["code"].(string)
		if !ok {
			diags.AddWarning(
				"Settings value type drift",
				fmt.Sprintf("field %q: element %d: expected string \"code\", got %T; retaining last-known value", key, i, obj["code"]),
			)
			return prior, diags
		}
		elems = append(elems, types.StringValue(code))
	}

	list, listDiags := types.ListValue(types.StringType, elems)
	diags.Append(listDiags...)
	return list, diags
}

// overlayMdnsPredefinedServices writes v (a flat List[String] of codes) into
// out[key] as an array of {code: string} objects, the inverse of
// decodeMdnsPredefinedServices. A null/unknown v is a no-op: the snapshot's
// existing value in out is left untouched.
func overlayMdnsPredefinedServices(out map[string]any, key string, v types.List) diag.Diagnostics {
	var diags diag.Diagnostics

	if v.IsNull() || v.IsUnknown() {
		return diags
	}

	elems := v.Elements()
	items := make([]any, 0, len(elems))
	for _, e := range elems {
		s, ok := e.(types.String)
		if !ok {
			diags.AddError(
				"Malformed settings value",
				fmt.Sprintf("field %q: element is not a string value: %T", key, e),
			)
			continue
		}
		items = append(items, map[string]any{"code": s.ValueString()})
	}
	out[key] = items
	return diags
}
