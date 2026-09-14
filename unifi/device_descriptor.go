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
				diags.AddError("Error Updating Port Overrides", resourcekit.DiagErrorText(err))
				return diags
			}
		}

		// radio_table is written the same way, and for the same reason, but it
		// MERGES rather than replaces: UpdateDeviceRadioTable writes only the
		// named members of the declared radios and leaves every other member,
		// every other radio, and the members this client has no field for
		// (nss, radio_caps, max_txpower) untouched -- so there is no stored
		// array to read back and resend, unlike port_overrides. The grouping is
		// still needed: the SDK applies one member mask to every radio in a
		// call, sending a masked member at its Go zero for a radio that did not
		// declare it, and the controller's member-level merge would apply that
		// zero -- so radios with different declared member sets can never share
		// a call. Radios are addressed by name (wifi-ng and its kin), which the
		// config must carry; a block naming no writable member, or no name, is
		// dropped rather than sent.
		radios, rd := deviceRadiosDeclaredFromConfig(ctx, config.RadioTable)
		diags.Append(rd...)
		if diags.HasError() {
			return diags
		}
		radios = declaredRadiosWithFields(radios)
		if len(radios) > 0 {
			if _, err := updateDeviceRadioTableGrouped(ctx, client, site, sdk, radios); err != nil {
				diags.AddError("Error Updating Radio Table", resourcekit.DiagErrorText(err))
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
// wiring only one direction would create a permanent diff. Wiring both
// directions would not fix the practitioner-visible problem either: measured
// directly against this controller generation (bypassing this dead code
// path, straight through UpdateDevicePortOverrides), a value placed on the
// wire is accepted and discarded, and forward is additionally reverted to
// "all". See the tagged_networkconf_ids description in
// provider-codegen/policy/device.json and this resource's ValidateConfig
// (device_validate.go) for the practitioner-facing side of that finding.
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
		Dot1XCtrl:         model.Dot1xCtrl.ValueString(),
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
	if !model.MulticastRouterNetworkconfIDs.IsNull() {
		diags.Append(model.MulticastRouterNetworkconfIDs.ElementsAs(
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
	declare("dot1x_ctrl", model.Dot1xCtrl)
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
	declare("multicast_router_networkconf_ids", model.MulticastRouterNetworkconfIDs)
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
// practitioner declared it, and for a declared port reconciles every
// modelled member the practitioner set (non-null in prior) to the value the
// controller reported, so drift on any of them surfaces on the next plan.
// op_mode and tagged_networkconf_ids are the two members it handles
// differently -- see the loop body.
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

	// One reconcile per Go type, each guarded by the prior being non-null: a
	// member the practitioner never declared stays as it was, so an
	// Optional-only attribute the config left null does not gain a value and
	// diff forever. Reconciling every declared member (not just the six the
	// legacy pass covered) is safe because the capture records nothing
	// discarded on a port-override write (discarded.DevicePortOverrides = []),
	// so the value read back is the value the controller kept and a declared
	// member reading back changed is genuine drift. setting_preference owns no
	// sibling members on a device (ownership.Device is empty), so no member
	// needs a "controller derives this" carve-out.
	reconcileString := func(prior types.String, v string) types.String {
		if prior.IsNull() {
			return prior
		}
		if v == "" {
			return types.StringNull()
		}
		return types.StringValue(v)
	}
	reconcileBool := func(prior types.Bool, v bool) types.Bool {
		if prior.IsNull() {
			return prior
		}
		return types.BoolValue(v)
	}
	reconcileInt64 := func(prior types.Int64, v *int64) types.Int64 {
		if prior.IsNull() {
			return prior
		}
		return types.Int64PointerValue(v)
	}
	// The collection helpers assume a non-null prior; the call site guards it.
	// An empty API value yields an empty collection, not null, the way the
	// legacy excluded_networkconf_ids reconcile did. String sets are sorted
	// for a stable result (a set ignores order); lists keep the API's order,
	// since a list is order-sensitive and a reorder is real drift.
	reconcileStringSet := func(v []string) (types.Set, diag.Diagnostics) {
		sorted := append([]string(nil), v...)
		sort.Strings(sorted)
		vals := make([]attr.Value, len(sorted))
		for i, s := range sorted {
			vals[i] = types.StringValue(s)
		}
		return types.SetValue(types.StringType, vals)
	}
	reconcileStringList := func(v []string) (types.List, diag.Diagnostics) {
		vals := make([]attr.Value, len(v))
		for i, s := range v {
			vals[i] = types.StringValue(s)
		}
		return types.ListValue(types.StringType, vals)
	}
	reconcileInt64List := func(v []int64) (types.List, diag.Diagnostics) {
		vals := make([]attr.Value, len(v))
		for i, n := range v {
			vals[i] = types.Int64Value(n)
		}
		return types.ListValue(types.Int64Type, vals)
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

		updated.Name = reconcileString(pm.Name, apiPO.Name)
		updated.PortProfileID = reconcileString(pm.PortProfileID, apiPO.PortProfileID)
		updated.PoeMode = reconcileString(pm.PoeMode, apiPO.PoeMode)
		updated.Dot1xCtrl = reconcileString(pm.Dot1xCtrl, apiPO.Dot1XCtrl)
		updated.FecMode = reconcileString(pm.FecMode, apiPO.FecMode)
		updated.Forward = reconcileString(pm.Forward, apiPO.Forward)
		updated.NativeNetworkID = reconcileString(pm.NativeNetworkID, apiPO.NATiveNetworkID)
		updated.SettingPreference = reconcileString(pm.SettingPreference, apiPO.SettingPreference)
		updated.StormctrlType = reconcileString(pm.StormctrlType, apiPO.StormctrlType)
		updated.TaggedVLANMgmt = reconcileString(pm.TaggedVLANMgmt, apiPO.TaggedVLANMgmt)
		updated.VoiceNetworkID = reconcileString(pm.VoiceNetworkID, apiPO.VoiceNetworkID)

		updated.Autoneg = reconcileBool(pm.Autoneg, apiPO.Autoneg)
		updated.EgressRateLimitKbpsEnabled = reconcileBool(pm.EgressRateLimitKbpsEnabled, apiPO.EgressRateLimitKbpsEnabled)
		updated.FlowControlEnabled = reconcileBool(pm.FlowControlEnabled, apiPO.FlowControlEnabled)
		updated.FullDuplex = reconcileBool(pm.FullDuplex, apiPO.FullDuplex)
		updated.Isolation = reconcileBool(pm.Isolation, apiPO.Isolation)
		updated.LldpmedEnabled = reconcileBool(pm.LldpmedEnabled, apiPO.LldpmedEnabled)
		updated.LldpmedNotifyEnabled = reconcileBool(pm.LldpmedNotifyEnabled, apiPO.LldpmedNotifyEnabled)
		updated.PortKeepaliveEnabled = reconcileBool(pm.PortKeepaliveEnabled, apiPO.PortKeepaliveEnabled)
		updated.PortSecurityEnabled = reconcileBool(pm.PortSecurityEnabled, apiPO.PortSecurityEnabled)
		updated.StormctrlBroadcastEnabled = reconcileBool(pm.StormctrlBroadcastEnabled, apiPO.StormctrlBroadcastastEnabled)
		updated.StormctrlMcastEnabled = reconcileBool(pm.StormctrlMcastEnabled, apiPO.StormctrlMcastEnabled)
		updated.StormctrlUcastEnabled = reconcileBool(pm.StormctrlUcastEnabled, apiPO.StormctrlUcastEnabled)
		updated.StpPortMode = reconcileBool(pm.StpPortMode, apiPO.StpPortMode)

		updated.EgressRateLimitKbps = reconcileInt64(pm.EgressRateLimitKbps, apiPO.EgressRateLimitKbps)
		updated.MirrorPortIDX = reconcileInt64(pm.MirrorPortIDX, apiPO.MirrorPortIDX)
		updated.PriorityQueue1Level = reconcileInt64(pm.PriorityQueue1Level, apiPO.PriorityQueue1Level)
		updated.PriorityQueue2Level = reconcileInt64(pm.PriorityQueue2Level, apiPO.PriorityQueue2Level)
		updated.PriorityQueue3Level = reconcileInt64(pm.PriorityQueue3Level, apiPO.PriorityQueue3Level)
		updated.PriorityQueue4Level = reconcileInt64(pm.PriorityQueue4Level, apiPO.PriorityQueue4Level)
		updated.Speed = reconcileInt64(pm.Speed, apiPO.Speed)
		updated.StormctrlBroadcastLevel = reconcileInt64(pm.StormctrlBroadcastLevel, apiPO.StormctrlBroadcastastLevel)
		updated.StormctrlBroadcastRate = reconcileInt64(pm.StormctrlBroadcastRate, apiPO.StormctrlBroadcastastRate)
		updated.StormctrlMcastLevel = reconcileInt64(pm.StormctrlMcastLevel, apiPO.StormctrlMcastLevel)
		updated.StormctrlMcastRate = reconcileInt64(pm.StormctrlMcastRate, apiPO.StormctrlMcastRate)
		updated.StormctrlUcastLevel = reconcileInt64(pm.StormctrlUcastLevel, apiPO.StormctrlUcastLevel)
		updated.StormctrlUcastRate = reconcileInt64(pm.StormctrlUcastRate, apiPO.StormctrlUcastRate)

		if !pm.Dot1XIDleTimeout.IsNull() {
			updated.Dot1XIDleTimeout = util.DurationPtrValue(apiPO.Dot1XIDleTimeout, time.Second)
		}
		if !pm.ExcludedNetworkIDs.IsNull() {
			set, setDiags := reconcileStringSet(apiPO.ExcludedNetworkIDs)
			diags.Append(setDiags...)
			updated.ExcludedNetworkIDs = set
		}
		if !pm.MulticastRouterNetworkconfIDs.IsNull() {
			set, setDiags := reconcileStringSet(apiPO.MulticastRouterNetworkIDs)
			diags.Append(setDiags...)
			updated.MulticastRouterNetworkconfIDs = set
		}
		if !pm.PortSecurityMACAddress.IsNull() {
			list, listDiags := reconcileStringList(apiPO.PortSecurityMACAddress)
			diags.Append(listDiags...)
			updated.PortSecurityMACAddress = list
		}
		if !pm.AggregateMembers.IsNull() {
			list, listDiags := reconcileInt64List(apiPO.AggregateMembers)
			diags.Append(listDiags...)
			updated.AggregateMembers = list
		}

		// op_mode is Optional+Computed with a "switch" default (schema), so
		// state always carries a value and there is no null to guard on: read
		// it back unconditionally, unlike the declared members above. An
		// aggregate/mirror/routed port then becomes visible instead of state
		// permanently asserting the default. A plain switch port reports op_mode
		// off the wire (it is only sent when non-default -- see
		// devicePortOverrideEncode), so an empty read is normalised to the
		// default "switch" rather than a null that would diff against it.
		if apiPO.OpMode == "" {
			updated.OpMode = types.StringValue("switch")
		} else {
			updated.OpMode = types.StringValue(apiPO.OpMode)
		}

		// tagged_networkconf_ids stays out of the reconcile: it is
		// declarable-but-inert -- never written, so it has no round-trip.

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

// deviceKitAfterReceive rebuilds port_override and radio_table from prior
// state. A null prior stays null: writing the controller's full list would
// make the next plan propose removing every entry the practitioner never
// managed.
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
		} else {
			reconciled, d := deviceReconcilePortOverrides(ctx, model.PortOverride, sdk.PortOverrides)
			diags.Append(d...)
			if !diags.HasError() {
				model.PortOverride = reconciled
			}
		}

		if model.RadioTable.IsNull() || model.RadioTable.IsUnknown() {
			// radio_table is Optional+Computed, so an unconfigured create plans
			// it Unknown, and Terraform requires a known value after apply.
			// Resolve it to a typed null rather than the controller's whole
			// array: a null prior stays null, the way port_override does, so a
			// device with radios the practitioner never declared does not have
			// them all appear in state and then read as a removal on the next
			// plan. (port_override only reaches this branch already-null, since
			// it is Optional-only, so it can leave a real element type alone;
			// radio_table has to force the unknown to a known null.)
			model.RadioTable = types.ListNull(deviceRadioTableElementType(ctx))
		} else {
			reconciled, d := deviceReconcileRadioTable(ctx, model.RadioTable, sdk.RadioTable)
			diags.Append(d...)
			if !diags.HasError() {
				model.RadioTable = reconciled
			}
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
	// ht is one of a fixed set of channel widths, enforced at plan time by the
	// schema's OneOf validator, so the only out-of-set value that reaches here
	// is the 0 a plain ValueInt64Pointer produces for an omitted (Unknown)
	// Optional+Computed radio -- the "unset" sentinel, like maxsta's above. The
	// controller rejects a 0 (api.err.InvalidValue), so drop it and let the
	// controller keep its own width.
	if radio.Ht != nil && !validHt[*radio.Ht] {
		radio.Ht = nil
	}

	return diags
}

// validHt is the set of channel widths the controller accepts for
// radio_table.ht, the same set the schema's OneOf validator enforces.
var validHt = map[int64]bool{
	20: true, 40: true, 80: true, 160: true, 240: true, 320: true,
	1080: true, 2160: true, 4320: true,
}

// deviceRadioTableEncode maps ONE radio_table block to the SDK type and the
// wire names of the members its config actually set, for the merge write
// updateDeviceRadioTableGrouped performs. It is the radio_table counterpart of
// devicePortOverrideEncode, and reads the member list off the model's own
// null/unknown-ness rather than the encoded struct for the reason that
// function's declaredFields comment sets out: every ui.DeviceRadioTable member
// is omitempty, so a member the config left alone is indistinguishable, once
// encoded, from one set to that member's zero value.
//
// sanitizeRadioForUpdate runs here, the same pre-write guard the whole-array
// path used, and its ht drop stays: it nils the out-of-range and unset-
// sentinel values the controller rejects. Those never reach the mask anyway --
// the presence check drops an Unknown member, which is what a sentinel is --
// but the guard is kept both because the task asks for it and because it is
// what keeps a member the guard nil'd (a disabled min_rssi) out of the mask,
// so maskedBody omits it rather than sending an explicit null the whole-array
// path never sent.
func deviceRadioTableEncode(
	ctx context.Context, object types.Object,
) (ui.DeviceRadioTable, []string, diag.Diagnostics) {
	var model radioTableModel
	diags := object.As(ctx, &model, basetypes.ObjectAsOptions{})
	if diags.HasError() {
		return ui.DeviceRadioTable{}, nil, diags
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

	fields := deviceRadioTableDeclaredFields(model, radio)
	if model.Name.IsUnknown() {
		// A name Terraform has not resolved yet cannot address a radio:
		// ValueString() returns "" for it, and the controller refuses an entry
		// with no name. Report nothing declared instead -- the caller drops an
		// empty entry (declaredRadiosWithFields), and the block takes effect on
		// a later apply once name is known.
		radio.Name = ""
		fields = nil
	}
	return radio, fields, diags
}

// deviceRadioTableDeclaredFields lists the wire names of the members this
// radio block's config actually set, presence-encoded so a false/""/0 the
// practitioner wrote is representable. It reads the model's null/unknown-ness,
// not the encoded struct, for the reason devicePortOverrideDeclaredFields sets
// out.
//
// name never appears here: it addresses the radio rather than configuring it,
// and UpdateDeviceRadioTable carries the key itself, the way index does for a
// port override. dfs never appears either: the controller discards it on write
// (behavior.json's discarded set records it), so sending it changes nothing
// and reconciling it back would surface the controller's own value as drift
// against a declared one -- it is declarable-but-inert, the radio_table
// analogue of port_override's tagged_networkconf_ids.
//
// The four members sanitizeRadioForUpdate can nil (ht, min_rssi, maxsta,
// sens_level) are named only when a value survived the guard, so one the guard
// dropped is omitted rather than sent as an explicit null.
//
// A member added to deviceRadioTableEncode without a matching declare() call
// here silently stops being writable through the merge path.
// Test_deviceRadioTableDeclaredFields_matchesEveryModeledMember pins the full
// set so that gap fails a test instead of shipping quietly.
func deviceRadioTableDeclaredFields(model radioTableModel, radio ui.DeviceRadioTable) []string {
	var fields []string
	declare := func(wire string, v attr.Value) {
		if !v.IsNull() && !v.IsUnknown() {
			fields = append(fields, wire)
		}
	}
	// A pointer member the guard nil'd is not sent: the config declared it, but
	// the value did not survive sanitizeRadioForUpdate, so naming it would send
	// a null.
	declareSanitized := func(wire string, v attr.Value, kept *int64) {
		if !v.IsNull() && !v.IsUnknown() && kept != nil {
			fields = append(fields, wire)
		}
	}

	declare("radio", model.Radio)
	declare("channel", model.Channel)
	declareSanitized("ht", model.Ht, radio.Ht)
	declare("tx_power", model.TxPower)
	declare("tx_power_mode", model.TxPowerMode)
	declare("min_rssi_enabled", model.MinRssiEnabled)
	declareSanitized("min_rssi", model.MinRssi, radio.MinRssi)
	declare("antenna_gain", model.AntennaGain)
	declare("antenna_id", model.AntennaID)
	declare("hard_noise_floor_enabled", model.HardNoiseFloorEnabled)
	declare("loadbalance_enabled", model.LoadbalanceEnabled)
	declareSanitized("maxsta", model.Maxsta, radio.Maxsta)
	declareSanitized("sens_level", model.SensLevel, radio.SensLevel)
	declare("sens_level_enabled", model.SensLevelEnabled)
	declare("vwire_enabled", model.VwireEnabled)

	return fields
}

// deviceRadiosDeclaredFromConfig encodes radio_table blocks together with the
// exact members each block's config named, for the merge write
// updateDeviceRadioTableGrouped performs. It reads config, not the
// plan/state effective carries, for the reason
// devicePortOverridesDeclaredFromConfig sets out: an Optional+Computed member
// can be non-null in state purely because a prior read filled it in.
func deviceRadiosDeclaredFromConfig(
	ctx context.Context, config types.List,
) ([]declaredRadio, diag.Diagnostics) {
	var diags diag.Diagnostics
	if config.IsNull() || config.IsUnknown() {
		return nil, diags
	}
	elements := config.Elements()
	out := make([]declaredRadio, 0, len(elements))
	for _, elem := range elements {
		object, ok := elem.(types.Object)
		if !ok {
			diags.Append(diag.NewErrorDiagnostic(
				"Invalid radio table model",
				"Error casting `radioTableModel` to `types.Object`",
			))
			continue
		}
		radio, fields, d := deviceRadioTableEncode(ctx, object)
		diags.Append(d...)
		if diags.HasError() {
			return nil, diags
		}
		out = append(out, declaredRadio{Radio: radio, Fields: fields})
	}
	return out, diags
}

// dedupeDeclaredRadios keeps one declared entry per radio name, the last one
// wins. radio_table is a list, so two blocks can name the same radio; sending
// both as one array would be ambiguous to a controller that keys on name.
// An entry with no name (an unknown, dropped downstream) is left alone.
func dedupeDeclaredRadios(declared []declaredRadio) []declaredRadio {
	seen := make(map[string]int, len(declared))
	out := make([]declaredRadio, 0, len(declared))
	for _, d := range declared {
		if d.Radio.Name == "" {
			out = append(out, d)
			continue
		}
		if at, duplicate := seen[d.Radio.Name]; duplicate {
			out[at] = d
			continue
		}
		seen[d.Radio.Name] = len(out)
		out = append(out, d)
	}
	return out
}

// declaredRadiosWithFields drops entries that name no writable member, or no
// radio at all: an unknown name lands here too, since deviceRadioTableEncode
// clears Fields for it. Writing nothing is the correct answer for "manage this
// radio, change nothing" under a merge, and it has to happen before grouping --
// an empty mask is not a valid UpdateDeviceRadioTable call, it is a refused one.
func declaredRadiosWithFields(declared []declaredRadio) []declaredRadio {
	declared = dedupeDeclaredRadios(declared)
	out := make([]declaredRadio, 0, len(declared))
	for _, d := range declared {
		if len(d.Fields) == 0 || d.Radio.Name == "" {
			continue
		}
		out = append(out, d)
	}
	return out
}

// deviceReconcileRadioTable rebuilds radio_table state from the prior list, not
// the controller's whole array: it keeps a radio unchanged unless the
// practitioner declared it, and for a declared radio reconciles every modelled
// member the practitioner set (non-null in prior) to the value the controller
// reported, so drift surfaces on the next plan. It is keyed by name, the
// controller's own radio key. It mirrors deviceReconcilePortOverrides, with two
// differences the capture dictates: dfs is read back only to resolve an unset
// Computed value, never over a set one (the controller discards a written dfs,
// so reading it back would diff forever), and there is no whole-collection
// carve-out like op_mode because radio_table carries no such default.
func deviceReconcileRadioTable(
	ctx context.Context,
	prior types.List,
	apiRadios []ui.DeviceRadioTable,
) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics

	apiByName := make(map[string]ui.DeviceRadioTable, len(apiRadios))
	for _, r := range apiRadios {
		if r.Name != "" {
			apiByName[r.Name] = r
		}
	}

	var priorModels []radioTableModel
	diags.Append(prior.ElementsAs(ctx, &priorModels, false)...)
	if diags.HasError() {
		return prior, diags
	}

	// A member the practitioner never declared stays as it was, so an
	// Optional-only value the config left null does not gain one and diff
	// forever. An Optional+Computed member left unset is Unknown rather than
	// null, and reconcileX below reads the controller's value for it (Unknown
	// is not null), which resolves the Computed without inventing drift.
	reconcileString := func(prior types.String, v string) types.String {
		if prior.IsNull() {
			return prior
		}
		if v == "" {
			return types.StringNull()
		}
		return types.StringValue(v)
	}
	reconcileBool := func(prior types.Bool, v bool) types.Bool {
		if prior.IsNull() {
			return prior
		}
		return types.BoolValue(v)
	}
	reconcileInt64 := func(prior types.Int64, v *int64) types.Int64 {
		if prior.IsNull() {
			return prior
		}
		return types.Int64PointerValue(v)
	}

	elements := make([]attr.Value, 0, len(priorModels))
	for _, pm := range priorModels {
		name := pm.Name.ValueString()
		apiR, found := apiByName[name]
		if !found {
			// Radio not in the API response -- keep the prior value unchanged.
			objVal, objDiags := types.ObjectValueFrom(ctx, radioTableAttrTypes(), pm)
			diags.Append(objDiags...)
			elements = append(elements, objVal)
			continue
		}

		updated := pm

		updated.Radio = reconcileString(pm.Radio, apiR.Radio)
		updated.Channel = reconcileString(pm.Channel, apiR.Channel)
		updated.TxPower = reconcileString(pm.TxPower, apiR.TxPower)
		updated.TxPowerMode = reconcileString(pm.TxPowerMode, apiR.TxPowerMode)
		updated.Name = reconcileString(pm.Name, apiR.Name)

		updated.MinRssiEnabled = reconcileBool(pm.MinRssiEnabled, apiR.MinRssiEnabled)
		updated.HardNoiseFloorEnabled = reconcileBool(pm.HardNoiseFloorEnabled, apiR.HardNoiseFloorEnabled)
		updated.LoadbalanceEnabled = reconcileBool(pm.LoadbalanceEnabled, apiR.LoadbalanceEnabled)
		updated.SensLevelEnabled = reconcileBool(pm.SensLevelEnabled, apiR.SensLevelEnabled)
		updated.VwireEnabled = reconcileBool(pm.VwireEnabled, apiR.VwireEnabled)

		updated.Ht = reconcileInt64(pm.Ht, apiR.Ht)
		updated.MinRssi = reconcileInt64(pm.MinRssi, apiR.MinRssi)
		updated.AntennaGain = reconcileInt64(pm.AntennaGain, apiR.AntennaGain)
		updated.AntennaID = reconcileInt64(pm.AntennaID, apiR.AntennaID)
		updated.Maxsta = reconcileInt64(pm.Maxsta, apiR.Maxsta)
		updated.SensLevel = reconcileInt64(pm.SensLevel, apiR.SensLevel)

		// dfs is discarded on write (behavior.json), so a value the practitioner
		// set must not be overwritten by the controller's own -- that would diff
		// forever. It is Optional+Computed, though, so an unset one is Unknown
		// and has to resolve to a known value; take the controller's only then.
		if pm.Dfs.IsUnknown() {
			updated.Dfs = types.BoolValue(apiR.Dfs)
		} else {
			updated.Dfs = pm.Dfs
		}

		objVal, objDiags := types.ObjectValueFrom(ctx, radioTableAttrTypes(), updated)
		diags.Append(objDiags...)
		elements = append(elements, objVal)
	}

	if diags.HasError() {
		return prior, diags
	}

	listValue, listDiags := types.ListValue(
		types.ObjectType{AttrTypes: radioTableAttrTypes()},
		elements,
	)
	diags.Append(listDiags...)
	if diags.HasError() {
		return prior, diags
	}
	return listValue, diags
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
		Fields: resourcekit.Override(deviceGenFields(), []resourcekit.Field[deviceKitModel, ui.Device]{
			resourcekit.DurationPtrField[deviceKitModel, ui.Device]{
				Wire:  "lcm_idle_timeout",
				Model: func(m *deviceKitModel) *timetypes.GoDuration { return &m.LcmIdleTimeout },
				SDK:   func(s *ui.Device) **int64 { return &s.LcmIDleTimeout },
				Elide: resourcekit.KeepZero,
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
			resourcekit.Int64Field[deviceKitModel, ui.Device]{
				Wire:  "state",
				Model: func(m *deviceKitModel) *types.Int64 { return &m.State },
				SDK:   func(s *ui.Device) *int64 { return (*int64)(&s.State) },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[deviceKitModel, ui.Device]{
				Wire:  "type",
				Model: func(m *deviceKitModel) *types.String { return &m.Type },
				SDK:   func(s *ui.Device) *string { return &s.Type },
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
		}),

		Prefetch:     deviceKitPrefetch(),
		AfterReceive: deviceKitAfterReceive(),

		// AlwaysWire only for values BeforeSend fills that no Field's plan
		// value could carry: type is Computed, so an unknown create-time plan
		// would otherwise omit it. port_overrides is not here -- it is never
		// part of the general masked write at all, see deviceKitBeforeSend.
		AlwaysWire: []string{"type"},

		// port_overrides and radio_table round-trip (deviceReconcile* on read,
		// updateDevice*Grouped via deviceKitBeforeSend on write) without ever
		// being a Fields entry or an AlwaysWire name, so they need to be named
		// here or TestEveryDescriptorAgreesWithItsSources reads them as managed
		// mapping.json fields this descriptor drops. radio_table left the field
		// list because its general masked write is a whole-array replace: the
		// generated members are omitempty, so it drops both a member sitting at
		// its zero value and the ones this client has no field for at all (nss,
		// radio_caps, max_txpower), clobbering what the AP reports about its own
		// hardware. UpdateDeviceRadioTable writes only the named members of the
		// declared radios and merges the rest -- see deviceKitBeforeSend.
		MappedElsewhere: []string{"port_overrides", "radio_table"},

		// port_override and radio_table are not Fields, so copyUncoveredPlanValues
		// would copy the raw plan over them on create and update -- clobbering what
		// deviceReconcilePortOverrides / deviceReconcileRadioTable (AfterReceive)
		// just rebuilt from the controller's answer. Marking them hook-owned keeps
		// the reconcile as the sole authority for these attributes on every code
		// path.
		HookOwned: []func(*deviceKitModel) any{
			func(m *deviceKitModel) any { return &m.PortOverride },
			func(m *deviceKitModel) any { return &m.RadioTable },
		},

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

// deviceRadioTableElementType reads the element type off the served schema
// rather than restating it, the same way devicePortOverrideElementType does --
// radio_table is a list ATTRIBUTE, not a block, so it comes off Attributes.
func deviceRadioTableElementType(ctx context.Context) attr.Type {
	attribute := resource_device.DeviceResourceSchema(ctx).Attributes["radio_table"]
	if attribute != nil {
		if listType, ok := attribute.GetType().(basetypes.ListType); ok {
			return listType.ElemType
		}
	}
	// Unreachable while the schema declares radio_table as a list attribute;
	// the test comparing against the served schema fails before this matters.
	return types.ObjectType{AttrTypes: radioTableAttrTypes()}
}
