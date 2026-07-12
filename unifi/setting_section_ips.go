package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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

// ipsSection is the settingSection implementation for the "ips" (Intrusion
// Prevention System / IDS/IPS and threat management) settings section. It is
// the deepest section in the resource: it drives the generalized nested
// codec (decodeObjectList/overlayObjectList, Task 16b) for three object
// lists (honeypot, suppression_alerts, suppression_whitelist), one of which
// (suppression_alerts) has a further-nested object list of its own
// (tracking) that the codec recurses into automatically.
//
// It also has a wire wrapper: the Terraform schema flattens
// suppression_alerts/suppression_whitelist to top-level attributes, but the
// controller wire nests them under a "suppression": {"alerts": [...],
// "whitelist": [...]} object. Every other ips field maps 1:1 (wire key ==
// schema name). The section glues this wrapper by hand in decode/overlay:
// it unwraps/rewraps the "suppression" submap itself and calls
// decodeObjectList/overlayObjectList with the inner wire key ("alerts"/
// "whitelist") directly.
//
// TODO(go-unifi): the "suppression" nesting is unwrapped by hand from the
// raw map rather than via settings.Ips.Suppression
// (*SettingIpsSuppression, already correctly nested in go-unifi). PERMANENT:
// the nested object is the controller's own wire shape, not a go-unifi
// modeling gap — the Terraform-schema flattening (a deliberate UX choice)
// would require this glue against the typed struct too, and raw map access
// is required regardless for this section's unmodeled-field RMW (dataCopy's
// TODO in setting_snapshot.go).
//
// key() and attrName() both return "ips": there is no top-level rename, only
// the suppression wrapper's internal remap.
type ipsSection struct{}

func init() {
	registerSection(ipsSection{})
}

func (ipsSection) key() string      { return "ips" }
func (ipsSection) attrName() string { return "ips" }

// schemaAttribute is byte-identical to the inline "ips" block in
// setting_resource.go's schema (Task 24a enforces this with a
// schema-equivalence golden).
func (ipsSection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Intrusion Prevention System (IPS/IDS) and threat management settings. Basic IDS/IPS uses the built-in Emerging Threats ruleset and is free. A UniFi CyberSecure subscription adds enhanced threat intelligence from Proofpoint and Cloudflare on top of the base ruleset.",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"ips_mode": schema.StringAttribute{
				MarkdownDescription: "IPS operating mode: ids (detect only), ips (detect and block), ipsInline, or disabled.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("ids", "ips", "ipsInline", "disabled"),
				},
			},
			"enabled_categories": schema.ListAttribute{
				MarkdownDescription: "Emerging Threats ruleset categories to enable (e.g. \"emerging-malware\", \"tor\", \"phishing\").",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"enabled_networks": schema.ListAttribute{
				MarkdownDescription: "Network IDs to apply IPS inspection to.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"honeypot_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable honeypot to detect internal port scans.",
				Optional:            true,
				Computed:            true,
			},
			"honeypot": schema.ListNestedAttribute{
				MarkdownDescription: "Honeypot IP addresses per network.",
				Optional:            true,
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"ip_address": schema.StringAttribute{
							MarkdownDescription: "IP address to use as a honeypot.",
							Required:            true,
						},
						"network_id": schema.StringAttribute{
							MarkdownDescription: "Network ID this honeypot IP belongs to.",
							Required:            true,
						},
						"version": schema.StringAttribute{
							MarkdownDescription: "IP version: v4 or v6.",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.OneOf("v4", "v6"),
							},
						},
					},
				},
			},
			"restrict_torrents": schema.BoolAttribute{
				MarkdownDescription: "Block BitTorrent traffic.",
				Optional:            true,
				Computed:            true,
			},
			"content_filtering_blocking_page_enabled": schema.BoolAttribute{
				MarkdownDescription: "Show a blocking page when content filtering blocks a request.",
				Optional:            true,
				Computed:            true,
			},
			"memory_optimized": schema.BoolAttribute{
				MarkdownDescription: "Use memory-optimized IPS ruleset (reduced rule set for low-memory devices).",
				Optional:            true,
				Computed:            true,
			},
			"advanced_filtering_preference": schema.StringAttribute{
				MarkdownDescription: "Advanced filtering mode: manual or disabled.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("manual", "disabled"),
				},
			},
			"suppression_alerts": schema.ListNestedAttribute{
				MarkdownDescription: "IPS signature alert suppression entries — silence specific signatures or categories.",
				Optional:            true,
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"category": schema.StringAttribute{
							MarkdownDescription: "Alert suppression signature category.",
							Optional:            true,
							Computed:            true,
						},
						"gid": schema.Int64Attribute{
							MarkdownDescription: "Signature Generator ID (GID).",
							Optional:            true,
							Computed:            true,
						},
						"id": schema.Int64Attribute{
							MarkdownDescription: "Signature ID.",
							Optional:            true,
							Computed:            true,
						},
						"signature": schema.StringAttribute{
							MarkdownDescription: "Suppression signature name.",
							Optional:            true,
							Computed:            true,
						},
						"type": schema.StringAttribute{
							MarkdownDescription: "Suppression type: `all` (everywhere) or `track` (only the tracked sources/destinations).",
							Optional:            true,
							Computed:            true,
							Validators: []validator.String{
								stringvalidator.OneOf("all", "track"),
							},
						},
						"tracking": schema.ListNestedAttribute{
							MarkdownDescription: "Tracking specifications (used when `type` is `track`).",
							Optional:            true,
							Computed:            true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"direction": schema.StringAttribute{
										MarkdownDescription: "Match direction: both, src, or dest.",
										Required:            true,
										Validators: []validator.String{
											stringvalidator.OneOf("both", "src", "dest"),
										},
									},
									"mode": schema.StringAttribute{
										MarkdownDescription: "Match mode: ip, subnet, or network.",
										Required:            true,
										Validators: []validator.String{
											stringvalidator.OneOf(
												"ip",
												"subnet",
												"network",
											),
										},
									},
									"value": schema.StringAttribute{
										MarkdownDescription: "IP address, CIDR subnet, or network ID to match.",
										Required:            true,
									},
								},
							},
						},
					},
				},
			},
			"suppression_whitelist": schema.ListNestedAttribute{
				MarkdownDescription: "IPS suppression whitelist entries — sources/destinations to exclude from inspection.",
				Optional:            true,
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"direction": schema.StringAttribute{
							MarkdownDescription: "Match direction: both, src, or dest.",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.OneOf("both", "src", "dest"),
							},
						},
						"mode": schema.StringAttribute{
							MarkdownDescription: "Match mode: ip, subnet, or network.",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.OneOf("ip", "subnet", "network"),
							},
						},
						"value": schema.StringAttribute{
							MarkdownDescription: "IP address, CIDR subnet, or network ID to whitelist.",
							Required:            true,
						},
					},
				},
			},
		},
	}
}

var (
	ipsHoneypotElemType  = types.ObjectType{AttrTypes: ipsHoneypotAttrTypes}
	ipsWhitelistElemType = types.ObjectType{AttrTypes: ipsWhitelistAttrTypes}
	ipsAlertElemType     = types.ObjectType{AttrTypes: ipsAlertAttrTypes}
)

// decode populates model.Ips from snap's "ips" section data.
// suppression_alerts/suppression_whitelist are unwrapped from the wire's
// "suppression" object by hand (the only remap in this section); every
// other field, including honeypot and the alert's nested tracking list, maps
// 1:1 through the generalized nested codec (decodeObjectList).
func (s ipsSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	var priorModel settingIpsModel
	if !prior.Ips.IsNull() && !prior.Ips.IsUnknown() {
		diags.Append(prior.Ips.As(ctx, &priorModel, basetypes.ObjectAsOptions{})...)
	}

	sec, _ := snap.section(s.key())
	data := sec.Data

	ipsMode, d := decodeString(data, "ips_mode", priorModel.IPSMode)
	diags.Append(d...)
	advancedFilteringPreference, d := decodeString(data, "advanced_filtering_preference", priorModel.AdvancedFilteringPreference)
	diags.Append(d...)
	honeypotEnabled, d := decodeBool(data, "honeypot_enabled", priorModel.HoneypotEnabled)
	diags.Append(d...)
	restrictTorrents, d := decodeBool(data, "restrict_torrents", priorModel.RestrictTorrents)
	diags.Append(d...)
	contentFilteringBlockingPageEnabled, d := decodeBool(data, "content_filtering_blocking_page_enabled", priorModel.ContentFilteringBlockingPageEnabled)
	diags.Append(d...)
	memoryOptimized, d := decodeBool(data, "memory_optimized", priorModel.MemoryOptimized)
	diags.Append(d...)
	enabledCategories, d := decodeStringList(ctx, data, "enabled_categories", priorModel.EnabledCategories)
	diags.Append(d...)
	enabledNetworks, d := decodeStringList(ctx, data, "enabled_networks", priorModel.EnabledNetworks)
	diags.Append(d...)
	honeypot, d := decodeObjectList(ctx, data, "honeypot", priorModel.Honeypot, ipsHoneypotElemType)
	diags.Append(d...)

	// ⚠️ Wire wrapper: the controller nests suppression_alerts/
	// suppression_whitelist under a "suppression" object. A nil/absent
	// suppression map is safe here: decodeObjectList reads a nil map's
	// missing key as absent and returns ListNull.
	suppression, _ := data["suppression"].(map[string]any)
	alerts, d := decodeObjectList(ctx, suppression, "alerts", priorModel.SuppressionAlerts, ipsAlertElemType)
	diags.Append(d...)
	whitelist, d := decodeObjectList(ctx, suppression, "whitelist", priorModel.SuppressionWhitelist, ipsWhitelistElemType)
	diags.Append(d...)

	if diags.HasError() {
		return diags
	}

	m := settingIpsModel{
		AdvancedFilteringPreference:         advancedFilteringPreference,
		ContentFilteringBlockingPageEnabled: contentFilteringBlockingPageEnabled,
		EnabledCategories:                   enabledCategories,
		EnabledNetworks:                     enabledNetworks,
		Honeypot:                            honeypot,
		HoneypotEnabled:                     honeypotEnabled,
		IPSMode:                             ipsMode,
		MemoryOptimized:                     memoryOptimized,
		RestrictTorrents:                    restrictTorrents,
		SuppressionWhitelist:                whitelist,
		SuppressionAlerts:                   alerts,
	}

	obj, objDiags := types.ObjectValueFrom(ctx, ipsAttrTypes, m)
	diags.Append(objDiags...)
	if diags.HasError() {
		return diags
	}

	model.Ips = obj
	return diags
}

// overlay computes the "ips" PUT body from model.Ips, starting from a deep
// copy of the snapshot's current section data so any unmodeled key already
// present on the controller is preserved. Returns configured == false (no
// write) when the section is not configured (null/unknown) in model.
//
// ⚠️ Wire wrapper: suppression_alerts/suppression_whitelist are written into
// a "suppression" submap, seeded from the base's existing "suppression" (if
// any) so an existing wrapper's unmodeled keys are preserved. The submap is
// only written back onto base when it already existed or the overlay added
// content - this avoids introducing an empty "suppression": {} when neither
// the base nor the config has either list (matching the legacy converter,
// which lazily creates *SettingIpsSuppression only when a list is set).
// overlayObjectList is a no-op for a null/unknown config list, so a null
// SuppressionAlerts+SuppressionWhitelist leaves an existing sup map (if any)
// untouched, and len(sup) > 0 for a preserved-but-unmodeled wrapper.
func (s ipsSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.Ips.IsNull() || model.Ips.IsUnknown() {
		return settings.RawSetting{}, false, diags
	}

	var m settingIpsModel
	diags.Append(model.Ips.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	base := snap.dataCopy(s.key())
	overlayString(base, "ips_mode", m.IPSMode)
	overlayString(base, "advanced_filtering_preference", m.AdvancedFilteringPreference)
	overlayBool(base, "honeypot_enabled", m.HoneypotEnabled)
	overlayBool(base, "restrict_torrents", m.RestrictTorrents)
	overlayBool(base, "content_filtering_blocking_page_enabled", m.ContentFilteringBlockingPageEnabled)
	overlayBool(base, "memory_optimized", m.MemoryOptimized)
	diags.Append(overlayStringList(ctx, base, "enabled_categories", m.EnabledCategories)...)
	diags.Append(overlayStringList(ctx, base, "enabled_networks", m.EnabledNetworks)...)
	diags.Append(overlayObjectList(ctx, base, "honeypot", m.Honeypot)...)

	// ⚠️ Wire wrapper: glue suppression_alerts/suppression_whitelist into
	// the "suppression" submap by hand.
	sup, had := base["suppression"].(map[string]any)
	if !had {
		sup = map[string]any{}
	}
	diags.Append(overlayObjectList(ctx, sup, "whitelist", m.SuppressionWhitelist)...)
	diags.Append(overlayObjectList(ctx, sup, "alerts", m.SuppressionAlerts)...)
	if had || len(sup) > 0 {
		base["suppression"] = sup
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

func (s ipsSection) capability(snap rawSettings) capabilityState {
	return sectionCapability(snap, s.key())
}

// carryBestEffort copies the plan's ips value onto dst. This section holds
// no secret leaves, so it is a straight copy with no per-leaf plan/prior
// choice needed.
func (ipsSection) carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics {
	dst.Ips = plan.Ips
	return nil
}

func (ipsSection) isConfigured(m settingResourceModel) bool {
	return !m.Ips.IsNull() && !m.Ips.IsUnknown()
}
