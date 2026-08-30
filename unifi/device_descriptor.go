package unifi

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_device "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_device"
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

// configNetworkToObject and configNetworkFromObject are ObjectField's Decode
// and Encode for config_network.
func configNetworkToObject(ctx context.Context, cn *ui.DeviceConfigNetwork) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics

	if cn == nil || (cn.Type == "" && cn.IP == "" && cn.Gateway == "" && cn.Netmask == "") {
		return types.ObjectNull(configNetworkAttrTypes()), diags
	}

	model := configNetworkModel{
		Type:           util.StringValueOrNull(cn.Type),
		IP:             util.StringValueOrNull(cn.IP),
		Netmask:        util.StringValueOrNull(cn.Netmask),
		Gateway:        util.StringValueOrNull(cn.Gateway),
		DNS1:           util.StringValueOrNull(cn.DNS1),
		DNS2:           util.StringValueOrNull(cn.DNS2),
		DNSsuffix:      util.StringValueOrNull(cn.DNSsuffix),
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
// A device is adopted, not created -- Backend.Create's closure polls for the
// MAC then adopts it. Writes use UpdateDeviceFields (masked), so a field the
// provider doesn't model is never overwritten to zero.
func deviceKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.Device] {
	return resourcekit.Backend[ui.Device]{
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
// Swallowing NotFound and api.err.UnknownDevice below is required: a forgotten
// device briefly disappears before reappearing, and treating either as fatal
// would fail an adoption that is merely in progress.
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

// deviceKitPrefetch hands BeforeSend the site, the one thing it needs but its
// signature doesn't carry. It deliberately does no IO: Prefetch also runs on
// every read, so a site-wide device list here would turn each refresh into a
// full inventory call.
func deviceKitPrefetch() func(context.Context, string) (any, diag.Diagnostics) {
	return func(_ context.Context, site string) (any, diag.Diagnostics) {
		return site, nil
	}
}

// deviceKitBeforeSend adopts the device when creating, and writes declared
// port overrides through the keyed overlay.
func deviceKitBeforeSend(
	client *ui.ApiClient,
) func(context.Context, *deviceKitModel, *deviceKitModel, deviceKitModel, *ui.Device, any) diag.Diagnostics {
	return func(
		ctx context.Context,
		config, effective *deviceKitModel,
		_ deviceKitModel,
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

		// Empty ID means create: the controller assigns it, so the plan can
		// only carry one after a create has already run.
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
			// An update tolerates a failed lookup: current only feeds the type
			// echo below, and an update's state already carries its own type
			// from the last read.
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

		// type is echoed from the controller on create: it's Computed, so the
		// plan holds it unknown and the mask would otherwise omit it entirely.
		if sdk.Type == "" && current != nil {
			sdk.Type = current.Type
		}

		// port_overrides is not a Field and stays off AlwaysWire: it is
		// written through its own keyed overlay
		// (updateDevicePortOverridesGrouped), one UpdateDevicePortOverrides
		// call per distinct declared member set, never through the general
		// masked device write. Measured against a live controller: a single
		// call carrying every declared member forces each port's undeclared
		// members to their Go zero value, so ports with different declared
		// member sets can never share a call, and an update that declares no
		// port_override block sends no port-overrides call at all, which is
		// what leaves an unconfigured port untouched.
		declared, d := devicePortOverridesDeclaredFromConfig(ctx, config.PortOverride)
		diags.Append(d...)
		if diags.HasError() {
			return diags
		}
		declared = dedupeDeclaredPortOverrides(declared)
		// A block that declares no writable member -- port_override { index
		// = 1 }, or one naming only index and the default op_mode -- means
		// "manage this port, change nothing" under a masked write, so it is
		// dropped here rather than sent: UpdateDevicePortOverrides refuses an
		// empty mask outright, and letting that error through would abort
		// every other declared port's write too, mid-apply, after some of
		// them had already gone out.
		declared = declaredPortOverridesWithFields(declared)
		if len(declared) > 0 {
			if _, err := updateDevicePortOverridesGrouped(ctx, client, site, sdk, declared); err != nil {
				diags.AddError("Error Updating Port Overrides", err.Error())
				return diags
			}
		}

		return diags
	}
}

// devicePortOverrideField is port_override, the largest nested shape here: 46
// attributes over a set of blocks. devicePortOverrideEncode maps ONE block
// to the wire; port_override is deliberately not a Field.
//
// port_override can't be a Field: the controller reports every port fully
// populated, but state must keep only what the practitioner configured,
// rebuilt from prior by deviceReconcilePortOverrides below -- not decoded
// fresh, or Terraform proposes removing every unmanaged port.
//
// tagged_networkconf_ids is declarable-but-inert: this Encode never writes
// it, even though the SDK has DevicePortOverrides.TaggedNetworkIDs, because
// wiring only one direction would create a permanent diff.
func devicePortOverrideEncode(
	ctx context.Context, object types.Object,
) (ui.DevicePortOverrides, []string, diag.Diagnostics) {
	var model portOverrideModel
	diags := object.As(ctx, &model, basetypes.ObjectAsOptions{})
	if diags.HasError() {
		return ui.DevicePortOverrides{}, nil, diags
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

	// op_mode is written only for a non-default mode: the controller rejects
	// it on a gateway PUT when set to the default, and omitting it on a
	// non-default mode leaves link aggregation never engaging.
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

	fields := devicePortOverrideDeclaredFields(model)
	if model.Index.IsUnknown() {
		// An index Terraform has not resolved yet cannot address a real
		// port: ValueInt64Pointer() returns a pointer to 0 for an unknown
		// value rather than nil, so leaving this alone would aim the write
		// at port 0. Report nothing declared instead -- the caller drops an
		// empty-fields entry (see the F1 fix in deviceKitBeforeSend), and
		// the block takes effect on a later apply once index is known.
		po.PortIDX = nil
		fields = nil
	}
	return po, fields, diags
}

// devicePortOverrideDeclaredFields lists the wire names of the members this
// block's config actually set, for the masked write
// (updateDevicePortOverridesGrouped, driven from
// devicePortOverridesDeclaredFromConfig).
//
// It reads model's own null-ness and known-ness, not the po struct
// devicePortOverrideEncode just built: every member of
// ui.DevicePortOverrides sits at its Go zero value when the config left it
// alone, indistinguishable from a member the config set to that same zero
// value. Inferring "declared" from po would reintroduce, one layer up, the
// exact bug measured against a live controller: a member two ports
// disagree on gets forced to its zero value on whichever port didn't name
// it. Unknown is checked alongside null for the same reason resourcekit's
// own Field implementations do (field.go, object_field.go,
// conditional_field.go, elide_check.go): a value Terraform has not
// resolved yet has no value to write, and the unsafe ValueString/
// ValueBool/ValueInt64Pointer accessors return the Go zero for it just
// like they do for null.
//
// index and tagged_networkconf_ids never appear here: index addresses the
// entry rather than configuring it, and tagged_networkconf_ids is
// declarable-but-inert (see the comment on devicePortOverrideEncode).
//
// A member added to devicePortOverrideEncode without a matching declare()
// call here silently stops being writable through the masked path.
// Test_devicePortOverrideDeclaredFields_matchesEveryModeledMember pins the
// full set so that gap fails a test instead of shipping quietly.
func devicePortOverrideDeclaredFields(model portOverrideModel) []string {
	var fields []string
	declare := func(wire string, v attr.Value) {
		if !v.IsNull() && !v.IsUnknown() {
			fields = append(fields, wire)
		}
	}

	declare("name", model.Name)
	declare("portconf_id", model.PortProfileID)
	declare("poe_mode", model.PoeMode)
	declare("dot1x_ctrl", model.Dot1XCtrl)
	declare("fec_mode", model.FecMode)
	declare("forward", model.Forward)
	declare("native_networkconf_id", model.NativeNetworkID)
	declare("setting_preference", model.SettingPreference)
	declare("stormctrl_type", model.StormctrlType)
	declare("tagged_vlan_mgmt", model.TaggedVLANMgmt)
	declare("voice_networkconf_id", model.VoiceNetworkID)

	declare("autoneg", model.Autoneg)
	declare("egress_rate_limit_kbps_enabled", model.EgressRateLimitKbpsEnabled)
	declare("flow_control_enabled", model.FlowControlEnabled)
	declare("full_duplex", model.FullDuplex)
	declare("isolation", model.Isolation)
	declare("lldpmed_enabled", model.LldpmedEnabled)
	declare("lldpmed_notify_enabled", model.LldpmedNotifyEnabled)
	declare("port_keepalive_enabled", model.PortKeepaliveEnabled)
	declare("port_security_enabled", model.PortSecurityEnabled)
	declare("stormctrl_bcast_enabled", model.StormctrlBroadcastEnabled)
	declare("stormctrl_mcast_enabled", model.StormctrlMcastEnabled)
	declare("stormctrl_ucast_enabled", model.StormctrlUcastEnabled)
	declare("stp_port_mode", model.StpPortMode)

	declare("egress_rate_limit_kbps", model.EgressRateLimitKbps)
	declare("mirror_port_idx", model.MirrorPortIDX)
	declare("priority_queue1_level", model.PriorityQueue1Level)
	declare("priority_queue2_level", model.PriorityQueue2Level)
	declare("priority_queue3_level", model.PriorityQueue3Level)
	declare("priority_queue4_level", model.PriorityQueue4Level)
	declare("speed", model.Speed)
	declare("stormctrl_bcast_level", model.StormctrlBroadcastLevel)
	declare("stormctrl_bcast_rate", model.StormctrlBroadcastRate)
	declare("stormctrl_mcast_level", model.StormctrlMcastLevel)
	declare("stormctrl_mcast_rate", model.StormctrlMcastRate)
	declare("stormctrl_ucast_level", model.StormctrlUcastLevel)
	declare("stormctrl_ucast_rate", model.StormctrlUcastRate)

	// Same non-default rule devicePortOverrideEncode applies to op_mode
	// above: a config value of "switch" is the default and must stay off
	// the wire on gateway devices, so it isn't treated as declared either.
	// Unknown is excluded explicitly rather than through declare(): an
	// unknown ValueString() also returns "", so opMode != "" already
	// excludes it, but that's incidental to the != "" check rather than a
	// stated rule, and this makes it one.
	if !model.OpMode.IsUnknown() {
		if opMode := model.OpMode.ValueString(); opMode != "" && opMode != "switch" {
			fields = append(fields, "op_mode")
		}
	}
	declare("dot1x_idle_timeout", model.Dot1XIDleTimeout)
	declare("aggregate_members", model.AggregateMembers)
	declare("excluded_networkconf_ids", model.ExcludedNetworkIDs)
	declare("multicast_router_networkconf_ids", model.MulticastRouterNetworkIDs)
	declare("port_security_mac_address", model.PortSecurityMACAddress)

	return fields
}

// dedupeDeclaredPortOverrides keeps one declared entry per port index, the
// last one wins.
//
// A set won't catch two blocks that name the same port but differ elsewhere;
// without this dedup, both would be sent -- as two separate
// UpdateDevicePortOverrides calls if their declared fields differ, racing
// each other for the same port_idx.
func dedupeDeclaredPortOverrides(declared []declaredPortOverride) []declaredPortOverride {
	seen := make(map[int64]int, len(declared))
	out := make([]declaredPortOverride, 0, len(declared))
	for _, d := range declared {
		if d.Override.PortIDX == nil {
			out = append(out, d)
			continue
		}
		if at, duplicate := seen[*d.Override.PortIDX]; duplicate {
			out[at] = d
			continue
		}
		seen[*d.Override.PortIDX] = len(out)
		out = append(out, d)
	}
	return out
}

// declaredPortOverridesWithFields drops entries whose config declared no
// writable member: an unknown index also lands here, since
// devicePortOverrideEncode clears Fields for it rather than address a
// still-unresolved port (see the comment there). Writing nothing is the
// correct answer for "manage this port, change nothing" under a masked
// write, and it also has to happen before grouping -- an empty mask is not
// a valid UpdateDevicePortOverrides call, it is a refused one.
func declaredPortOverridesWithFields(declared []declaredPortOverride) []declaredPortOverride {
	out := make([]declaredPortOverride, 0, len(declared))
	for _, d := range declared {
		if len(d.Fields) == 0 {
			continue
		}
		out = append(out, d)
	}
	return out
}

// deviceReconcilePortOverrides rebuilds port_override state from the prior
// set, not the controller's answer: it keeps a port unchanged unless the
// practitioner declared it, and only overwrites attributes that were set in
// prior.
func deviceReconcilePortOverrides(
	ctx context.Context,
	prior types.Set,
	apiOverrides []ui.DevicePortOverrides,
) (types.Set, diag.Diagnostics) {
	var diags diag.Diagnostics

	apiByIndex := make(map[int64]ui.DevicePortOverrides, len(apiOverrides))
	for _, po := range apiOverrides {
		if po.PortIDX != nil {
			apiByIndex[*po.PortIDX] = po
		}
	}

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
				setVal, setDiags := types.SetValue(types.StringType, vals)
				diags.Append(setDiags...)
				updated.ExcludedNetworkIDs = setVal
			} else {
				emptySet, setDiags := types.SetValue(types.StringType, []attr.Value{})
				diags.Append(setDiags...)
				updated.ExcludedNetworkIDs = emptySet
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

// devicePortOverridesDeclaredFromConfig encodes port_override blocks
// together with the exact members each block's config named, for the
// masked write updateDevicePortOverridesGrouped performs.
//
// It reads config, not the plan/state effective carries: an Optional+Computed
// member (autoneg, stp_port_mode, op_mode, ...) can be non-null in state
// purely because a prior read filled it in, never because the practitioner
// wrote it -- treating that as "declared" would send it on every future
// update regardless of what config says, and, worse, would make two ports
// with different Optional+Computed history look like they declared
// different member sets when neither's config named the member at all.
// config carries none of that: an attribute the practitioner did not write
// is null there, full stop, which is exactly the "declared or not" signal
// the masked write needs -- see devicePortOverrideDeclaredFields.
func devicePortOverridesDeclaredFromConfig(
	ctx context.Context, config types.Set,
) ([]declaredPortOverride, diag.Diagnostics) {
	var diags diag.Diagnostics
	if config.IsNull() || config.IsUnknown() {
		return nil, diags
	}
	elements := config.Elements()
	out := make([]declaredPortOverride, 0, len(elements))
	for _, elem := range elements {
		object, ok := elem.(types.Object)
		if !ok {
			diags.Append(diag.NewErrorDiagnostic(
				"Invalid port override model",
				"Error casting `portOverrideModel` to `types.Object`",
			))
			continue
		}
		po, fields, d := devicePortOverrideEncode(ctx, object)
		diags.Append(d...)
		if diags.HasError() {
			return nil, diags
		}
		out = append(out, declaredPortOverride{Override: po, Fields: fields})
	}
	return out, diags
}

// deviceKitAfterReceive rebuilds port_override from prior state. A null prior
// stays null: writing the controller's full port list would make the next
// plan propose removing every port the practitioner never managed.
func deviceKitAfterReceive() func(
	context.Context, *ui.Device, *deviceKitModel, deviceKitModel, any,
) diag.Diagnostics {
	return func(
		ctx context.Context,
		sdk *ui.Device,
		model *deviceKitModel,
		_ deviceKitModel,
		_ any,
	) diag.Diagnostics {
		var diags diag.Diagnostics
		if model.PortOverride.IsNull() || model.PortOverride.IsUnknown() {
			// A create's zero model carries an untyped null here, which the
			// framework rejects with MISSING TYPE; give it the element type
			// explicitly (a real prior read already has one).
			if model.PortOverride.ElementType(ctx) == nil {
				model.PortOverride = types.SetNull(devicePortOverrideElementType(ctx))
			}
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

// sanitizeRadioForUpdate drops radio fields whose out-of-range values the
// controller rejects (400) on UDM/Dream Machine gateways. It runs on every
// update, not just ones touching radio settings, because radio_table
// (Optional+Computed) is in the write mask on every update once a device has
// been read, whether or not radios changed.
func sanitizeRadioForUpdate(radioName string, radio *ui.DeviceRadioTable) diag.Diagnostics {
	var diags diag.Diagnostics
	inRange := func(v *int64, lo, hi int64) bool { return v != nil && *v >= lo && *v <= hi }
	warnDropped := func(field string, v, lo, hi int64) {
		diags.AddWarning(
			"Radio field out of range -- not applied",
			fmt.Sprintf(
				"radio %q: %s=%d is outside the controller's valid range [%d,%d] and was dropped "+
					"from the update (the controller rejects out-of-range values with "+
					"api.err.InvalidPayload). The declared value will not take effect -- adjust it "+
					"to be within range.",
				radioName, field, v, lo, hi,
			),
		)
	}

	if radio.MinRssiEnabled && radio.MinRssi != nil && !inRange(radio.MinRssi, -90, -67) {
		warnDropped("min_rssi", *radio.MinRssi, -90, -67)
	}
	if !radio.MinRssiEnabled || !inRange(radio.MinRssi, -90, -67) {
		radio.MinRssi = nil
	}
	// maxsta has no enabled flag, so a controller-reported 0 flows back on
	// every update where the user never configured it -- that's the "unset"
	// sentinel, not a declared value, so only warn when it's genuinely set.
	if radio.Maxsta != nil && *radio.Maxsta != 0 && !inRange(radio.Maxsta, 1, 200) {
		warnDropped("maxsta", *radio.Maxsta, 1, 200)
	}
	if !inRange(radio.Maxsta, 1, 200) {
		radio.Maxsta = nil
	}
	if radio.SensLevelEnabled && radio.SensLevel != nil && !inRange(radio.SensLevel, -90, -50) {
		warnDropped("sens_level", *radio.SensLevel, -90, -50)
	}
	if !radio.SensLevelEnabled || !inRange(radio.SensLevel, -90, -50) {
		radio.SensLevel = nil
	}

	return diags
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
		// Fields is one literal because an instrument parses this file rather
		// than running it; assembling it via helper calls would make every
		// field in it read as missing.
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
			// OmitZero: Optional+Computed with no schema default and no
			// UseStateForUnknown -- an unset plan value is Unknown on
			// create, and ValueInt64Pointer() would force-emit the zero
			// the controller's pattern (1-100) rejects. Same class as
			// dtim_6e (R2-C Task 10b).
			resourcekit.Int64PtrField[deviceKitModel, ui.Device]{
				Wire:  "lcm_brightness",
				Model: func(m *deviceKitModel) *types.Int64 { return &m.LcmBrightness },
				SDK:   func(s *ui.Device) **int64 { return &s.LcmBrightness },
				Elide: resourcekit.KeepZero, OmitZero: true,
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
				Elide: resourcekit.NullZero,
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
			// New must be set here: ToModel calls it on every read and list, and
			// a nil New panics. ZeroReadProblems guards this by running every
			// field's ToModel against a zero object.
			resourcekit.StringLikeField[deviceKitModel, ui.Device, hwtypes.MACAddress]{
				Wire:  "mac",
				Model: func(m *deviceKitModel) *hwtypes.MACAddress { return &m.MAC },
				SDK:   func(s *ui.Device) *string { return &s.MAC },
				New: func(v basetypes.StringValue) hwtypes.MACAddress {
					return hwtypes.MACAddress{StringValue: v}
				},
				Elide: resourcekit.NullZero,
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
				Elide: resourcekit.NullZero,
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
					radio := ui.DeviceRadioTable{
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
					}
					diags.Append(sanitizeRadioForUpdate(radio.Radio, &radio)...)
					return radio, diags
				},
				Decode: func(
					ctx context.Context, radio ui.DeviceRadioTable,
				) (types.Object, diag.Diagnostics) {
					return types.ObjectValueFrom(ctx, radioTableAttrTypes(), radioTableModel{
						Radio:                 util.StringValueOrNull(radio.Radio),
						Channel:               util.StringValueOrNull(radio.Channel),
						Ht:                    types.Int64PointerValue(radio.Ht),
						TxPower:               util.StringValueOrNull(radio.TxPower),
						TxPowerMode:           util.StringValueOrNull(radio.TxPowerMode),
						MinRssiEnabled:        types.BoolValue(radio.MinRssiEnabled),
						MinRssi:               types.Int64PointerValue(radio.MinRssi),
						AntennaGain:           types.Int64PointerValue(radio.AntennaGain),
						AntennaID:             types.Int64PointerValue(radio.AntennaID),
						Dfs:                   types.BoolValue(radio.Dfs),
						HardNoiseFloorEnabled: types.BoolValue(radio.HardNoiseFloorEnabled),
						LoadbalanceEnabled:    types.BoolValue(radio.LoadbalanceEnabled),
						Maxsta:                types.Int64PointerValue(radio.Maxsta),
						Name:                  util.StringValueOrNull(radio.Name),
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
						Name:         util.StringValueOrNull(outlet.Name),
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

		// AlwaysWire only for values BeforeSend fills that no Field's plan
		// value could carry: type is Computed, so an unknown create-time plan
		// would otherwise omit it. port_overrides is not here -- it is never
		// part of the general masked write at all, see deviceKitBeforeSend.
		AlwaysWire: []string{"type"},

		// port_overrides round-trips (deviceReconcilePortOverrides on read,
		// updateDevicePortOverridesGrouped via deviceKitBeforeSend on write)
		// without ever being a Fields entry or an AlwaysWire name, so it
		// needs to be named here or TestEveryDescriptorAgreesWithItsSources
		// reads it as a managed mapping.json field this descriptor drops.
		MappedElsewhere: []string{"port_overrides"},

		// A device is hardware. Destroying the resource releases it from state;
		// forgetting the device is a separate, opt-in act.
		BeforeDelete: func(_ context.Context, model *deviceKitModel) (bool, diag.Diagnostics) {
			return model.ForgetOnDestroy.ValueBool(), nil
		},
	}
}

// deviceRestoreCreateValues fixes up what the controller answers with after a
// create, and it is create-only because Backend.CreateFields is: the response
// can still carry the pre-adoption flag even though BeforeSend already waited
// for Connected. This can't live in AfterReceive, which also runs on reads,
// where forcing Adopted true would hide a real unadoption.
func deviceRestoreCreateValues(created, sent *ui.Device) {
	if created == nil {
		return
	}
	created.Adopted = true
	if sent != nil && sent.Name != "" {
		created.Name = sent.Name
	}
}

// devicePortOverrideElementType reads the element type off the served schema
// rather than restating it: the generated schema is the one place the shape
// already lives, and a restatement here would be a second copy that drifts.
func devicePortOverrideElementType(ctx context.Context) attr.Type {
	block := resource_device.DeviceResourceSchema(ctx).Blocks["port_override"]
	if block != nil {
		if setType, ok := block.Type().(basetypes.SetType); ok {
			return setType.ElemType
		}
	}
	// Unreachable while the schema declares port_override as a set block; the
	// test comparing against the served schema fails before this can matter.
	return types.ObjectType{}
}
