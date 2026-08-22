package unifi

// The unifi_device descriptor.
//
// device is the FIRST kit migration of a surface whose nesting is genuinely
// SDK-backed: config_network is *DeviceConfigNetwork, and outlet_overrides,
// port_overrides and radio_table are real slice types. 24 of 24 object fields
// across the seven surfaces still carrying the nested blocker are model-side
// groupings over FLAT SDK fields, which no field kind can express -- see
// surface-blockers.json. This surface is the exception, which is why it is the
// one that could be migrated.
//
// port_override needed ObjectSetField, which did not exist: the attribute is a
// set_nested_block, so the model carries types.Set where ObjectListField's
// accessor is *types.List.

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util/retry"
)

type deviceKitModel struct {
	ID              types.String       `tfsdk:"id"`
	Site            types.String       `tfsdk:"site"`
	MAC             hwtypes.MACAddress `tfsdk:"mac"`
	Name            types.String       `tfsdk:"name"`
	Disabled        types.Bool         `tfsdk:"disabled"`
	PortOverride    types.Set          `tfsdk:"port_override"`
	AllowAdoption   types.Bool         `tfsdk:"allow_adoption"`
	ForgetOnDestroy types.Bool         `tfsdk:"forget_on_destroy"`

	// Network configuration
	ConfigNetwork types.Object `tfsdk:"config_network"`

	// LED settings
	LedOverride                types.String `tfsdk:"led_override"`
	LedOverrideColor           types.String `tfsdk:"led_override_color"`
	LedOverrideColorBrightness types.Int64  `tfsdk:"led_override_color_brightness"`

	// Device features
	BandsteeringMode  types.String `tfsdk:"bandsteering_mode"`
	FlowctrlEnabled   types.Bool   `tfsdk:"flowctrl_enabled"`
	JumboframeEnabled types.Bool   `tfsdk:"jumboframe_enabled"`
	StpVersion        types.String `tfsdk:"stp_version"`
	StpPriority       types.Int64  `tfsdk:"stp_priority"`
	Locked            types.Bool   `tfsdk:"locked"`

	// PoE settings
	PoeMode types.String `tfsdk:"poe_mode"`

	// VLAN
	SwitchVLANEnabled types.Bool `tfsdk:"switch_vlan_enabled"`

	// Mesh
	MeshStaVapEnabled types.Bool `tfsdk:"mesh_sta_vap_enabled"`

	// Radio settings
	RadioTable types.List `tfsdk:"radio_table"`

	// Advanced features
	OutdoorModeOverride types.String `tfsdk:"outdoor_mode_override"`
	Volume              types.Int64  `tfsdk:"volume"`
	BaresipPassword     types.String `tfsdk:"x_baresip_password"`

	// LCD/LCM settings
	LcmBrightness          types.Int64          `tfsdk:"lcm_brightness"`
	LcmBrightnessOverride  types.Bool           `tfsdk:"lcm_brightness_override"`
	LcmIDleTimeout         timetypes.GoDuration `tfsdk:"lcm_idle_timeout"`
	LcmIDleTimeoutOverride types.Bool           `tfsdk:"lcm_idle_timeout_override"`
	LcmNightModeBegins     types.String         `tfsdk:"lcm_night_mode_begins"`
	LcmNightModeEnds       types.String         `tfsdk:"lcm_night_mode_ends"`

	// Outlet settings
	OutletOverrides types.List `tfsdk:"outlet_overrides"`
	OutletEnabled   types.Bool `tfsdk:"outlet_enabled"`

	// Management
	MgmtNetworkID types.String `tfsdk:"mgmt_network_id"`

	// Computed attributes
	Adopted types.Bool   `tfsdk:"adopted"`
	Model   types.String `tfsdk:"model"`
	Type    types.String `tfsdk:"type"`
	State   types.Int64  `tfsdk:"state"`

	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

// deviceKitFlatFields is the thirty scalar attributes, generated from
// provider-codegen/policy/device.json against the model struct and the SDK
// struct rather than transcribed.
//
// SIX LINES PER FIELD, which is the measured marginal cost of a flat field --
// dns_record's Fields block is 6.1 and this is 6.0. It is the denominator for
// whether the kit saves more than it adds.
//

// configNetworkToObject and configNetworkFromObject are ObjectField's Decode and
// Encode, transplanted from the hand-written pair with only the receiver and the
// SDK alias changed.
//
// THEY MOVE UNCHANGED BECAUSE config_network IS NOT A COLLECTION. The three
// collection shapes cannot be transplanted the same way: their mappers carry
// eleven `continue` statements meaning "drop this element and keep the rest",
// and a per-element Encode has no "keep going". See #221.
func configNetworkToObject(ctx context.Context, cn *ui.DeviceConfigNetwork) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics

	if cn == nil || (cn.Type == "" && cn.IP == "" && cn.Gateway == "" && cn.Netmask == "") {
		return types.ObjectNull(configNetworkAttrTypes()), diags
	}

	model := configNetworkModel{
		Type:           stringOrNull(cn.Type),
		IP:             stringOrNull(cn.IP),
		Netmask:        stringOrNull(cn.Netmask),
		Gateway:        stringOrNull(cn.Gateway),
		DNS1:           stringOrNull(cn.DNS1),
		DNS2:           stringOrNull(cn.DNS2),
		DNSsuffix:      stringOrNull(cn.DNSsuffix),
		BondingEnabled: types.BoolValue(cn.BondingEnabled),
	}

	objVal, objDiags := types.ObjectValueFrom(ctx, configNetworkAttrTypes(), model)
	diags.Append(objDiags...)
	return objVal, diags
}

func configNetworkFromObject(ctx context.Context, object types.Object) (*ui.DeviceConfigNetwork, diag.Diagnostics) {
	var diags diag.Diagnostics

	if object.IsNull() || object.IsUnknown() {
		return nil, diags
	}

	var model configNetworkModel
	diags.Append(object.As(ctx, &model, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return nil, diags
	}

	cn := &ui.DeviceConfigNetwork{
		Type:           model.Type.ValueString(),
		IP:             model.IP.ValueString(),
		Netmask:        model.Netmask.ValueString(),
		Gateway:        model.Gateway.ValueString(),
		DNS1:           model.DNS1.ValueString(),
		DNS2:           model.DNS2.ValueString(),
		DNSsuffix:      model.DNSsuffix.ValueString(),
		BondingEnabled: model.BondingEnabled.ValueBool(),
	}

	return cn, diags
}

// deviceKitBackend wires the SDK calls the kit drives.
//
// CREATE IS NOT A CREATE, and that is the surface's defining property rather
// than an awkwardness. A device is not made by Terraform; it is adopted. The
// closure looks the device up by MAC, waits for the controller to see it, and
// adopts it -- which is why Backend.Create takes a closure rather than a method
// name. The retry is the controller's own latency: a device informs on its own
// schedule and is not addressable until it does.
//
// UpdateDeviceFields EXISTS, so device takes the masked path rather than the
// whole-object one. That matters for #191: the mask names only what the
// descriptor manages, so a field the provider does not model is never on the
// wire, and a device with no port_override block cannot have its overrides
// blanked by an update that never mentions them.
func deviceKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.Device] {
	return resourcekit.Backend[ui.Device]{
		// CREATE IS A MASKED WRITE, NOT A POST. The device already exists and
		// already carries its config; BeforeSend has adopted it and filled in
		// the ID by the time this runs. Writing the whole object here would
		// assert a zero value for all 79 attributes the practitioner did not
		// set, over a live device.
		CreateFields: func(
			ctx context.Context, site string, in *ui.Device, fields ...string,
		) (*ui.Device, error) {
			created, err := client.UpdateDeviceFields(ctx, site, in, fields...)
			if err != nil {
				return nil, err
			}
			deviceRestoreCreateValues(created, in)
			return created, nil
		},
		Read: func(ctx context.Context, site, id string) (*ui.Device, error) {
			return client.GetDevice(ctx, site, id)
		},
		UpdateFields: func(ctx context.Context, site string, in *ui.Device, fields ...string) (*ui.Device, error) {
			return client.UpdateDeviceFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			device, err := client.GetDevice(ctx, site, id)
			if err != nil {
				return err
			}
			return client.ForgetDevice(ctx, site, device.MAC)
		},
		List: func(ctx context.Context, site string) ([]ui.Device, error) {
			return client.ListDevice(ctx, site)
		},
		GetID: func(s *ui.Device) string { return s.ID },
		SetID: func(s *ui.Device, id string) { s.ID = id },
	}
}

// deviceWaitForState polls until the device reaches targetState.
//
// Transcribed from the hand-written resource unchanged. The two error
// swallows are load-bearing and not defensive padding: a forgotten device
// disappears from the controller for a few seconds before it reappears, and
// during that window the lookup answers NotFound or api.err.UnknownDevice.
// Treating either as fatal would fail an adoption that is merely in progress.
func deviceWaitForState(
	ctx context.Context,
	client *ui.ApiClient,
	site, mac string,
	targetState ui.DeviceState,
	pendingStates []ui.DeviceState,
	timeout time.Duration,
) (*ui.Device, error) {
	// Always consider unknown to be a pending state.
	pendingStates = append(pendingStates, ui.DeviceStateUnknown)

	var pending []string
	for _, state := range pendingStates {
		pending = append(pending, state.String())
	}

	wait := retry.StateChangeConf{
		Pending: pending,
		Target:  []string{targetState.String()},
		Refresh: func() (any, string, error) {
			device, err := client.GetDeviceByMAC(ctx, site, mac)

			if _, ok := err.(*ui.NotFoundError); ok {
				err = nil
			}

			if err != nil && strings.Contains(err.Error(), "api.err.UnknownDevice") {
				err = nil
			}

			var state string
			if device != nil {
				state = device.State.String()
			}

			if device == nil {
				return nil, state, err
			}

			return device, state, err
		},
		Timeout:        timeout,
		NotFoundChecks: 30,
	}

	outputRaw, err := wait.WaitForStateContext(ctx)

	if output, ok := outputRaw.(*ui.Device); ok {
		return output, err
	}

	return nil, err
}

// deviceKitPrefetch hands BeforeSend the site, which is the one thing it needs
// and the one thing its signature does not carry.
//
// It deliberately does NO IO. Prefetch runs on read as well as write, and the
// only fetch device needs is keyed by MAC -- which is not known until BeforeSend
// has the SDK object. Doing a site-wide device list here to make one lookup
// available would turn every refresh into a full inventory call.
func deviceKitPrefetch() func(context.Context, string) (any, diag.Diagnostics) {
	return func(_ context.Context, site string) (any, diag.Diagnostics) {
		return site, nil
	}
}

// deviceKitBeforeSend adopts the device when creating, and merges port
// overrides on every write.
//
// WHY BOTH LIVE IN ONE HOOK: each needs the device the controller currently
// holds, and that is one fetch. The hand-written resource made the same single
// fetch for three reasons; two of them have since dissolved. It echoed `state`
// and `adopted` because it built a bare Device whose Go zero values would
// otherwise land in a whole-object diff and be rejected by UDM gateways
// (#177), and it copied `type` across because the PUT body required it. Under
// a field mask neither survives: the SDK object is built from the MODEL, which
// carries what the last read returned, so those fields are either absent from
// the mask or correct in it. buildMinimalUpdateDevice went with them.
//
// THE CREATE BRANCH IS KEYED ON AN EMPTY ID, because no hook parameter says
// which operation is running. That is the same discriminator the kit's own
// update path uses when it refuses a patch with an empty ID, and it is exactly
// right here: a device's ID is assigned by the controller, so the plan can only
// carry one after a create has already happened. Adopting on update would be a
// behaviour change -- the hand-written resource never re-adopts.
func deviceKitBeforeSend(
	client *ui.ApiClient,
) func(context.Context, *deviceKitModel, *deviceKitModel, *ui.Device, any) diag.Diagnostics {
	return func(
		ctx context.Context,
		_, effective *deviceKitModel,
		sdk *ui.Device,
		prefetched any,
	) diag.Diagnostics {
		var diags diag.Diagnostics

		site, _ := prefetched.(string)
		if sdk.MAC == "" {
			diags.AddError(
				"MAC Address Required",
				"No MAC address specified, please import the device using terraform import",
			)
			return diags
		}
		sdk.MAC = cleanMAC(sdk.MAC)
		mac := sdk.MAC

		creating := sdk.ID == ""

		var current *ui.Device
		if creating {
			// A device that has only just started informing is not there yet.
			err := retry.RetryContext(ctx, 2*time.Minute, func() *retry.RetryError {
				d, err := client.GetDeviceByMAC(ctx, site, mac)
				if err != nil {
					return retry.RetryableError(err)
				}
				current = d
				return nil
			})
			if err != nil {
				diags.AddError(
					"Error Reading Device",
					fmt.Sprintf("Could not read device with MAC %s: %s", mac, err),
				)
				return diags
			}
			if current == nil {
				diags.AddError(
					"Device Not Found",
					fmt.Sprintf("Device not found using mac %s", mac),
				)
				return diags
			}
		} else {
			// An update tolerates a failed lookup the way the hand-written
			// resource did: the merge below simply has nothing to preserve.
			current, _ = client.GetDeviceByMAC(ctx, site, mac)
		}

		if creating {
			if !current.Adopted {
				if !effective.AllowAdoption.ValueBool() {
					diags.AddError(
						"Device Not Adopted",
						"Device must be adopted before it can be managed",
					)
					return diags
				}
				if err := client.AdoptDevice(ctx, site, mac); err != nil {
					diags.AddError(
						"Error Adopting Device",
						fmt.Sprintf("Could not adopt device with MAC %s: %s", mac, err),
					)
					return diags
				}
				adopted, err := deviceWaitForState(
					ctx, client, site, mac,
					ui.DeviceStateConnected,
					[]ui.DeviceState{
						ui.DeviceStateAdopting,
						ui.DeviceStatePending,
						ui.DeviceStateProvisioning,
						ui.DeviceStateUpgrading,
					},
					3*time.Minute,
				)
				if err != nil {
					diags.AddError(
						"Error Waiting for Device Adoption",
						fmt.Sprintf("Could not wait for device adoption: %s", err),
					)
					return diags
				}
				current = adopted
			}
			sdk.ID = current.ID
		}

		// #266. The PUT replaces the whole array, so managing a subset of ports
		// without clobbering the rest means starting from what the controller
		// holds. Under a mask this runs only to be discarded when port_overrides
		// is not being written, which is why port_overrides is NOT in AlwaysWire:
		// forcing it into every mask would restore exactly the coupling the mask
		// removes.
		// port_overrides is not a Field, so ToSDK left it empty and the
		// declared blocks have to be encoded here.
		declared, d := devicePortOverridesFromModel(ctx, effective.PortOverride)
		diags.Append(d...)
		if diags.HasError() {
			return diags
		}
		sdk.PortOverrides = portOverridesForUpdate(
			current, deviceDedupePortOverrides(declared),
		)

		return diags
	}
}

// deviceKitCollectionFields are the three nested collections.
//

// deviceStringList and deviceInt64List build a nullable list attribute from an
// SDK slice. An empty slice is null, not an empty list, which is what the
// hand-written mappers did in each of their six copies.
func deviceStringList(values []string, sorted bool) (types.List, diag.Diagnostics) {
	if len(values) == 0 {
		return types.ListNull(types.StringType), nil
	}
	if sorted {
		values = append([]string(nil), values...)
		sort.Strings(values)
	}
	out := make([]attr.Value, 0, len(values))
	for _, v := range values {
		out = append(out, types.StringValue(v))
	}
	return types.ListValue(types.StringType, out)
}

func deviceInt64List(values []int64) (types.List, diag.Diagnostics) {
	if len(values) == 0 {
		return types.ListNull(types.Int64Type), nil
	}
	out := make([]attr.Value, 0, len(values))
	for _, v := range values {
		out = append(out, types.Int64Value(v))
	}
	return types.ListValue(types.Int64Type, out)
}

// devicePortOverrideField is port_override, the largest nested shape here: 46
// attributes over a set of blocks.
//
// THE NULL GUARDS ARE GONE AND THE BEHAVIOUR IS UNCHANGED. The hand-written
// encoder wrapped almost every assignment in `if !model.X.IsNull()`, which
// reads like it is protecting something but is not: ValueString() answers ""
// for a null string and ValueInt64Pointer() answers nil for a null number,
// which is exactly what the guarded branch would have left in the zero-valued
// struct. op_mode is the one real condition and it survives below.
// devicePortOverrideEncode and devicePortOverrideDecode map ONE port override
// block, and port_override is deliberately NOT a Field.
//
// ObjectSetField cannot express this surface, and it is worth saying why since
// the kind was built for it. A field kind decodes what the controller returned:
// every element in, every element out. port_override must do the opposite. The
// controller reports EVERY port on the switch with every attribute populated,
// and the practitioner configures a handful -- so state is rebuilt from the
// PRIOR set, taking API values only for the ports the configuration names and
// only for the attributes it set. A port the practitioner never mentioned must
// not enter state, or Terraform plans to remove ports it does not manage.
//
// That is the port_profile pattern: an attribute reconstructed on read by a
// hook rather than mapped by a Field. The kit already supports it, because the
// read path loads prior state into the model BEFORE ToModel runs, so an
// attribute no Field touches still holds its prior value at AfterReceive.
//
// The consequence for writes is that nothing in the plan can put port_overrides
// in the mask, which is exactly what AlwaysWire exists for.
func devicePortOverrideEncode(
	ctx context.Context, object types.Object,
) (ui.DevicePortOverrides, diag.Diagnostics) {
	var model portOverrideModel
	diags := object.As(ctx, &model, basetypes.ObjectAsOptions{})
	if diags.HasError() {
		return ui.DevicePortOverrides{}, diags
	}

	po := ui.DevicePortOverrides{
		PortIDX:           model.Index.ValueInt64Pointer(),
		Name:              model.Name.ValueString(),
		PortProfileID:     model.PortProfileID.ValueString(),
		PoeMode:           model.PoeMode.ValueString(),
		Dot1XCtrl:         model.Dot1XCtrl.ValueString(),
		FecMode:           model.FecMode.ValueString(),
		Forward:           model.Forward.ValueString(),
		NATiveNetworkID:   model.NativeNetworkID.ValueString(),
		SettingPreference: model.SettingPreference.ValueString(),
		StormctrlType:     model.StormctrlType.ValueString(),
		TaggedVLANMgmt:    model.TaggedVLANMgmt.ValueString(),
		VoiceNetworkID:    model.VoiceNetworkID.ValueString(),

		Autoneg:                      model.Autoneg.ValueBool(),
		EgressRateLimitKbpsEnabled:   model.EgressRateLimitKbpsEnabled.ValueBool(),
		FlowControlEnabled:           model.FlowControlEnabled.ValueBool(),
		FullDuplex:                   model.FullDuplex.ValueBool(),
		Isolation:                    model.Isolation.ValueBool(),
		LldpmedEnabled:               model.LldpmedEnabled.ValueBool(),
		LldpmedNotifyEnabled:         model.LldpmedNotifyEnabled.ValueBool(),
		PortKeepaliveEnabled:         model.PortKeepaliveEnabled.ValueBool(),
		PortSecurityEnabled:          model.PortSecurityEnabled.ValueBool(),
		StormctrlBroadcastastEnabled: model.StormctrlBroadcastEnabled.ValueBool(),
		StormctrlMcastEnabled:        model.StormctrlMcastEnabled.ValueBool(),
		StormctrlUcastEnabled:        model.StormctrlUcastEnabled.ValueBool(),
		StpPortMode:                  model.StpPortMode.ValueBool(),

		EgressRateLimitKbps:        model.EgressRateLimitKbps.ValueInt64Pointer(),
		MirrorPortIDX:              model.MirrorPortIDX.ValueInt64Pointer(),
		PriorityQueue1Level:        model.PriorityQueue1Level.ValueInt64Pointer(),
		PriorityQueue2Level:        model.PriorityQueue2Level.ValueInt64Pointer(),
		PriorityQueue3Level:        model.PriorityQueue3Level.ValueInt64Pointer(),
		PriorityQueue4Level:        model.PriorityQueue4Level.ValueInt64Pointer(),
		Speed:                      model.Speed.ValueInt64Pointer(),
		StormctrlBroadcastastLevel: model.StormctrlBroadcastLevel.ValueInt64Pointer(),
		StormctrlBroadcastastRate:  model.StormctrlBroadcastRate.ValueInt64Pointer(),
		StormctrlMcastLevel:        model.StormctrlMcastLevel.ValueInt64Pointer(),
		StormctrlMcastRate:         model.StormctrlMcastRate.ValueInt64Pointer(),
		StormctrlUcastLevel:        model.StormctrlUcastLevel.ValueInt64Pointer(),
		StormctrlUcastRate:         model.StormctrlUcastRate.ValueInt64Pointer(),
	}

	// op_mode is written ONLY for a non-default mode. Sending it on a
	// PUT for a gateway (UDM) is rejected (#213), and those ports never
	// run aggregate or mirror, so they stay at the "switch" default and
	// it is skipped. Writing it for the non-default cases is what makes
	// an SFP+ link aggregation actually engage (#177) -- otherwise
	// aggregate_members is sent without op_mode ever switching.
	if opMode := model.OpMode.ValueString(); opMode != "" && opMode != "switch" {
		po.OpMode = opMode
	}
	if !model.Dot1XIDleTimeout.IsNull() {
		po.Dot1XIDleTimeout = util.DurationUnitsPtr(model.Dot1XIDleTimeout, time.Second)
	}

	if !model.AggregateMembers.IsNull() {
		diags.Append(model.AggregateMembers.ElementsAs(ctx, &po.AggregateMembers, true)...)
	}
	if !model.ExcludedNetworkIDs.IsNull() {
		diags.Append(model.ExcludedNetworkIDs.ElementsAs(ctx, &po.ExcludedNetworkIDs, true)...)
	}
	if !model.MulticastRouterNetworkIDs.IsNull() {
		diags.Append(model.MulticastRouterNetworkIDs.ElementsAs(
			ctx, &po.MulticastRouterNetworkIDs, true)...)
	}
	if !model.PortSecurityMACAddress.IsNull() {
		diags.Append(model.PortSecurityMACAddress.ElementsAs(
			ctx, &po.PortSecurityMACAddress, true)...)
	}
	return po, diags

}

func devicePortOverrideDecode(
	ctx context.Context, po ui.DevicePortOverrides,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	model := portOverrideModel{
		Index:             types.Int64PointerValue(po.PortIDX),
		Name:              stringOrNull(po.Name),
		PortProfileID:     stringOrNull(po.PortProfileID),
		OpMode:            stringOrNull(po.OpMode),
		PoeMode:           stringOrNull(po.PoeMode),
		Dot1XCtrl:         stringOrNull(po.Dot1XCtrl),
		FecMode:           stringOrNull(po.FecMode),
		Forward:           stringOrNull(po.Forward),
		NativeNetworkID:   stringOrNull(po.NATiveNetworkID),
		SettingPreference: stringOrNull(po.SettingPreference),
		StormctrlType:     stringOrNull(po.StormctrlType),
		TaggedVLANMgmt:    stringOrNull(po.TaggedVLANMgmt),
		VoiceNetworkID:    stringOrNull(po.VoiceNetworkID),

		Autoneg:                    types.BoolValue(po.Autoneg),
		EgressRateLimitKbpsEnabled: types.BoolValue(po.EgressRateLimitKbpsEnabled),
		FlowControlEnabled:         types.BoolValue(po.FlowControlEnabled),
		FullDuplex:                 types.BoolValue(po.FullDuplex),
		Isolation:                  types.BoolValue(po.Isolation),
		LldpmedEnabled:             types.BoolValue(po.LldpmedEnabled),
		LldpmedNotifyEnabled:       types.BoolValue(po.LldpmedNotifyEnabled),
		PortKeepaliveEnabled:       types.BoolValue(po.PortKeepaliveEnabled),
		PortSecurityEnabled:        types.BoolValue(po.PortSecurityEnabled),
		StormctrlBroadcastEnabled:  types.BoolValue(po.StormctrlBroadcastastEnabled),
		StormctrlMcastEnabled:      types.BoolValue(po.StormctrlMcastEnabled),
		StormctrlUcastEnabled:      types.BoolValue(po.StormctrlUcastEnabled),
		StpPortMode:                types.BoolValue(po.StpPortMode),

		Dot1XIDleTimeout:        util.DurationPtrValue(po.Dot1XIDleTimeout, time.Second),
		EgressRateLimitKbps:     types.Int64PointerValue(po.EgressRateLimitKbps),
		MirrorPortIDX:           types.Int64PointerValue(po.MirrorPortIDX),
		PriorityQueue1Level:     types.Int64PointerValue(po.PriorityQueue1Level),
		PriorityQueue2Level:     types.Int64PointerValue(po.PriorityQueue2Level),
		PriorityQueue3Level:     types.Int64PointerValue(po.PriorityQueue3Level),
		PriorityQueue4Level:     types.Int64PointerValue(po.PriorityQueue4Level),
		Speed:                   types.Int64PointerValue(po.Speed),
		StormctrlBroadcastLevel: types.Int64PointerValue(po.StormctrlBroadcastastLevel),
		StormctrlBroadcastRate:  types.Int64PointerValue(po.StormctrlBroadcastastRate),
		StormctrlMcastLevel:     types.Int64PointerValue(po.StormctrlMcastLevel),
		StormctrlMcastRate:      types.Int64PointerValue(po.StormctrlMcastRate),
		StormctrlUcastLevel:     types.Int64PointerValue(po.StormctrlUcastLevel),
		StormctrlUcastRate:      types.Int64PointerValue(po.StormctrlUcastRate),

		// #235: the pinned SDK has no TaggedNetworkIDs field, so nothing
		// fills this in. Left as an untyped zero types.List it makes
		// ObjectValueFrom fail with "MISSING TYPE" against the schema's
		// ListAttribute, so it is explicitly a typed null.
		TaggedNetworkIDs: types.ListNull(types.StringType),
	}

	aggregate, d := deviceInt64List(po.AggregateMembers)
	diags.Append(d...)
	model.AggregateMembers = aggregate

	// Excluded networks are SORTED on the way in. The controller
	// returns them in an order of its own, and without this a stable
	// configuration produces a permanent diff.
	excluded, d := deviceStringList(po.ExcludedNetworkIDs, true)
	diags.Append(d...)
	model.ExcludedNetworkIDs = excluded

	multicast, d := deviceStringList(po.MulticastRouterNetworkIDs, false)
	diags.Append(d...)
	model.MulticastRouterNetworkIDs = multicast

	macs, d := deviceStringList(po.PortSecurityMACAddress, false)
	diags.Append(d...)
	model.PortSecurityMACAddress = macs

	if diags.HasError() {
		return types.ObjectNull(portOverrideAttrTypes()), diags
	}
	object, d := types.ObjectValueFrom(ctx, portOverrideAttrTypes(), model)
	diags.Append(d...)
	return object, diags

}

// deviceDedupePortOverrides keeps one block per port index, the last one wins.
//
// The hand-written encoder collected overrides into a map keyed by port index,
// so two blocks naming the same port silently collapsed to whichever the loop
// reached last. A set cannot hold two IDENTICAL blocks, but it holds two that
// name the same port and differ elsewhere quite happily, so dropping this
// would start sending both.
//
// Last-wins is preserved, but the ORDER is not the old one and could not be:
// the map was iterated to build the result, and Go randomises that, so the
// hand-written encoder emitted its overrides in a different order on every
// apply. mergePortOverridesByIndex says it appends in declared order "so the
// result is deterministic" -- it could not deliver that with a shuffled input.
// Set element order is the configuration's own, so it now can.
func deviceDedupePortOverrides(overrides []ui.DevicePortOverrides) []ui.DevicePortOverrides {
	seen := make(map[int64]int, len(overrides))
	out := make([]ui.DevicePortOverrides, 0, len(overrides))
	for _, po := range overrides {
		if po.PortIDX == nil {
			out = append(out, po)
			continue
		}
		if at, duplicate := seen[*po.PortIDX]; duplicate {
			out[at] = po
			continue
		}
		seen[*po.PortIDX] = len(out)
		out = append(out, po)
	}
	return out
}

// deviceReconcilePortOverrides rebuilds port_override state from the PRIOR set
// rather than from the controller's answer.
//
// Moved verbatim from the hand-written resource, which declared it on the
// resource receiver without ever using it. The logic is the whole reason
// port_override cannot be a Field: it walks the ports the practitioner
// configured, takes the API value only for attributes that were non-null in
// prior state, and keeps a port unchanged when the controller does not report
// it at all.
func deviceReconcilePortOverrides(
	ctx context.Context,
	prior types.Set,
	apiOverrides []ui.DevicePortOverrides,
) (types.Set, diag.Diagnostics) {
	var diags diag.Diagnostics

	// Build a map from port index → API port override for fast lookup.
	apiByIndex := make(map[int64]ui.DevicePortOverrides, len(apiOverrides))
	for _, po := range apiOverrides {
		if po.PortIDX != nil {
			apiByIndex[*po.PortIDX] = po
		}
	}

	// Iterate over the user-configured (prior) port overrides and rebuild each
	// one using values from the API response for the same port index.
	var priorModels []portOverrideModel
	diags.Append(prior.ElementsAs(ctx, &priorModels, false)...)
	if diags.HasError() {
		return prior, diags
	}

	elements := make([]attr.Value, 0, len(priorModels))
	for _, pm := range priorModels {
		idx := pm.Index.ValueInt64()
		apiPO, found := apiByIndex[idx]
		if !found {
			// Port not in API response — keep prior value unchanged.
			objVal, objDiags := types.ObjectValueFrom(ctx, pm.AttributeTypes(), pm)
			diags.Append(objDiags...)
			elements = append(elements, objVal)
			continue
		}

		// Build a new model seeded from the prior (user config), then update
		// only the fields that were explicitly set (non-null in prior) with
		// the actual API value so drift is visible.
		updated := pm

		if !pm.Name.IsNull() {
			if apiPO.Name == "" {
				updated.Name = types.StringNull()
			} else {
				updated.Name = types.StringValue(apiPO.Name)
			}
		}
		if !pm.NativeNetworkID.IsNull() {
			if apiPO.NATiveNetworkID == "" {
				updated.NativeNetworkID = types.StringNull()
			} else {
				updated.NativeNetworkID = types.StringValue(apiPO.NATiveNetworkID)
			}
		}
		if !pm.Forward.IsNull() {
			if apiPO.Forward == "" {
				updated.Forward = types.StringNull()
			} else {
				updated.Forward = types.StringValue(apiPO.Forward)
			}
		}
		if !pm.TaggedVLANMgmt.IsNull() {
			if apiPO.TaggedVLANMgmt == "" {
				updated.TaggedVLANMgmt = types.StringNull()
			} else {
				updated.TaggedVLANMgmt = types.StringValue(apiPO.TaggedVLANMgmt)
			}
		}
		if !pm.ExcludedNetworkIDs.IsNull() {
			if len(apiPO.ExcludedNetworkIDs) > 0 {
				sorted := make([]string, len(apiPO.ExcludedNetworkIDs))
				copy(sorted, apiPO.ExcludedNetworkIDs)
				sort.Strings(sorted)
				vals := make([]attr.Value, len(sorted))
				for i, id := range sorted {
					vals[i] = types.StringValue(id)
				}
				listVal, listDiags := types.ListValue(types.StringType, vals)
				diags.Append(listDiags...)
				updated.ExcludedNetworkIDs = listVal
			} else {
				emptyList, listDiags := types.ListValue(types.StringType, []attr.Value{})
				diags.Append(listDiags...)
				updated.ExcludedNetworkIDs = emptyList
			}
		}
		if !pm.PortProfileID.IsNull() {
			if apiPO.PortProfileID == "" {
				updated.PortProfileID = types.StringNull()
			} else {
				updated.PortProfileID = types.StringValue(apiPO.PortProfileID)
			}
		}

		objVal, objDiags := types.ObjectValueFrom(ctx, updated.AttributeTypes(), updated)
		diags.Append(objDiags...)
		elements = append(elements, objVal)
	}

	if diags.HasError() {
		return prior, diags
	}

	setValue, setDiags := types.SetValue(
		types.ObjectType{AttrTypes: portOverrideAttrTypes()},
		elements,
	)
	diags.Append(setDiags...)
	if diags.HasError() {
		return prior, diags
	}
	return setValue, diags
}

// devicePortOverridesFromModel encodes the declared blocks into SDK overrides.
func devicePortOverridesFromModel(
	ctx context.Context, set types.Set,
) ([]ui.DevicePortOverrides, diag.Diagnostics) {
	var diags diag.Diagnostics
	if set.IsNull() || set.IsUnknown() {
		return nil, diags
	}
	elements := set.Elements()
	out := make([]ui.DevicePortOverrides, 0, len(elements))
	for _, elem := range elements {
		object, ok := elem.(types.Object)
		if !ok {
			diags.Append(diag.NewErrorDiagnostic(
				"Invalid port override model",
				"Error casting `portOverrideModel` to `types.Object`",
			))
			continue
		}
		po, d := devicePortOverrideEncode(ctx, object)
		diags.Append(d...)
		if diags.HasError() {
			return nil, diags
		}
		out = append(out, po)
	}
	return out, diags
}

// deviceKitAfterReceive rebuilds port_override from prior state.
//
// A NULL PRIOR STAYS NULL. The practitioner who configured no port_override
// blocks must not have the controller's full port list written into their
// state, or the next plan proposes removing every port they never managed.
func deviceKitAfterReceive() func(
	context.Context, *ui.Device, *deviceKitModel, any,
) diag.Diagnostics {
	return func(
		ctx context.Context,
		sdk *ui.Device,
		model *deviceKitModel,
		_ any,
	) diag.Diagnostics {
		var diags diag.Diagnostics
		if model.PortOverride.IsNull() || model.PortOverride.IsUnknown() {
			return diags
		}
		reconciled, d := deviceReconcilePortOverrides(ctx, model.PortOverride, sdk.PortOverrides)
		diags.Append(d...)
		if !diags.HasError() {
			model.PortOverride = reconciled
		}
		return diags
	}
}

// deviceKitSpec is the whole of unifi_device's behaviour.
func deviceKitSpec() resourcekit.Spec[deviceKitModel, ui.Device] {
	return resourcekit.Spec[deviceKitModel, ui.Device]{
		TypeName: "device",
		Subject:  "Device",
		New:      func() *ui.Device { return &ui.Device{} },
		ID:       func(m *deviceKitModel) *types.String { return &m.ID },
		Site:     func(m *deviceKitModel) *types.String { return &m.Site },
		Timeouts: func(m *deviceKitModel) *timeouts.Value { return &m.Timeouts },
		// THE FIELD LIST IS ONE LITERAL BECAUSE AN INSTRUMENT READS IT.
		// internal descriptor checks parse this file rather than run it, so a
		// list assembled from helper calls at run time is invisible to them and
		// every field in it would read as missing.
		Fields: []resourcekit.Field[deviceKitModel, ui.Device]{
			resourcekit.BoolField[deviceKitModel, ui.Device]{
				Wire:  "adopted",
				Model: func(m *deviceKitModel) *types.Bool { return &m.Adopted },
				SDK:   func(s *ui.Device) *bool { return &s.Adopted },
			},
			resourcekit.StringField[deviceKitModel, ui.Device]{
				Wire:  "bandsteering_mode",
				Model: func(m *deviceKitModel) *types.String { return &m.BandsteeringMode },
				SDK:   func(s *ui.Device) *string { return &s.BandsteeringMode },
				Elide: resourcekit.NullZero,
			},
			resourcekit.BoolField[deviceKitModel, ui.Device]{
				Wire:  "disabled",
				Model: func(m *deviceKitModel) *types.Bool { return &m.Disabled },
				SDK:   func(s *ui.Device) *bool { return &s.Disabled },
			},
			resourcekit.BoolField[deviceKitModel, ui.Device]{
				Wire:  "flowctrl_enabled",
				Model: func(m *deviceKitModel) *types.Bool { return &m.FlowctrlEnabled },
				SDK:   func(s *ui.Device) *bool { return &s.FlowctrlEnabled },
			},
			resourcekit.BoolField[deviceKitModel, ui.Device]{
				Wire:  "jumboframe_enabled",
				Model: func(m *deviceKitModel) *types.Bool { return &m.JumboframeEnabled },
				SDK:   func(s *ui.Device) *bool { return &s.JumboframeEnabled },
			},
			resourcekit.Int64PtrField[deviceKitModel, ui.Device]{
				Wire:  "lcm_brightness",
				Model: func(m *deviceKitModel) *types.Int64 { return &m.LcmBrightness },
				SDK:   func(s *ui.Device) **int64 { return &s.LcmBrightness },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[deviceKitModel, ui.Device]{
				Wire:  "lcm_brightness_override",
				Model: func(m *deviceKitModel) *types.Bool { return &m.LcmBrightnessOverride },
				SDK:   func(s *ui.Device) *bool { return &s.LcmBrightnessOverride },
			},
			resourcekit.DurationPtrField[deviceKitModel, ui.Device]{
				Wire:  "lcm_idle_timeout",
				Model: func(m *deviceKitModel) *timetypes.GoDuration { return &m.LcmIDleTimeout },
				SDK:   func(s *ui.Device) **int64 { return &s.LcmIDleTimeout },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[deviceKitModel, ui.Device]{
				Wire:  "lcm_idle_timeout_override",
				Model: func(m *deviceKitModel) *types.Bool { return &m.LcmIDleTimeoutOverride },
				SDK:   func(s *ui.Device) *bool { return &s.LcmIDleTimeoutOverride },
			},
			resourcekit.StringField[deviceKitModel, ui.Device]{
				Wire:  "lcm_night_mode_begins",
				Model: func(m *deviceKitModel) *types.String { return &m.LcmNightModeBegins },
				SDK:   func(s *ui.Device) *string { return &s.LcmNightModeBegins },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[deviceKitModel, ui.Device]{
				Wire:  "lcm_night_mode_ends",
				Model: func(m *deviceKitModel) *types.String { return &m.LcmNightModeEnds },
				SDK:   func(s *ui.Device) *string { return &s.LcmNightModeEnds },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[deviceKitModel, ui.Device]{
				Wire:  "led_override",
				Model: func(m *deviceKitModel) *types.String { return &m.LedOverride },
				SDK:   func(s *ui.Device) *string { return &s.LedOverride },
				Elide: resourcekit.NullZero,
			},
			resourcekit.StringField[deviceKitModel, ui.Device]{
				Wire:  "led_override_color",
				Model: func(m *deviceKitModel) *types.String { return &m.LedOverrideColor },
				SDK:   func(s *ui.Device) *string { return &s.LedOverrideColor },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.Int64PtrField[deviceKitModel, ui.Device]{
				Wire:  "led_override_color_brightness",
				Model: func(m *deviceKitModel) *types.Int64 { return &m.LedOverrideColorBrightness },
				SDK:   func(s *ui.Device) **int64 { return &s.LedOverrideColorBrightness },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[deviceKitModel, ui.Device]{
				Wire:  "locked",
				Model: func(m *deviceKitModel) *types.Bool { return &m.Locked },
				SDK:   func(s *ui.Device) *bool { return &s.Locked },
			},
			resourcekit.StringLikeField[deviceKitModel, ui.Device, hwtypes.MACAddress]{
				Wire:  "mac",
				Model: func(m *deviceKitModel) *hwtypes.MACAddress { return &m.MAC },
				SDK:   func(s *ui.Device) *string { return &s.MAC },
			},
			resourcekit.BoolField[deviceKitModel, ui.Device]{
				Wire:  "mesh_sta_vap_enabled",
				Model: func(m *deviceKitModel) *types.Bool { return &m.MeshStaVapEnabled },
				SDK:   func(s *ui.Device) *bool { return &s.MeshStaVapEnabled },
			},
			resourcekit.StringField[deviceKitModel, ui.Device]{
				Wire:  "mgmt_network_id",
				Model: func(m *deviceKitModel) *types.String { return &m.MgmtNetworkID },
				SDK:   func(s *ui.Device) *string { return &s.MgmtNetworkID },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[deviceKitModel, ui.Device]{
				Wire:  "model",
				Model: func(m *deviceKitModel) *types.String { return &m.Model },
				SDK:   func(s *ui.Device) *string { return &s.Model },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[deviceKitModel, ui.Device]{
				Wire:  "name",
				Model: func(m *deviceKitModel) *types.String { return &m.Name },
				SDK:   func(s *ui.Device) *string { return &s.Name },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[deviceKitModel, ui.Device]{
				Wire:  "outdoor_mode_override",
				Model: func(m *deviceKitModel) *types.String { return &m.OutdoorModeOverride },
				SDK:   func(s *ui.Device) *string { return &s.OutdoorModeOverride },
				Elide: resourcekit.NullZero,
			},
			resourcekit.BoolField[deviceKitModel, ui.Device]{
				Wire:  "outlet_enabled",
				Model: func(m *deviceKitModel) *types.Bool { return &m.OutletEnabled },
				SDK:   func(s *ui.Device) *bool { return &s.OutletEnabled },
			},
			resourcekit.StringField[deviceKitModel, ui.Device]{
				Wire:  "poe_mode",
				Model: func(m *deviceKitModel) *types.String { return &m.PoeMode },
				SDK:   func(s *ui.Device) *string { return &s.PoeMode },
				Elide: resourcekit.NullZero,
			},
			resourcekit.Int64Field[deviceKitModel, ui.Device]{
				Wire:  "state",
				Model: func(m *deviceKitModel) *types.Int64 { return &m.State },
				SDK:   func(s *ui.Device) *int64 { return (*int64)(&s.State) },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.Int64PtrField[deviceKitModel, ui.Device]{
				Wire:  "stp_priority",
				Model: func(m *deviceKitModel) *types.Int64 { return &m.StpPriority },
				SDK:   func(s *ui.Device) **int64 { return &s.StpPriority },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[deviceKitModel, ui.Device]{
				Wire:  "stp_version",
				Model: func(m *deviceKitModel) *types.String { return &m.StpVersion },
				SDK:   func(s *ui.Device) *string { return &s.StpVersion },
				Elide: resourcekit.NullZero,
			},
			resourcekit.BoolField[deviceKitModel, ui.Device]{
				Wire:  "switch_vlan_enabled",
				Model: func(m *deviceKitModel) *types.Bool { return &m.SwitchVLANEnabled },
				SDK:   func(s *ui.Device) *bool { return &s.SwitchVLANEnabled },
			},
			resourcekit.StringField[deviceKitModel, ui.Device]{
				Wire:  "type",
				Model: func(m *deviceKitModel) *types.String { return &m.Type },
				SDK:   func(s *ui.Device) *string { return &s.Type },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.Int64PtrField[deviceKitModel, ui.Device]{
				Wire:  "volume",
				Model: func(m *deviceKitModel) *types.Int64 { return &m.Volume },
				SDK:   func(s *ui.Device) **int64 { return &s.Volume },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[deviceKitModel, ui.Device]{
				Wire:  "x_baresip_password",
				Model: func(m *deviceKitModel) *types.String { return &m.BaresipPassword },
				SDK:   func(s *ui.Device) *string { return &s.BaresipPassword },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.ObjectListField[deviceKitModel, ui.Device, ui.DeviceRadioTable]{
				Wire:      "radio_table",
				Model:     func(m *deviceKitModel) *types.List { return &m.RadioTable },
				SDK:       func(s *ui.Device) *[]ui.DeviceRadioTable { return &s.RadioTable },
				AttrTypes: radioTableAttrTypes(),
				Encode: func(
					ctx context.Context, object types.Object,
				) (ui.DeviceRadioTable, diag.Diagnostics) {
					var model radioTableModel
					diags := object.As(ctx, &model, basetypes.ObjectAsOptions{})
					if diags.HasError() {
						return ui.DeviceRadioTable{}, diags
					}
					return ui.DeviceRadioTable{
						Radio:                 model.Radio.ValueString(),
						Channel:               model.Channel.ValueString(),
						Ht:                    model.Ht.ValueInt64Pointer(),
						TxPower:               model.TxPower.ValueString(),
						TxPowerMode:           model.TxPowerMode.ValueString(),
						MinRssiEnabled:        model.MinRssiEnabled.ValueBool(),
						MinRssi:               model.MinRssi.ValueInt64Pointer(),
						AntennaGain:           model.AntennaGain.ValueInt64Pointer(),
						AntennaID:             model.AntennaID.ValueInt64Pointer(),
						Dfs:                   model.Dfs.ValueBool(),
						HardNoiseFloorEnabled: model.HardNoiseFloorEnabled.ValueBool(),
						LoadbalanceEnabled:    model.LoadbalanceEnabled.ValueBool(),
						Maxsta:                model.Maxsta.ValueInt64Pointer(),
						Name:                  model.Name.ValueString(),
						SensLevel:             model.SensLevel.ValueInt64Pointer(),
						SensLevelEnabled:      model.SensLevelEnabled.ValueBool(),
						VwireEnabled:          model.VwireEnabled.ValueBool(),
					}, diags
				},
				Decode: func(
					ctx context.Context, radio ui.DeviceRadioTable,
				) (types.Object, diag.Diagnostics) {
					return types.ObjectValueFrom(ctx, radioTableAttrTypes(), radioTableModel{
						Radio:                 stringOrNull(radio.Radio),
						Channel:               stringOrNull(radio.Channel),
						Ht:                    types.Int64PointerValue(radio.Ht),
						TxPower:               stringOrNull(radio.TxPower),
						TxPowerMode:           stringOrNull(radio.TxPowerMode),
						MinRssiEnabled:        types.BoolValue(radio.MinRssiEnabled),
						MinRssi:               types.Int64PointerValue(radio.MinRssi),
						AntennaGain:           types.Int64PointerValue(radio.AntennaGain),
						AntennaID:             types.Int64PointerValue(radio.AntennaID),
						Dfs:                   types.BoolValue(radio.Dfs),
						HardNoiseFloorEnabled: types.BoolValue(radio.HardNoiseFloorEnabled),
						LoadbalanceEnabled:    types.BoolValue(radio.LoadbalanceEnabled),
						Maxsta:                types.Int64PointerValue(radio.Maxsta),
						Name:                  stringOrNull(radio.Name),
						SensLevel:             types.Int64PointerValue(radio.SensLevel),
						SensLevelEnabled:      types.BoolValue(radio.SensLevelEnabled),
						VwireEnabled:          types.BoolValue(radio.VwireEnabled),
					})
				},
				Elide: resourcekit.KeepZero,
			},
			resourcekit.ObjectListField[deviceKitModel, ui.Device, ui.DeviceOutletOverrides]{
				Wire:      "outlet_overrides",
				Model:     func(m *deviceKitModel) *types.List { return &m.OutletOverrides },
				SDK:       func(s *ui.Device) *[]ui.DeviceOutletOverrides { return &s.OutletOverrides },
				AttrTypes: outletOverrideAttrTypes(),
				Encode: func(
					ctx context.Context, object types.Object,
				) (ui.DeviceOutletOverrides, diag.Diagnostics) {
					var model outletOverrideModel
					diags := object.As(ctx, &model, basetypes.ObjectAsOptions{})
					if diags.HasError() {
						return ui.DeviceOutletOverrides{}, diags
					}
					return ui.DeviceOutletOverrides{
						Index:        model.Index.ValueInt64Pointer(),
						Name:         model.Name.ValueString(),
						RelayState:   model.RelayState.ValueBool(),
						CycleEnabled: model.CycleEnabled.ValueBool(),
					}, diags
				},
				Decode: func(
					ctx context.Context, outlet ui.DeviceOutletOverrides,
				) (types.Object, diag.Diagnostics) {
					return types.ObjectValueFrom(ctx, outletOverrideAttrTypes(), outletOverrideModel{
						Index:        types.Int64PointerValue(outlet.Index),
						Name:         stringOrNull(outlet.Name),
						RelayState:   types.BoolValue(outlet.RelayState),
						CycleEnabled: types.BoolValue(outlet.CycleEnabled),
					})
				},
				Elide: resourcekit.KeepZero,
			},
			resourcekit.ObjectField[deviceKitModel, ui.Device, ui.DeviceConfigNetwork]{
				Wire:      "config_network",
				Model:     func(m *deviceKitModel) *types.Object { return &m.ConfigNetwork },
				SDK:       func(s *ui.Device) **ui.DeviceConfigNetwork { return &s.ConfigNetwork },
				AttrTypes: configNetworkAttrTypes(),
				Encode:    configNetworkFromObject,
				Decode:    configNetworkToObject,
				Elide:     resourcekit.KeepZero,
			},
		},

		Prefetch:     deviceKitPrefetch(),
		AfterReceive: deviceKitAfterReceive(),

		// port_overrides IS DERIVED, so nothing in the plan can put it in the
		// mask. It is not a Field -- see devicePortOverrideEncode for why the
		// read direction cannot be one -- so BeforeSend is what fills it in,
		// and this is what carries it to the wire.
		AlwaysWire: []string{"port_overrides"},

		// A device is hardware. Destroying the resource releases it from state;
		// forgetting the device is a separate, opt-in act.
		BeforeDelete: func(_ context.Context, model *deviceKitModel) (bool, diag.Diagnostics) {
			return model.ForgetOnDestroy.ValueBool(), nil
		},
	}
}

// deviceRestoreCreateValues fixes up what the controller answers with after a
// create, and it is create-only because Backend.CreateFields is.
//
// ADOPTED IS TRUE BECAUSE THE CREATE SUCCEEDED. BeforeSend adopts the device
// and waits for it to reach Connected before anything is written, so by the
// time there is a response the device IS adopted -- but the object the write
// answers with can still carry the pre-adoption flag. Letting that reach state
// leaves a device recorded as unadopted immediately after the apply that
// adopted it, and the next plan proposes adopting it again.
//
// The name is kept from the request for the same reason: it is what the
// practitioner asked for and what was just written.
//
// This cannot live in AfterReceive. That hook also runs on every read, where
// forcing adopted to true would hide a device that has genuinely been
// unadopted on the controller.
func deviceRestoreCreateValues(created, sent *ui.Device) {
	if created == nil {
		return
	}
	created.Adopted = true
	if sent != nil && sent.Name != "" {
		created.Name = sent.Name
	}
}
