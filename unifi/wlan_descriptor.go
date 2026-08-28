package unifi

import (
	"context"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	listresource_wlan "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_wlan"
	resource_wlan "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_wlan"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// wlanKitModel is the existing resource model, reused rather than restated.
type wlanKitModel struct {
	ID                          types.String `tfsdk:"id"`
	Site                        types.String `tfsdk:"site"`
	Name                        types.String `tfsdk:"name"`
	NetworkID                   types.String `tfsdk:"network_id"`
	UserGroupID                 types.String `tfsdk:"user_group_id"`
	Security                    types.String `tfsdk:"security"`
	WPA3Support                 types.Bool   `tfsdk:"wpa3_support"`
	WPA3Transition              types.Bool   `tfsdk:"wpa3_transition"`
	PMFMode                     types.String `tfsdk:"pmf_mode"`
	Passphrase                  types.String `tfsdk:"passphrase"`
	PassphraseWO                types.String `tfsdk:"passphrase_wo"`
	HideSSID                    types.Bool   `tfsdk:"hide_ssid"`
	IsGuest                     types.Bool   `tfsdk:"is_guest"`
	Enabled                     types.Bool   `tfsdk:"enabled"`
	ApGroupIDs                  types.Set    `tfsdk:"ap_group_ids"`
	ApGroupMode                 types.String `tfsdk:"ap_group_mode"`
	VLANEnabled                 types.Bool   `tfsdk:"vlan_enabled"`
	VLAN                        types.Int64  `tfsdk:"vlan"`
	WLANBand                    types.String `tfsdk:"wlan_band"`
	WLANBands                   types.Set    `tfsdk:"wlan_bands"`
	MulticastEnhance            types.Bool   `tfsdk:"multicast_enhance"`
	MacFilter                   types.Object `tfsdk:"mac_filter"`
	PrivatePresharedKeysEnabled types.Bool   `tfsdk:"private_preshared_keys_enabled"`
	PrivatePresharedKeys        types.List   `tfsdk:"private_preshared_keys"`
	RadiusProfileID             types.String `tfsdk:"radius_profile_id"`
	NasIDentifierType           types.String `tfsdk:"nas_identifier_type"`
	Schedule                    types.List   `tfsdk:"schedule"`
	No2GhzOui                   types.Bool   `tfsdk:"no2ghz_oui"`
	L2Isolation                 types.Bool   `tfsdk:"l2_isolation"`
	ProxyArp                    types.Bool   `tfsdk:"proxy_arp"`
	BssTransition               types.Bool   `tfsdk:"bss_transition"`
	Uapsd                       types.Bool   `tfsdk:"uapsd"`
	FastRoamingEnabled          types.Bool   `tfsdk:"fast_roaming_enabled"`
	MinimumDataRate2GKbps       types.Int64  `tfsdk:"minimum_data_rate_2g_kbps"`
	MinimumDataRate5GKbps       types.Int64  `tfsdk:"minimum_data_rate_5g_kbps"`
	MinrateSettingPreference    types.String `tfsdk:"minrate_setting_preference"`
	RoamingAssistantNaEnabled   types.Bool   `tfsdk:"roaming_assistant_na_enabled"`
	RoamingAssistantNaRssi      types.Int64  `tfsdk:"roaming_assistant_na_rssi"`
	RoamingAssistant6EEnabled   types.Bool   `tfsdk:"roaming_assistant_6e_enabled"`
	RoamingAssistant6ERssi      types.Int64  `tfsdk:"roaming_assistant_6e_rssi"`

	// Security / encryption
	WPAMode types.String `tfsdk:"wpa_mode"`
	WPAEnc  types.String `tfsdk:"wpa_enc"`

	// DTIM
	DTIMMode types.String `tfsdk:"dtim_mode"`
	DTIMNg   types.Int64  `tfsdk:"dtim_ng"`
	DTIMNa   types.Int64  `tfsdk:"dtim_na"`
	DTIM6E   types.Int64  `tfsdk:"dtim_6e"`

	// Misc toggles
	GroupRekey           types.Int64 `tfsdk:"group_rekey"`
	IappEnabled          types.Bool  `tfsdk:"iapp_enabled"`
	WPA3FastRoaming      types.Bool  `tfsdk:"wpa3_fast_roaming"`
	WPA3Enhanced192      types.Bool  `tfsdk:"wpa3_enhanced_192"`
	RADIUSMacAuthEnabled types.Bool  `tfsdk:"radius_mac_auth_enabled"`
	EnhancedIot          types.Bool  `tfsdk:"enhanced_iot"`
	Hotspot2ConfEnabled  types.Bool  `tfsdk:"hotspot2conf_enabled"`
	MloEnabled           types.Bool  `tfsdk:"mlo_enabled"`
	BroadcastFilterList  types.Set   `tfsdk:"bc_filter_list"`

	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

// wlanPrefetched is what Prefetch fetches. Two lookups rather than one, because
// the surface defaults two different identifiers from site inventory and the
// kit hands back a single value.
type wlanPrefetched struct {
	wlanGroups []ui.WLANGroup
	apGroups   []ui.APGroup
}

// AttributeTypes for the two element models that lacked one. ppsk already
// declares its own in wlan_resource.go.
func (m wlanMacFilterModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"enabled": types.BoolType,
		"list":    types.SetType{ElemType: types.StringType},
		"policy":  types.StringType,
	}
}

func (m wlanScheduleModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"day_of_week":  types.StringType,
		"start_hour":   types.Int64Type,
		"start_minute": types.Int64Type,
		"duration":     timetypes.GoDurationType{},
		"name":         types.StringType,
	}
}

// encodeWlanMacFilter writes the mac_filter block onto its three flat wires.
//
// The block is one Terraform object over three unrelated fields on ui.WLAN --
// there is no MacFilter struct in the SDK to point a plain ObjectField at, which
// is what ScatteredObjectField exists for.
func encodeWlanMacFilter(ctx context.Context, object types.Object, sdk *ui.WLAN) diag.Diagnostics {
	var diags diag.Diagnostics
	var block wlanMacFilterModel
	diags.Append(object.As(ctx, &block, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	sdk.MACFilterEnabled = block.Enabled.ValueBool()
	sdk.MACFilterPolicy = block.Policy.ValueString()
	sdk.MACFilterList = nil
	if !block.List.IsNull() && !block.List.IsUnknown() {
		var macs []types.String
		diags.Append(block.List.ElementsAs(ctx, &macs, false)...)
		if diags.HasError() {
			return diags
		}
		for _, mac := range macs {
			sdk.MACFilterList = append(sdk.MACFilterList, mac.ValueString())
		}
	}
	return diags
}

func decodeWlanMacFilter(ctx context.Context, sdk *ui.WLAN, _ types.Object) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	list := types.SetNull(types.StringType)
	if len(sdk.MACFilterList) > 0 {
		values := make([]attr.Value, len(sdk.MACFilterList))
		for i, mac := range sdk.MACFilterList {
			values[i] = types.StringValue(mac)
		}
		set, d := types.SetValue(types.StringType, values)
		diags.Append(d...)
		list = set
	}
	policy := types.StringNull()
	if sdk.MACFilterPolicy != "" {
		policy = types.StringValue(sdk.MACFilterPolicy)
	}
	object, d := types.ObjectValue(wlanMacFilterModel{}.AttributeTypes(), map[string]attr.Value{
		"enabled": types.BoolValue(sdk.MACFilterEnabled),
		"list":    list,
		"policy":  policy,
	})
	diags.Append(d...)
	return object, diags
}

// wlanScheduleToSDK and wlanScheduleFromSDK live in BeforeSend/AfterReceive
// rather than as a Field because schedule isn't 1:1: write collapses each
// model entry to a single-day SDK element, but read must expand one
// controller element into several model entries, since the controller can
// return multi-day entries even though our own writes never produce them.
func wlanScheduleToSDK(ctx context.Context, model *wlanKitModel, sdk *ui.WLAN) diag.Diagnostics {
	var diags diag.Diagnostics
	sdk.ScheduleWithDuration = nil
	if !model.Schedule.IsNull() && !model.Schedule.IsUnknown() {
		var entries []wlanScheduleModel
		diags.Append(model.Schedule.ElementsAs(ctx, &entries, false)...)
		if diags.HasError() {
			return diags
		}
		for _, entry := range entries {
			sdk.ScheduleWithDuration = append(sdk.ScheduleWithDuration, ui.WLANScheduleWithDuration{
				StartDaysOfWeek: []string{entry.DayOfWeek.ValueString()},
				StartHour:       entry.StartHour.ValueInt64Pointer(),
				StartMinute:     entry.StartMinute.ValueInt64Pointer(),
				DurationMinutes: util.DurationUnitsPtr(entry.Duration, time.Minute),
				Name:            entry.Name.ValueString(),
			})
		}
	}
	sdk.ScheduleEnabled = len(sdk.ScheduleWithDuration) > 0
	return diags
}

func wlanScheduleFromSDK(sdk *ui.WLAN, model *wlanKitModel) diag.Diagnostics {
	var diags diag.Diagnostics
	elementType := types.ObjectType{AttrTypes: wlanScheduleModel{}.AttributeTypes()}
	if len(sdk.ScheduleWithDuration) == 0 {
		model.Schedule = types.ListNull(elementType)
		return diags
	}
	var values []attr.Value
	for _, entry := range sdk.ScheduleWithDuration {
		for _, day := range entry.StartDaysOfWeek {
			object, d := types.ObjectValue(wlanScheduleModel{}.AttributeTypes(), map[string]attr.Value{
				"day_of_week":  types.StringValue(day),
				"start_hour":   types.Int64PointerValue(entry.StartHour),
				"start_minute": types.Int64PointerValue(entry.StartMinute),
				"duration":     util.DurationPtrValue(entry.DurationMinutes, time.Minute),
				"name":         types.StringValue(entry.Name),
			})
			diags.Append(d...)
			values = append(values, object)
		}
	}
	list, d := types.ListValue(elementType, values)
	diags.Append(d...)
	model.Schedule = list
	return diags
}

func wlanPrefetch(client *ui.ApiClient) func(context.Context, string) (any, diag.Diagnostics) {
	return func(ctx context.Context, site string) (any, diag.Diagnostics) {
		var diags diag.Diagnostics
		wlanGroups, err := client.ListWLANGroup(ctx, site)
		if err != nil {
			diags.AddError("Error Listing WLAN Groups", "Could not list WLAN groups: "+err.Error())
			return nil, diags
		}
		apGroups, err := client.ListAPGroup(ctx, site)
		if err != nil {
			diags.AddError("Error Listing AP Groups", "Could not list AP groups: "+err.Error())
			return nil, diags
		}
		return wlanPrefetched{wlanGroups: wlanGroups, apGroups: apGroups}, diags
	}
}

// wlanBeforeSend carries the five derived wires and the two defaults, all in
// AlwaysWire since no attribute holds them and nothing else would add them to
// the mask.
func wlanBeforeSend(ctx context.Context, config, effective *wlanKitModel, sdk *ui.WLAN, prefetched any) diag.Diagnostics {
	var diags diag.Diagnostics

	// Always true on the wire; no attribute has ever carried it.
	sdk.NameCombineEnabled = true

	// wlan_band is derived from wlan_bands; the wlan_band attribute itself is
	// read-only, and neither wire is written when the set is absent.
	if !effective.WLANBands.IsNull() && !effective.WLANBands.IsUnknown() {
		var contains2g, contains5g bool
		var bands []types.String
		diags.Append(effective.WLANBands.ElementsAs(ctx, &bands, false)...)
		if diags.HasError() {
			return diags
		}
		for _, band := range bands {
			switch band.ValueString() {
			case "2g":
				contains2g = true
			case "5g":
				contains5g = true
			}
		}
		switch {
		case contains2g && contains5g:
			sdk.WLANBand = "both"
		case contains2g:
			sdk.WLANBand = "2g"
		case contains5g:
			sdk.WLANBand = "5g"
		}
	}

	// One attribute drives two wires on each band: the rate, and an enabled
	// flag that is a predicate over it. The flags are write-only -- nothing
	// reads them back into the model.
	sdk.MinrateNgEnabled = effective.MinimumDataRate2GKbps.ValueInt64() > 0
	sdk.MinrateNaEnabled = effective.MinimumDataRate5GKbps.ValueInt64() > 0

	diags.Append(wlanScheduleToSDK(ctx, effective, sdk)...)
	if diags.HasError() {
		return diags
	}

	// passphrase_wo never lands in state: stashed onto effective here so
	// AfterReceive, which sees no config, can null it before state is written.
	if wo := config.PassphraseWO; !wo.IsNull() && !wo.IsUnknown() && wo.ValueString() != "" {
		sdk.Passphrase = wo.ValueString()
		effective.PassphraseWO = wo
	}

	inventory, _ := prefetched.(wlanPrefetched)

	// UDM SE requires ap_group_ids even when ap_group_mode is "all", but only
	// on create: sdk.ID is empty only then, since Update calls Backend.SetID
	// before BeforeSend runs.
	if sdk.ID == "" && sdk.ApGroupMode == "all" && len(sdk.ApGroupIDs) == 0 {
		for _, group := range inventory.apGroups {
			if group.HiddenID == "default" {
				sdk.ApGroupIDs = []string{group.ID}
				break
			}
		}
		if len(sdk.ApGroupIDs) == 0 && len(inventory.apGroups) > 0 {
			sdk.ApGroupIDs = []string{inventory.apGroups[0].ID}
		}
	}

	// go-unifi serialises WLANGroupID without omitempty, so a blank sends
	// `"wlangroup_id":""`, which UniFi Network 10.x rejects. The default WLAN
	// group reports attr_hidden_id "Default"; AP groups use "default", so match
	// case-insensitively.
	if sdk.WLANGroupID == "" { //nolint:staticcheck // required on the wire regardless, see above
		for _, group := range inventory.wlanGroups {
			if strings.EqualFold(group.HiddenID, "default") {
				sdk.WLANGroupID = group.ID //nolint:staticcheck // as above
				break
			}
		}
		if sdk.WLANGroupID == "" && len(inventory.wlanGroups) > 0 { //nolint:staticcheck // as above
			sdk.WLANGroupID = inventory.wlanGroups[0].ID //nolint:staticcheck // as above
		}
	}

	return diags
}

func wlanAfterReceive(
	ctx context.Context,
	sdk *ui.WLAN,
	model *wlanKitModel,
	prior wlanKitModel,
	_ any,
) diag.Diagnostics {
	var diags diag.Diagnostics

	// PassphraseWO non-null means this apply used the write-only form; a null
	// prior means the last apply already nulled Passphrase (a bare refresh).
	if !model.PassphraseWO.IsNull() || prior.Passphrase.IsNull() {
		model.Passphrase = types.StringNull()
	}
	if !model.PassphraseWO.IsNull() {
		model.PassphraseWO = types.StringNull()
	}

	// private_preshared_keys needs the same prior-preservation on a refresh
	// that Create/Update get for free from ObjectListField.CopyPlanToState,
	// which doesn't run here: without this, a controller that redacts a
	// password or reorders bindings would manufacture drift on every plan.
	ppsk, d := wlanPrivatePresharedKeysState(ctx, sdk, prior.PrivatePresharedKeys)
	diags.Append(d...)
	model.PrivatePresharedKeys = ppsk

	diags.Append(wlanScheduleFromSDK(sdk, model)...)
	return diags
}

// wlanPrivatePresharedKeysState decides whether private_preshared_keys should
// keep the prior state or take the controller's response, for the refresh
// path wlanAfterReceive uses it from.
//
// A password UniFi omits on an otherwise-matching entry is not a valid
// changed password -- the schema requires 8 to 255 characters, so an entry
// with none is redaction, not clearance. Matching visible passwords first
// keeps a duplicate network binding unambiguous.
func wlanPrivatePresharedKeysState(
	ctx context.Context,
	wlan *ui.WLAN,
	prior types.List,
) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics
	ppskType := types.ObjectType{AttrTypes: wlanPrivatePresharedKeyModel{}.AttributeTypes()}

	// Disabled PPSK, or an absent list, has no key state to preserve.
	if !wlan.PrivatePresharedKeysEnabled || len(wlan.PrivatePresharedKeys) == 0 {
		return types.ListNull(ppskType), diags
	}

	values := make([]wlanPrivatePresharedKeyModel, len(wlan.PrivatePresharedKeys))
	for i, key := range wlan.PrivatePresharedKeys {
		values[i] = wlanPrivatePresharedKeyModel{
			NetworkID: types.StringValue(key.NetworkID),
			Password:  types.StringValue(key.Password),
		}
	}
	remote, d := types.ListValueFrom(ctx, ppskType, values)
	diags.Append(d...)
	if prior.IsNull() || prior.IsUnknown() || len(values) != len(prior.Elements()) {
		return remote, diags
	}

	var keys []wlanPrivatePresharedKeyModel
	diags.Append(prior.ElementsAs(ctx, &keys, false)...)
	if diags.HasError() {
		return remote, diags
	}

	matched := make([]bool, len(keys))
	match := func(current ui.WLANPrivatePresharedKeys) bool {
		for i, old := range keys {
			if matched[i] || old.NetworkID.ValueString() != current.NetworkID {
				continue
			}
			if current.Password != "" && old.Password.ValueString() != current.Password {
				continue
			}
			matched[i] = true
			return true
		}
		return false
	}

	// Match visible passwords first, so an omitted password cannot consume the
	// binding a visible one needs.
	for _, current := range wlan.PrivatePresharedKeys {
		if current.Password != "" && !match(current) {
			return remote, diags
		}
	}
	for _, current := range wlan.PrivatePresharedKeys {
		if current.Password == "" && !match(current) {
			return remote, diags
		}
	}

	return prior, diags
}

func wlanKitSpec() resourcekit.Spec[wlanKitModel, ui.WLAN] {
	return resourcekit.Spec[wlanKitModel, ui.WLAN]{
		TypeName: "wlan",
		Subject:  "WLAN",
		New:      func() *ui.WLAN { return &ui.WLAN{} },
		ID:       func(m *wlanKitModel) *types.String { return &m.ID },
		Site:     func(m *wlanKitModel) *types.String { return &m.Site },
		Timeouts: func(m *wlanKitModel) *timeouts.Value { return &m.Timeouts },
		// The documented import handle is the bare SSID; see Spec.Name.
		Name:   func(m *wlanKitModel) *types.String { return &m.Name },
		IDWire: "_id",
		// Five wires no attribute holds. Each is set by wlanBeforeSend, so the
		// plan never mentions them and nothing else would add them to the mask.
		AlwaysWire: []string{
			"name_combine_enabled",
			"wlangroup_id",
			"wlan_band",
			"schedule_enabled",
			"schedule_with_duration",
			"minrate_ng_enabled",
			"minrate_na_enabled",
		},
		Prefetch:     nil, // bound in Configure, where the client exists
		BeforeSend:   wlanBeforeSend,
		AfterReceive: wlanAfterReceive,
		Fields: []resourcekit.Field[wlanKitModel, ui.WLAN]{
			resourcekit.StringField[wlanKitModel, ui.WLAN]{
				Wire:  "name",
				Model: func(m *wlanKitModel) *types.String { return &m.Name },
				SDK:   func(s *ui.WLAN) *string { return &s.Name },
			},
			resourcekit.StringField[wlanKitModel, ui.WLAN]{
				Wire:  "networkconf_id",
				Model: func(m *wlanKitModel) *types.String { return &m.NetworkID },
				SDK:   func(s *ui.WLAN) *string { return &s.NetworkID },
			},
			resourcekit.StringField[wlanKitModel, ui.WLAN]{
				Wire:  "usergroup_id",
				Model: func(m *wlanKitModel) *types.String { return &m.UserGroupID },
				SDK:   func(s *ui.WLAN) *string { return &s.UserGroupID },
			},
			resourcekit.StringField[wlanKitModel, ui.WLAN]{
				Wire:  "security",
				Model: func(m *wlanKitModel) *types.String { return &m.Security },
				SDK:   func(s *ui.WLAN) *string { return &s.Security },
			},
			resourcekit.BoolField[wlanKitModel, ui.WLAN]{
				Wire:  "wpa3_support",
				Model: func(m *wlanKitModel) *types.Bool { return &m.WPA3Support },
				SDK:   func(s *ui.WLAN) *bool { return &s.WPA3Support },
			},
			resourcekit.BoolField[wlanKitModel, ui.WLAN]{
				Wire:  "wpa3_transition",
				Model: func(m *wlanKitModel) *types.Bool { return &m.WPA3Transition },
				SDK:   func(s *ui.WLAN) *bool { return &s.WPA3Transition },
			},
			resourcekit.StringField[wlanKitModel, ui.WLAN]{
				Wire:  "pmf_mode",
				Model: func(m *wlanKitModel) *types.String { return &m.PMFMode },
				SDK:   func(s *ui.WLAN) *string { return &s.PMFMode },
				Elide: resourcekit.NullZero,
			},
			resourcekit.StringField[wlanKitModel, ui.WLAN]{
				Wire:  "x_passphrase",
				Model: func(m *wlanKitModel) *types.String { return &m.Passphrase },
				SDK:   func(s *ui.WLAN) *string { return &s.Passphrase },
				Elide: resourcekit.NullZero,
			},
			resourcekit.BoolField[wlanKitModel, ui.WLAN]{
				Wire:  "hide_ssid",
				Model: func(m *wlanKitModel) *types.Bool { return &m.HideSSID },
				SDK:   func(s *ui.WLAN) *bool { return &s.HideSSID },
			},
			resourcekit.BoolField[wlanKitModel, ui.WLAN]{
				Wire:  "is_guest",
				Model: func(m *wlanKitModel) *types.Bool { return &m.IsGuest },
				SDK:   func(s *ui.WLAN) *bool { return &s.IsGuest },
			},
			resourcekit.BoolField[wlanKitModel, ui.WLAN]{
				Wire:  "enabled",
				Model: func(m *wlanKitModel) *types.Bool { return &m.Enabled },
				SDK:   func(s *ui.WLAN) *bool { return &s.Enabled },
			},
			resourcekit.StringField[wlanKitModel, ui.WLAN]{
				Wire:  "ap_group_mode",
				Model: func(m *wlanKitModel) *types.String { return &m.ApGroupMode },
				SDK:   func(s *ui.WLAN) *string { return &s.ApGroupMode },
				Elide: resourcekit.NullZero,
			},
			resourcekit.BoolField[wlanKitModel, ui.WLAN]{
				Wire:  "vlan_enabled",
				Model: func(m *wlanKitModel) *types.Bool { return &m.VLANEnabled },
				SDK:   func(s *ui.WLAN) *bool { return &s.VLANEnabled },
			},
			resourcekit.Int64PtrField[wlanKitModel, ui.WLAN]{
				Wire:  "vlan",
				Model: func(m *wlanKitModel) *types.Int64 { return &m.VLAN },
				SDK:   func(s *ui.WLAN) **int64 { return &s.VLAN },
				Elide: resourcekit.NullZero,
			},
			resourcekit.BoolField[wlanKitModel, ui.WLAN]{
				Wire:  "mcastenhance_enabled",
				Model: func(m *wlanKitModel) *types.Bool { return &m.MulticastEnhance },
				SDK:   func(s *ui.WLAN) *bool { return &s.MulticastEnhanceEnabled },
			},
			resourcekit.StringField[wlanKitModel, ui.WLAN]{
				Wire:  "radiusprofile_id",
				Model: func(m *wlanKitModel) *types.String { return &m.RadiusProfileID },
				SDK:   func(s *ui.WLAN) *string { return &s.RADIUSProfileID },
			},
			resourcekit.StringField[wlanKitModel, ui.WLAN]{
				Wire:  "nas_identifier_type",
				Model: func(m *wlanKitModel) *types.String { return &m.NasIDentifierType },
				SDK:   func(s *ui.WLAN) *string { return &s.NasIDentifierType },
				Elide: resourcekit.NullZero,
			},
			resourcekit.BoolField[wlanKitModel, ui.WLAN]{
				Wire:  "no2ghz_oui",
				Model: func(m *wlanKitModel) *types.Bool { return &m.No2GhzOui },
				SDK:   func(s *ui.WLAN) *bool { return &s.No2GhzOui },
			},
			resourcekit.BoolField[wlanKitModel, ui.WLAN]{
				Wire:  "l2_isolation",
				Model: func(m *wlanKitModel) *types.Bool { return &m.L2Isolation },
				SDK:   func(s *ui.WLAN) *bool { return &s.L2Isolation },
			},
			resourcekit.BoolField[wlanKitModel, ui.WLAN]{
				Wire:  "proxy_arp",
				Model: func(m *wlanKitModel) *types.Bool { return &m.ProxyArp },
				SDK:   func(s *ui.WLAN) *bool { return &s.ProxyArp },
			},
			resourcekit.BoolField[wlanKitModel, ui.WLAN]{
				Wire:  "bss_transition",
				Model: func(m *wlanKitModel) *types.Bool { return &m.BssTransition },
				SDK:   func(s *ui.WLAN) *bool { return &s.BssTransition },
			},
			resourcekit.BoolField[wlanKitModel, ui.WLAN]{
				Wire:  "uapsd_enabled",
				Model: func(m *wlanKitModel) *types.Bool { return &m.Uapsd },
				SDK:   func(s *ui.WLAN) *bool { return &s.UapsdEnabled },
			},
			resourcekit.BoolField[wlanKitModel, ui.WLAN]{
				Wire:  "fast_roaming_enabled",
				Model: func(m *wlanKitModel) *types.Bool { return &m.FastRoamingEnabled },
				SDK:   func(s *ui.WLAN) *bool { return &s.FastRoamingEnabled },
			},
			resourcekit.StringField[wlanKitModel, ui.WLAN]{
				Wire:  "minrate_setting_preference",
				Model: func(m *wlanKitModel) *types.String { return &m.MinrateSettingPreference },
				SDK:   func(s *ui.WLAN) *string { return &s.MinrateSettingPreference },
				Elide: resourcekit.NullZero,
			},
			resourcekit.Int64PtrField[wlanKitModel, ui.WLAN]{
				Wire:  "minrate_ng_data_rate_kbps",
				Model: func(m *wlanKitModel) *types.Int64 { return &m.MinimumDataRate2GKbps },
				SDK:   func(s *ui.WLAN) **int64 { return &s.MinrateNgDataRateKbps },
			},
			resourcekit.Int64PtrField[wlanKitModel, ui.WLAN]{
				Wire:  "minrate_na_data_rate_kbps",
				Model: func(m *wlanKitModel) *types.Int64 { return &m.MinimumDataRate5GKbps },
				SDK:   func(s *ui.WLAN) **int64 { return &s.MinrateNaDataRateKbps },
			},
			resourcekit.BoolField[wlanKitModel, ui.WLAN]{
				Wire:  "roaming_assistant_na_enabled",
				Model: func(m *wlanKitModel) *types.Bool { return &m.RoamingAssistantNaEnabled },
				SDK:   func(s *ui.WLAN) *bool { return &s.RoamingAssistantNaEnabled },
			},
			resourcekit.Int64PtrField[wlanKitModel, ui.WLAN]{
				Wire:  "roaming_assistant_na_rssi",
				Model: func(m *wlanKitModel) *types.Int64 { return &m.RoamingAssistantNaRssi },
				SDK:   func(s *ui.WLAN) **int64 { return &s.RoamingAssistantNaRssi },
			},
			resourcekit.BoolField[wlanKitModel, ui.WLAN]{
				Wire:  "roaming_assistant_6e_enabled",
				Model: func(m *wlanKitModel) *types.Bool { return &m.RoamingAssistant6EEnabled },
				SDK:   func(s *ui.WLAN) *bool { return &s.RoamingAssistant6EEnabled },
			},
			resourcekit.Int64PtrField[wlanKitModel, ui.WLAN]{
				Wire:  "roaming_assistant_6e_rssi",
				Model: func(m *wlanKitModel) *types.Int64 { return &m.RoamingAssistant6ERssi },
				SDK:   func(s *ui.WLAN) **int64 { return &s.RoamingAssistant6ERssi },
			},
			resourcekit.Int64PtrField[wlanKitModel, ui.WLAN]{
				Wire:  "group_rekey",
				Model: func(m *wlanKitModel) *types.Int64 { return &m.GroupRekey },
				SDK:   func(s *ui.WLAN) **int64 { return &s.GroupRekey },
			},
			resourcekit.StringField[wlanKitModel, ui.WLAN]{
				Wire:  "dtim_mode",
				Model: func(m *wlanKitModel) *types.String { return &m.DTIMMode },
				SDK:   func(s *ui.WLAN) *string { return &s.DTIMMode },
				Elide: resourcekit.NullZero,
			},
			resourcekit.StringField[wlanKitModel, ui.WLAN]{
				Wire:  "wpa_enc",
				Model: func(m *wlanKitModel) *types.String { return &m.WPAEnc },
				SDK:   func(s *ui.WLAN) *string { return &s.WPAEnc },
				Elide: resourcekit.NullZero,
			},
			resourcekit.StringField[wlanKitModel, ui.WLAN]{
				Wire:  "wpa_mode",
				Model: func(m *wlanKitModel) *types.String { return &m.WPAMode },
				SDK:   func(s *ui.WLAN) *string { return &s.WPAMode },
				Elide: resourcekit.NullZero,
			},
			resourcekit.BoolField[wlanKitModel, ui.WLAN]{
				Wire:  "iapp_enabled",
				Model: func(m *wlanKitModel) *types.Bool { return &m.IappEnabled },
				SDK:   func(s *ui.WLAN) *bool { return &s.IappEnabled },
			},
			resourcekit.BoolField[wlanKitModel, ui.WLAN]{
				Wire:  "wpa3_fast_roaming",
				Model: func(m *wlanKitModel) *types.Bool { return &m.WPA3FastRoaming },
				SDK:   func(s *ui.WLAN) *bool { return &s.WPA3FastRoaming },
			},
			resourcekit.BoolField[wlanKitModel, ui.WLAN]{
				Wire:  "wpa3_enhanced_192",
				Model: func(m *wlanKitModel) *types.Bool { return &m.WPA3Enhanced192 },
				SDK:   func(s *ui.WLAN) *bool { return &s.WPA3Enhanced192 },
			},
			resourcekit.BoolField[wlanKitModel, ui.WLAN]{
				Wire:  "radius_mac_auth_enabled",
				Model: func(m *wlanKitModel) *types.Bool { return &m.RADIUSMacAuthEnabled },
				SDK:   func(s *ui.WLAN) *bool { return &s.RADIUSMACAuthEnabled },
			},
			resourcekit.BoolField[wlanKitModel, ui.WLAN]{
				Wire:  "enhanced_iot",
				Model: func(m *wlanKitModel) *types.Bool { return &m.EnhancedIot },
				SDK:   func(s *ui.WLAN) *bool { return &s.EnhancedIot },
			},
			resourcekit.BoolField[wlanKitModel, ui.WLAN]{
				Wire:  "hotspot2conf_enabled",
				Model: func(m *wlanKitModel) *types.Bool { return &m.Hotspot2ConfEnabled },
				SDK:   func(s *ui.WLAN) *bool { return &s.Hotspot2ConfEnabled },
			},
			resourcekit.BoolField[wlanKitModel, ui.WLAN]{
				Wire:  "mlo_enabled",
				Model: func(m *wlanKitModel) *types.Bool { return &m.MloEnabled },
				SDK:   func(s *ui.WLAN) *bool { return &s.MloEnabled },
			},
			// OmitZero: dtim_ng/na/6e are Optional+Computed with no schema
			// default, so an unset one is Unknown on create; the controller's
			// validator (^([1-9]|...|25[0-5])$|^$) rejects the zero that
			// ValueInt64Pointer() would otherwise send for it.
			resourcekit.Int64PtrField[wlanKitModel, ui.WLAN]{
				Wire:  "dtim_ng",
				Model: func(m *wlanKitModel) *types.Int64 { return &m.DTIMNg },
				SDK:   func(s *ui.WLAN) **int64 { return &s.DTIMNg },
				Elide: resourcekit.KeepZero, OmitZero: true,
			},
			resourcekit.Int64PtrField[wlanKitModel, ui.WLAN]{
				Wire:  "dtim_na",
				Model: func(m *wlanKitModel) *types.Int64 { return &m.DTIMNa },
				SDK:   func(s *ui.WLAN) **int64 { return &s.DTIMNa },
				Elide: resourcekit.KeepZero, OmitZero: true,
			},
			resourcekit.Int64PtrField[wlanKitModel, ui.WLAN]{
				Wire:  "dtim_6e",
				Model: func(m *wlanKitModel) *types.Int64 { return &m.DTIM6E },
				SDK:   func(s *ui.WLAN) **int64 { return &s.DTIM6E },
				Elide: resourcekit.KeepZero, OmitZero: true,
			},
			resourcekit.BoolField[wlanKitModel, ui.WLAN]{
				Wire:  "private_preshared_keys_enabled",
				Model: func(m *wlanKitModel) *types.Bool { return &m.PrivatePresharedKeysEnabled },
				SDK:   func(s *ui.WLAN) *bool { return &s.PrivatePresharedKeysEnabled },
			},
			// wlan_band is read-only as a field: the write side derives it from
			// wlan_bands in BeforeSend instead. ReadDefault substitutes "both"
			// for an empty value.
			resourcekit.ReadOnly[wlanKitModel, ui.WLAN](
				resourcekit.StringField[wlanKitModel, ui.WLAN]{
					Wire:        "wlan_band",
					Model:       func(m *wlanKitModel) *types.String { return &m.WLANBand },
					SDK:         func(s *ui.WLAN) *string { return &s.WLANBand },
					ReadDefault: "both",
				},
			),
			resourcekit.StringSetField[wlanKitModel, ui.WLAN]{
				Wire:  "ap_group_ids",
				Model: func(m *wlanKitModel) *types.Set { return &m.ApGroupIDs },
				SDK:   func(s *ui.WLAN) *[]string { return &s.ApGroupIDs },
			},
			resourcekit.StringSetField[wlanKitModel, ui.WLAN]{
				Wire:  "wlan_bands",
				Model: func(m *wlanKitModel) *types.Set { return &m.WLANBands },
				SDK:   func(s *ui.WLAN) *[]string { return &s.WLANBands },
			},
			resourcekit.StringSetField[wlanKitModel, ui.WLAN]{
				Wire:  "bc_filter_list",
				Model: func(m *wlanKitModel) *types.Set { return &m.BroadcastFilterList },
				SDK:   func(s *ui.WLAN) *[]string { return &s.BroadcastFilterList },
			},
			// One Terraform object over three unrelated flat wires, which is
			// what ScatteredObjectField is for -- ui.WLAN has no MacFilter
			// struct to point an ObjectField at.
			resourcekit.ScatteredObjectField[wlanKitModel, ui.WLAN]{
				Wires:     []string{"mac_filter_enabled", "mac_filter_list", "mac_filter_policy"},
				Model:     func(m *wlanKitModel) *types.Object { return &m.MacFilter },
				AttrTypes: wlanMacFilterModel{}.AttributeTypes(),
				Encode:    encodeWlanMacFilter,
				Decode:    decodeWlanMacFilter,
			},
			// ppsk maps one to one, so it is a Field. The per-key password is
			// sensitive and the controller does not always echo it; the plan
			// value survives through Field.CopyPlanToState.
			resourcekit.ObjectListField[wlanKitModel, ui.WLAN, ui.WLANPrivatePresharedKeys]{
				Wire:      "private_preshared_keys",
				Model:     func(m *wlanKitModel) *types.List { return &m.PrivatePresharedKeys },
				SDK:       func(s *ui.WLAN) *[]ui.WLANPrivatePresharedKeys { return &s.PrivatePresharedKeys },
				AttrTypes: wlanPrivatePresharedKeyModel{}.AttributeTypes(),
				Elide:     resourcekit.NullZero,
				Encode: func(ctx context.Context, object types.Object) (ui.WLANPrivatePresharedKeys, diag.Diagnostics) {
					var diags diag.Diagnostics
					var element wlanPrivatePresharedKeyModel
					diags.Append(object.As(ctx, &element, basetypes.ObjectAsOptions{})...)
					return ui.WLANPrivatePresharedKeys{
						NetworkID: element.NetworkID.ValueString(),
						Password:  element.Password.ValueString(),
					}, diags
				},
				Decode: func(ctx context.Context, element ui.WLANPrivatePresharedKeys) (types.Object, diag.Diagnostics) {
					return types.ObjectValueFrom(ctx, wlanPrivatePresharedKeyModel{}.AttributeTypes(), wlanPrivatePresharedKeyModel{
						NetworkID: types.StringValue(element.NetworkID),
						Password:  types.StringValue(element.Password),
					})
				},
			},
		},
		// Seeded here as well as in wlanKitBackend, because Configure binds the
		// real Backend and a unit test calling ToModel on an unconfigured spec
		// would otherwise dereference nil.
		Backend: resourcekit.Backend[ui.WLAN]{
			GetID: func(s *ui.WLAN) string { return s.ID },
			SetID: func(s *ui.WLAN, id string) { s.ID = id },
		},
	}
}

func wlanKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_wlan.WlanResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

func wlanKitList() resourcekit.ListSpec[ui.WLAN] {
	return resourcekit.ListSpec[ui.WLAN]{
		ConfigSchema: listresource_wlan.WlanListResourceSchema,
		DisplayName: func(s *ui.WLAN) string {
			if s.Name != "" {
				return s.Name
			}
			return s.ID
		},
		Filters: map[string]func(*ui.WLAN) string{
			"name": func(s *ui.WLAN) string { return s.Name },
			"enabled": func(s *ui.WLAN) string {
				if s.Enabled {
					return "true"
				}
				return "false"
			},
		},
	}
}

func wlanKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.WLAN] {
	return resourcekit.Backend[ui.WLAN]{
		Create: func(ctx context.Context, site string, in *ui.WLAN) (*ui.WLAN, error) {
			return client.CreateWLAN(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.WLAN, error) {
			return client.GetWLAN(ctx, site, id)
		},
		ReadByName: func(ctx context.Context, site, name string) (*ui.WLAN, error) {
			return client.GetWLANByName(ctx, site, name)
		},
		UpdateFields: func(ctx context.Context, site string, in *ui.WLAN, fields ...string) (*ui.WLAN, error) {
			return client.UpdateWLANFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.DeleteWLAN(ctx, site, id)
		},
		List: func(ctx context.Context, site string) ([]ui.WLAN, error) {
			return client.ListWLAN(ctx, site)
		},
		GetID: func(s *ui.WLAN) string { return s.ID },
		SetID: func(s *ui.WLAN, id string) { s.ID = id },
	}
}
