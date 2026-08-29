package unifi

// The ips section descriptor: replaces the hand-written writeIpsSection /
// readIpsSection (setting_sections.go) and their ipsModelToSetting /
// ipsSettingToModel / ipsSuppressionModelToSetting / writeIpsSuppression /
// ipsSuppressionConfigured helpers (deleted from setting_resource.go). The
// model types and attribute-type maps moved here too, from
// setting_resource.go: descriptor_mapping_test.go's loadDescriptors reads a
// descriptor's model tags from the same file the Spec literal is in. See
// setting_usg_descriptor.go for the shape a section with a second document
// follows -- ips is the second one, after usg/usg_geo.
//
// ips_suppression is simpler than usg_geo in one respect: alerts and
// whitelist are settings.IpsSuppression's own top-level json tags (not
// nested one level under a wrapper the way usg_geo's four attributes sit
// under ip_filtering), so ipsSuppressionKitBackend needs no
// read-modify-write bridge -- it reads and writes settings.IpsSuppression
// directly, the same shape ipsKitBackend and every other section's own
// backend already uses.
//
// It differs in another respect that DOES need a deliberate choice:
// readIpsSection tolerated a not-found ips_suppression by passing a nil
// *settings.IpsSuppression into ipsSettingToModel, which then read every
// list as empty (not null) whenever the plan configured it -- suppression
// lists round-trip as "empty" on a controller that has never held the
// setting, not "left however state last had them". usgGeoKitDocument's own
// OnReadNotFound is nil, which works for usg_geo because a document-level
// not-found there is supposed to leave the model untouched (see task 5b's
// report and its "Fifth divergence" -- geo_ip_filtering_enabled reads back
// null instead of a concrete false, precisely because ToModel never runs).
// Reproducing readIpsSection's own tolerance instead requires ToModel to
// keep running on a not-found, which OnReadNotFound cannot arrange (it has
// no access to model). So ipsSuppressionKitBackend.Read absorbs the
// not-found itself, returning a zero &settings.IpsSuppression{} rather than
// propagating the error -- Spec.ToModel then runs normally, and
// ObjectListField's own KeepZero elision decodes the zero (nil) Alerts/
// Whitelist slices as empty, not null, lists. ipsAfterReceive is what turns
// that into null for an attribute the plan never configured, same as every
// other list here.

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// settingIpsHoneypotModel is one element of ips's honeypot list.
type settingIpsHoneypotModel struct {
	IPAddress types.String `tfsdk:"ip_address"`
	NetworkID types.String `tfsdk:"network_id"`
	Version   types.String `tfsdk:"version"`
}

// settingIpsWhitelistModel is one element of ips's suppression_whitelist
// list, owned by ipsSuppressionKitSpec. Its shape (direction/mode/value) is
// identical to settingIpsTrackingModel's own -- schema_model_agreement_test.go
// declares both "unifi_setting.ips.suppression_alerts.tracking" and
// "unifi_setting.ips.suppression_whitelist" ambiguous between the two for
// exactly this reason. TestIpsSuppressionAttributesMapToNamedSDKMembers is
// what actually resolves which wire each one carries.
type settingIpsWhitelistModel struct {
	Direction types.String `tfsdk:"direction"`
	Mode      types.String `tfsdk:"mode"`
	Value     types.String `tfsdk:"value"`
}

// settingIpsTrackingModel is one element of an alert's own tracking list --
// nested two levels deep (ips.suppression_alerts[].tracking[]), which no
// Field kind reaches directly; ipsAlertEncode/ipsAlertDecode convert it by
// hand, the same way settingIpsAlertModel's other members are.
type settingIpsTrackingModel struct {
	Direction types.String `tfsdk:"direction"`
	Mode      types.String `tfsdk:"mode"`
	Value     types.String `tfsdk:"value"`
}

// settingIpsAlertModel is one element of ips's suppression_alerts list,
// owned by ipsSuppressionKitSpec.
type settingIpsAlertModel struct {
	Category  types.String `tfsdk:"category"`
	Gid       types.Int64  `tfsdk:"gid"`
	ID        types.Int64  `tfsdk:"id"`
	Signature types.String `tfsdk:"signature"`
	Type      types.String `tfsdk:"type"`
	Tracking  types.List   `tfsdk:"tracking"`
}

// settingIpsModel is ips's own section model, decoded out of
// settingResourceModel.Ips. Its SuppressionWhitelist/SuppressionAlerts
// members are mapped by ipsSuppressionKitSpec, not ipsKitSpec -- see this
// file's own top comment.
type settingIpsModel struct {
	AdvancedFilteringPreference         types.String `tfsdk:"advanced_filtering_preference"`
	ContentFilteringBlockingPageEnabled types.Bool   `tfsdk:"content_filtering_blocking_page_enabled"`
	EnabledCategories                   types.List   `tfsdk:"enabled_categories"`
	EnabledNetworks                     types.List   `tfsdk:"enabled_networks"`
	Honeypot                            types.List   `tfsdk:"honeypot"`
	HoneypotEnabled                     types.Bool   `tfsdk:"honeypot_enabled"`
	IPSMode                             types.String `tfsdk:"ips_mode"`
	MemoryOptimized                     types.Bool   `tfsdk:"memory_optimized"`
	RestrictTorrents                    types.Bool   `tfsdk:"restrict_torrents"`
	SuppressionWhitelist                types.List   `tfsdk:"suppression_whitelist"`
	SuppressionAlerts                   types.List   `tfsdk:"suppression_alerts"`
}

// ipsHoneypotAttrTypes, ipsWhitelistAttrTypes, ipsTrackingAttrTypes,
// ipsAlertAttrTypes and ipsAttrTypes type ips's own nested lists and its own
// object in state; all must match the generated schema exactly.
var (
	ipsHoneypotAttrTypes = map[string]attr.Type{
		"ip_address": types.StringType,
		"network_id": types.StringType,
		"version":    types.StringType,
	}
	ipsWhitelistAttrTypes = map[string]attr.Type{
		"direction": types.StringType,
		"mode":      types.StringType,
		"value":     types.StringType,
	}
	ipsTrackingAttrTypes = map[string]attr.Type{
		"direction": types.StringType,
		"mode":      types.StringType,
		"value":     types.StringType,
	}
	ipsAlertAttrTypes = map[string]attr.Type{
		"category":  types.StringType,
		"gid":       types.Int64Type,
		"id":        types.Int64Type,
		"signature": types.StringType,
		"type":      types.StringType,
		"tracking":  types.ListType{ElemType: types.ObjectType{AttrTypes: ipsTrackingAttrTypes}},
	}
	ipsAttrTypes = map[string]attr.Type{
		"advanced_filtering_preference":           types.StringType,
		"content_filtering_blocking_page_enabled": types.BoolType,
		"enabled_categories":                      types.ListType{ElemType: types.StringType},
		"enabled_networks":                        types.ListType{ElemType: types.StringType},
		"honeypot": types.ListType{
			ElemType: types.ObjectType{AttrTypes: ipsHoneypotAttrTypes},
		},
		"honeypot_enabled":  types.BoolType,
		"ips_mode":          types.StringType,
		"memory_optimized":  types.BoolType,
		"restrict_torrents": types.BoolType,
		"suppression_whitelist": types.ListType{
			ElemType: types.ObjectType{AttrTypes: ipsWhitelistAttrTypes},
		},
		"suppression_alerts": types.ListType{
			ElemType: types.ObjectType{AttrTypes: ipsAlertAttrTypes},
		},
	}
)

// ipsKitSpec maps every non-suppression attribute of the generated ips
// schema (resource_setting/setting_resource_gen.go's "ips"
// SingleNestedAttribute) onto settings.Ips. Elide judgments follow
// resourcekit.ElideProblems' schema-driven rule: advanced_filtering_preference
// and ips_mode carry a stringvalidator.OneOf that rejects "", so they want
// NullZero; enabled_categories, enabled_networks and honeypot are
// Optional+Computed with no validator on the list attribute itself (only
// their elements carry any), so KeepZero is what the check demands, same as
// usg_geo's countries and doh's own lists. content_filtering_blocking_page_enabled,
// honeypot_enabled, memory_optimized and restrict_torrents are plain bools,
// which carry no Elide at all. suppression_alerts/suppression_whitelist
// aren't Fields here at all -- see ipsSuppressionKitSpec and this file's own
// top comment.
func ipsKitSpec() resourcekit.Spec[settingIpsModel, settings.Ips] {
	return resourcekit.Spec[settingIpsModel, settings.Ips]{
		TypeName: "setting_ips",
		Subject:  "IPS Setting",
		New:      func() *settings.Ips { return &settings.Ips{} },
		// setting_ips.mapping.json declares these two managed under this same
		// surface; ipsSuppressionKitSpec (no TypeName of its own) is the Spec
		// that actually carries and verifies them.
		MappedElsewhere: []string{"alerts", "whitelist"},
		Fields: []resourcekit.Field[settingIpsModel, settings.Ips]{
			resourcekit.StringField[settingIpsModel, settings.Ips]{
				Wire:  "advanced_filtering_preference",
				Model: func(m *settingIpsModel) *types.String { return &m.AdvancedFilteringPreference },
				SDK:   func(s *settings.Ips) *string { return &s.AdvancedFilteringPreference },
				Elide: resourcekit.NullZero,
			},
			resourcekit.BoolField[settingIpsModel, settings.Ips]{
				Wire:  "content_filtering_blocking_page_enabled",
				Model: func(m *settingIpsModel) *types.Bool { return &m.ContentFilteringBlockingPageEnabled },
				SDK:   func(s *settings.Ips) *bool { return &s.ContentFilteringBlockingPageEnabled },
			},
			resourcekit.StringListField[settingIpsModel, settings.Ips]{
				Wire:  "enabled_categories",
				Model: func(m *settingIpsModel) *types.List { return &m.EnabledCategories },
				SDK:   func(s *settings.Ips) *[]string { return &s.EnabledCategories },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringListField[settingIpsModel, settings.Ips]{
				Wire:  "enabled_networks",
				Model: func(m *settingIpsModel) *types.List { return &m.EnabledNetworks },
				SDK:   func(s *settings.Ips) *[]string { return &s.EnabledNetworks },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.ObjectListField[settingIpsModel, settings.Ips, settings.SettingIpsHoneypot]{
				Wire:      "honeypot",
				Model:     func(m *settingIpsModel) *types.List { return &m.Honeypot },
				SDK:       func(s *settings.Ips) *[]settings.SettingIpsHoneypot { return &s.Honeypot },
				AttrTypes: ipsHoneypotAttrTypes,
				Encode:    ipsHoneypotEncode,
				Decode:    ipsHoneypotDecode,
				Elide:     resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingIpsModel, settings.Ips]{
				Wire:  "honeypot_enabled",
				Model: func(m *settingIpsModel) *types.Bool { return &m.HoneypotEnabled },
				SDK:   func(s *settings.Ips) *bool { return &s.HoneypotEnabled },
			},
			resourcekit.StringField[settingIpsModel, settings.Ips]{
				Wire:  "ips_mode",
				Model: func(m *settingIpsModel) *types.String { return &m.IPSMode },
				SDK:   func(s *settings.Ips) *string { return &s.IPsMode },
				Elide: resourcekit.NullZero,
			},
			resourcekit.BoolField[settingIpsModel, settings.Ips]{
				Wire:  "memory_optimized",
				Model: func(m *settingIpsModel) *types.Bool { return &m.MemoryOptimized },
				SDK:   func(s *settings.Ips) *bool { return &s.MemoryOptimized },
			},
			resourcekit.BoolField[settingIpsModel, settings.Ips]{
				Wire:  "restrict_torrents",
				Model: func(m *settingIpsModel) *types.Bool { return &m.RestrictTorrents },
				SDK:   func(s *settings.Ips) *bool { return &s.RestrictTorrents },
			},
		},
	}
}

func ipsHoneypotEncode(
	ctx context.Context, object types.Object,
) (settings.SettingIpsHoneypot, diag.Diagnostics) {
	var model settingIpsHoneypotModel
	diags := object.As(ctx, &model, basetypes.ObjectAsOptions{})
	return settings.SettingIpsHoneypot{
		IPAddress: model.IPAddress.ValueString(),
		NetworkID: model.NetworkID.ValueString(),
		Version:   model.Version.ValueString(),
	}, diags
}

func ipsHoneypotDecode(
	ctx context.Context, element settings.SettingIpsHoneypot,
) (types.Object, diag.Diagnostics) {
	return types.ObjectValueFrom(ctx, ipsHoneypotAttrTypes, settingIpsHoneypotModel{
		IPAddress: types.StringValue(element.IPAddress),
		NetworkID: types.StringValue(element.NetworkID),
		Version:   types.StringValue(element.Version),
	})
}

// ipsWhitelistEncode/ipsWhitelistDecode serve ipsSuppressionKitSpec's own
// suppression_whitelist ObjectListField.
func ipsWhitelistEncode(
	ctx context.Context, object types.Object,
) (settings.SettingIpsSuppressionWhitelist, diag.Diagnostics) {
	var model settingIpsWhitelistModel
	diags := object.As(ctx, &model, basetypes.ObjectAsOptions{})
	return settings.SettingIpsSuppressionWhitelist{
		Direction: model.Direction.ValueString(),
		Mode:      model.Mode.ValueString(),
		Value:     model.Value.ValueString(),
	}, diags
}

func ipsWhitelistDecode(
	ctx context.Context, element settings.SettingIpsSuppressionWhitelist,
) (types.Object, diag.Diagnostics) {
	return types.ObjectValueFrom(ctx, ipsWhitelistAttrTypes, settingIpsWhitelistModel{
		Direction: types.StringValue(element.Direction),
		Mode:      types.StringValue(element.Mode),
		Value:     types.StringValue(element.Value),
	})
}

// ipsTrackingEncode/ipsTrackingDecode convert one tracking element; called
// by hand from ipsAlertEncode/ipsAlertDecode, since tracking sits nested two
// levels deep (suppression_alerts[].tracking[]) where no Field kind reaches.
func ipsTrackingEncode(
	ctx context.Context, object types.Object,
) (settings.SettingIpsSuppressionTracking, diag.Diagnostics) {
	var model settingIpsTrackingModel
	diags := object.As(ctx, &model, basetypes.ObjectAsOptions{})
	return settings.SettingIpsSuppressionTracking{
		Direction: model.Direction.ValueString(),
		Mode:      model.Mode.ValueString(),
		Value:     model.Value.ValueString(),
	}, diags
}

func ipsTrackingDecode(
	ctx context.Context, element settings.SettingIpsSuppressionTracking,
) (types.Object, diag.Diagnostics) {
	return types.ObjectValueFrom(ctx, ipsTrackingAttrTypes, settingIpsTrackingModel{
		Direction: types.StringValue(element.Direction),
		Mode:      types.StringValue(element.Mode),
		Value:     types.StringValue(element.Value),
	})
}

// ipsAlertEncode/ipsAlertDecode serve ipsSuppressionKitSpec's own
// suppression_alerts ObjectListField. category/signature/type are read back
// through util.StringValueOrNull, matching the deleted ipsSettingToModel's
// own treatment of these three (type's OneOf("all","track") would want the
// same NullZero a top-level Field's Elide gives it, but nothing here reaches
// that instrument since the alert is one element of a list nested inside
// another Field's own Encode/Decode -- reproducing the deleted mapper
// exactly is the safest choice available, the same reasoning
// dnsVerificationDecode's simpler case didn't need). gid/id carry the #303
// omit-not-zero guard by hand, for the same reason lcm's brightness and
// syslog's port/netconsole_port carry Int64PtrField{OmitZero: true}: an
// Unknown value's ValueInt64Pointer() resolves to a pointer to zero, which
// would reach the wire as a literal 0 instead of "not configured". Unlike
// those two, gid/id are Optional+Computed with no min/max validator
// (confirmed against the generated schema), so a configured zero is a value
// this preserves rather than omits -- the guard only ever skips null/unknown,
// matching the deleted ipsSuppressionModelToSetting's own guard exactly, not
// Int64PtrField's OmitZero (which additionally skips a known zero).
func ipsAlertEncode(
	ctx context.Context, object types.Object,
) (settings.SettingIpsSuppressionAlerts, diag.Diagnostics) {
	var model settingIpsAlertModel
	diags := object.As(ctx, &model, basetypes.ObjectAsOptions{})
	alert := settings.SettingIpsSuppressionAlerts{
		Category:  model.Category.ValueString(),
		Signature: model.Signature.ValueString(),
		Type:      model.Type.ValueString(),
	}
	if !model.Gid.IsNull() && !model.Gid.IsUnknown() {
		gid := model.Gid.ValueInt64()
		alert.Gid = &gid
	}
	if !model.ID.IsNull() && !model.ID.IsUnknown() {
		id := model.ID.ValueInt64()
		alert.ID = &id
	}
	if !model.Tracking.IsNull() && !model.Tracking.IsUnknown() {
		for _, element := range model.Tracking.Elements() {
			elementObject, ok := element.(types.Object)
			if !ok {
				diags.AddError("Converting tracking",
					"a tracking list element is not an object, so it cannot be encoded")
				continue
			}
			tracking, d := ipsTrackingEncode(ctx, elementObject)
			diags.Append(d...)
			alert.Tracking = append(alert.Tracking, tracking)
		}
	}
	return alert, diags
}

func ipsAlertDecode(
	ctx context.Context, element settings.SettingIpsSuppressionAlerts,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	trackingValues := make([]attr.Value, 0, len(element.Tracking))
	for _, t := range element.Tracking {
		object, d := ipsTrackingDecode(ctx, t)
		diags.Append(d...)
		trackingValues = append(trackingValues, object)
	}
	// Unconditional, matching the deleted ipsSettingToModel: an alert's own
	// tracking list is never plan-conditioned at this nested level, only the
	// top-level suppression_alerts list is (see ipsAfterReceive) -- so this
	// is always a real (possibly empty) list, never null.
	trackingList, d := types.ListValue(types.ObjectType{AttrTypes: ipsTrackingAttrTypes}, trackingValues)
	diags.Append(d...)
	object, d := types.ObjectValueFrom(ctx, ipsAlertAttrTypes, settingIpsAlertModel{
		Category:  util.StringValueOrNull(element.Category),
		Gid:       types.Int64PointerValue(element.Gid),
		ID:        types.Int64PointerValue(element.ID),
		Signature: util.StringValueOrNull(element.Signature),
		Type:      util.StringValueOrNull(element.Type),
		Tracking:  trackingList,
	})
	diags.Append(d...)
	return object, diags
}

// ipsAfterReceive reproduces the deleted ipsSettingToModel: every one of
// ips's eleven attributes -- the nine ipsKitSpec maps plus the two
// suppression_alerts/suppression_whitelist ipsSuppressionKitSpec's own
// document hydrates into this same shared model -- is plan-conditioned:
// null unless the practitioner's own config (prior) set it, so an unmanaged
// ips attribute never drifts. The five lists use a single guard, not the
// double one mgmt's ssh_keys applies: a configured-but-empty list stays an
// empty list, matching the deleted mapper's own single
// plan.IsNull()/IsUnknown() check on each (the same choice doh's own lists
// made -- see setting_doh_descriptor.go's dohAfterReceive).
func ipsAfterReceive(
	_ context.Context, _ *settings.Ips, model *settingIpsModel, prior settingIpsModel,
) diag.Diagnostics {
	boolOrNull := func(priorValue, modelValue types.Bool) types.Bool {
		if priorValue.IsNull() || priorValue.IsUnknown() {
			return types.BoolNull()
		}
		return modelValue
	}
	stringOrNull := func(priorValue, modelValue types.String) types.String {
		if priorValue.IsNull() || priorValue.IsUnknown() {
			return types.StringNull()
		}
		return modelValue
	}

	model.ContentFilteringBlockingPageEnabled = boolOrNull(
		prior.ContentFilteringBlockingPageEnabled, model.ContentFilteringBlockingPageEnabled)
	model.HoneypotEnabled = boolOrNull(prior.HoneypotEnabled, model.HoneypotEnabled)
	model.MemoryOptimized = boolOrNull(prior.MemoryOptimized, model.MemoryOptimized)
	model.RestrictTorrents = boolOrNull(prior.RestrictTorrents, model.RestrictTorrents)

	model.AdvancedFilteringPreference = stringOrNull(prior.AdvancedFilteringPreference, model.AdvancedFilteringPreference)
	model.IPSMode = stringOrNull(prior.IPSMode, model.IPSMode)

	if prior.EnabledCategories.IsNull() || prior.EnabledCategories.IsUnknown() {
		model.EnabledCategories = types.ListNull(types.StringType)
	}
	if prior.EnabledNetworks.IsNull() || prior.EnabledNetworks.IsUnknown() {
		model.EnabledNetworks = types.ListNull(types.StringType)
	}
	if prior.Honeypot.IsNull() || prior.Honeypot.IsUnknown() {
		model.Honeypot = types.ListNull(types.ObjectType{AttrTypes: ipsHoneypotAttrTypes})
	}
	if prior.SuppressionWhitelist.IsNull() || prior.SuppressionWhitelist.IsUnknown() {
		model.SuppressionWhitelist = types.ListNull(types.ObjectType{AttrTypes: ipsWhitelistAttrTypes})
	}
	if prior.SuppressionAlerts.IsNull() || prior.SuppressionAlerts.IsUnknown() {
		model.SuppressionAlerts = types.ListNull(types.ObjectType{AttrTypes: ipsAlertAttrTypes})
	}

	return nil
}

// ipsNestedSchema is the ips SingleNestedAttribute's own Attributes, wrapped
// as a schema.Schema so resourcekit's conformance checks -- built for a
// whole resource's top-level schema -- can run against one section of
// unifi_setting instead.
func ipsNestedSchema(ctx context.Context) schema.Schema {
	built := resource_setting.SettingResourceSchema(ctx)
	ips := built.Attributes["ips"].(schema.SingleNestedAttribute) //nolint:forcetypeassert // ips is declared as SingleNestedAttribute in the generated schema; a mismatch here is a generator regression this is meant to catch loudly.
	return schema.Schema{Attributes: ips.Attributes}
}

// ipsSuppressionNestedSchema is the subset of ips's own nested schema that
// ipsSuppressionKitSpec maps: resourcekit's conformance checks compare a
// Spec against exactly the schema.Schema it owns, and ips_suppression owns
// two of ips's eleven attributes, not the whole SingleNestedAttribute.
func ipsSuppressionNestedSchema(ctx context.Context) schema.Schema {
	ips := ipsNestedSchema(ctx)
	attrs := map[string]schema.Attribute{}
	for _, name := range []string{"suppression_alerts", "suppression_whitelist"} {
		attrs[name] = ips.Attributes[name]
	}
	return schema.Schema{Attributes: attrs}
}

// ipsKitBackend binds ipsKitSpec to a client: Read is GetSetting[*Ips],
// UpdateFields is the masked UpdateSettingFields -- naming only the fields
// the plan set instead of the read-modify-write whole-document PUT
// writeIpsSection used. This is also what retires the deleted
// ipsModelToSetting's own read-then-merge: settings.Ips force-emits four
// fields (content_filtering_blocking_page_enabled, honeypot_enabled,
// memory_optimized, restrict_torrents; no omitempty), which never enter a
// masked write's body unless the plan actually named them --
// TestIpsSpecMasksOnlyTheFieldsThePlanSet pins this directly.
func ipsKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.Ips] {
	return resourcekit.Backend[settings.Ips]{
		Read: func(ctx context.Context, site, _ string) (*settings.Ips, error) {
			_, ips, err := ui.GetSetting[*settings.Ips](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return ips, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.Ips, fields ...string,
		) (*settings.Ips, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// ipsSuppressionKitSpec maps ips's two suppression_alerts/suppression_whitelist
// attributes onto settings.IpsSuppression directly -- unlike usg_geo, both
// are IpsSuppression's own top-level json tags ("alerts", "whitelist"), so
// no bridging struct is needed the way usgGeoKitSpec needed
// SettingUsgGeoIPFiltering. Both lists are Optional+Computed with no
// validator on the list attribute itself, so KeepZero is what the
// conformance check demands, same as ipsKitSpec's own three lists. Carries
// no TypeName -- see this file's own top comment and usg_geo's precedent in
// setting_usg_descriptor.go.
func ipsSuppressionKitSpec() resourcekit.Spec[settingIpsModel, settings.IpsSuppression] {
	return resourcekit.Spec[settingIpsModel, settings.IpsSuppression]{
		Subject: "IPS Suppression Setting",
		New:     func() *settings.IpsSuppression { return &settings.IpsSuppression{} },
		Fields: []resourcekit.Field[settingIpsModel, settings.IpsSuppression]{
			resourcekit.ObjectListField[settingIpsModel, settings.IpsSuppression, settings.SettingIpsSuppressionAlerts]{
				Wire:      "alerts",
				Model:     func(m *settingIpsModel) *types.List { return &m.SuppressionAlerts },
				SDK:       func(s *settings.IpsSuppression) *[]settings.SettingIpsSuppressionAlerts { return &s.Alerts },
				AttrTypes: ipsAlertAttrTypes,
				Encode:    ipsAlertEncode,
				Decode:    ipsAlertDecode,
				Elide:     resourcekit.KeepZero,
			},
			resourcekit.ObjectListField[settingIpsModel, settings.IpsSuppression, settings.SettingIpsSuppressionWhitelist]{
				Wire:  "whitelist",
				Model: func(m *settingIpsModel) *types.List { return &m.SuppressionWhitelist },
				SDK: func(s *settings.IpsSuppression) *[]settings.SettingIpsSuppressionWhitelist {
					return &s.Whitelist
				},
				AttrTypes: ipsWhitelistAttrTypes,
				Encode:    ipsWhitelistEncode,
				Decode:    ipsWhitelistDecode,
				Elide:     resourcekit.KeepZero,
			},
		},
	}
}

// ipsSuppressionKitBackend binds ipsSuppressionKitSpec to a client. Read
// absorbs a not-found the same tolerance readIpsSection gave it -- see this
// file's own top comment for why that has to happen here rather than
// through OnReadNotFound. UpdateFields is the same masked
// UpdateSettingFields every other section's backend uses; no
// read-modify-write is needed since alerts/whitelist are IpsSuppression's
// own top-level wires.
func ipsSuppressionKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.IpsSuppression] {
	return resourcekit.Backend[settings.IpsSuppression]{
		Read: func(ctx context.Context, site, _ string) (*settings.IpsSuppression, error) {
			_, suppression, err := ui.GetSetting[*settings.IpsSuppression](client, ctx, site)
			if err != nil {
				var notFound *ui.NotFoundError
				if errors.As(err, &notFound) {
					return &settings.IpsSuppression{}, nil
				}
				return nil, err
			}
			return suppression, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.IpsSuppression, fields ...string,
		) (*settings.IpsSuppression, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// ipsSuppressionNotSupportedDiagnostic is the deleted writeIpsSuppression's
// own controller-too-old diagnostic, copied verbatim.
func ipsSuppressionNotSupportedDiagnostic(error) diag.Diagnostics {
	var diags diag.Diagnostics
	diags.AddError(
		"IPS Suppression Not Supported By This Controller",
		"The `suppression_alerts` and `suppression_whitelist` attributes are stored in "+
			"the `ips_suppression` setting, which this controller does not expose. UniFi "+
			"Network 10.x moved them out of the `ips` setting. Remove them from the `ips` "+
			"block, or upgrade the controller.",
	)
	return diags
}

// ipsSuppressionKitDocument builds ips's Extra: written only when the plan
// sets at least one suppression list -- an empty mask is a no-op that
// SpecDocument's own Write skips before Backend.UpdateFields ever runs,
// exactly the predicate the deleted ipsSuppressionConfigured checked by
// hand (either list non-null). Read unconditionally, the same as every
// other Extra. OnWriteNotFound reproduces writeIpsSuppression's own
// diagnostic; OnReadNotFound stays nil, since ipsSuppressionKitBackend's own
// Read never returns a not-found error for Document.Read to see -- it
// absorbs one itself (see this file's own top comment).
func ipsSuppressionKitDocument(client *ui.ApiClient) resourcekit.Document[settingIpsModel] {
	spec := ipsSuppressionKitSpec()
	spec.Backend = ipsSuppressionKitBackend(client)
	return resourcekit.SpecDocument[settingIpsModel, settings.IpsSuppression]{
		Spec:            spec,
		OnWriteNotFound: ipsSuppressionNotSupportedDiagnostic,
	}
}

// ipsKitSection builds the ips entry for settingResource's Sections, bound to
// client via settingKitSections, which calls it with r.client.ApiClient.
func ipsKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := ipsKitSpec()
	spec.Backend = ipsKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingIpsModel, settings.Ips]{
		SectionName:  "ips",
		Get:          func(m *settingResourceModel) *types.Object { return &m.Ips },
		Set:          func(m *settingResourceModel, o types.Object) { m.Ips = o },
		AttrTypes:    ipsAttrTypes,
		Spec:         spec,
		AfterReceive: ipsAfterReceive,
		Extra:        []resourcekit.Document[settingIpsModel]{ipsSuppressionKitDocument(client)},
	}
}
