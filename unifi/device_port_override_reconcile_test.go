package unifi

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// These tests drive deviceReconcilePortOverrides directly. The behaviour
// under test: a declared (non-null in prior) member takes the controller's
// answer, empty included -- measured live, UpdateDevicePortOverrides
// returned 200 for fec_mode, voice_networkconf_id,
// multicast_router_networkconf_ids, tagged_vlan_mgmt and stored none of
// them, and the old six-member reconcile kept the discarded values in
// state indefinitely.

func reconcileStringSetOf(t *testing.T, ids ...string) types.Set {
	t.Helper()
	vals := make([]attr.Value, len(ids))
	for i, id := range ids {
		vals[i] = types.StringValue(id)
	}
	set, d := types.SetValue(types.StringType, vals)
	if d.HasError() {
		t.Fatalf("building a string set: %v", d)
	}
	return set
}

func reconcileStringListOf(t *testing.T, ss ...string) types.List {
	t.Helper()
	vals := make([]attr.Value, len(ss))
	for i, s := range ss {
		vals[i] = types.StringValue(s)
	}
	list, d := types.ListValue(types.StringType, vals)
	if d.HasError() {
		t.Fatalf("building a string list: %v", d)
	}
	return list
}

func reconcileInt64ListOf(t *testing.T, ns ...int64) types.List {
	t.Helper()
	vals := make([]attr.Value, len(ns))
	for i, n := range ns {
		vals[i] = types.Int64Value(n)
	}
	list, d := types.ListValue(types.Int64Type, vals)
	if d.HasError() {
		t.Fatalf("building an int64 list: %v", d)
	}
	return list
}

// undeclaredPortOverride is a block that declares nothing but its index:
// scalar zero values are typed nulls already, but a zero types.Set/List
// carries no element type and the framework refuses it, so the collections
// need their nulls spelled out.
func undeclaredPortOverride(idx int64) portOverrideModel {
	return portOverrideModel{
		Index:                     types.Int64Value(idx),
		AggregateMembers:          types.ListNull(types.Int64Type),
		Dot1XIDleTimeout:          timetypes.NewGoDurationNull(),
		ExcludedNetworkIDs:        types.SetNull(types.StringType),
		MulticastRouterNetworkIDs: types.SetNull(types.StringType),
		PortSecurityMACAddress:    types.ListNull(types.StringType),
		TaggedNetworkIDs:          types.SetNull(types.StringType),
	}
}

// fullyDeclaredPortOverride declares every member. Bools are true so a
// zero-valued controller answer visibly flips them.
func fullyDeclaredPortOverride(t *testing.T, idx int64) portOverrideModel {
	t.Helper()
	return portOverrideModel{
		Index:                      types.Int64Value(idx),
		Name:                       types.StringValue("prior-name"),
		PortProfileID:              types.StringValue("prior-prof"),
		OpMode:                     types.StringValue("mirror"),
		PoeMode:                    types.StringValue("off"),
		AggregateMembers:           reconcileInt64ListOf(t, 1),
		Autoneg:                    types.BoolValue(true),
		Dot1XCtrl:                  types.StringValue("auto"),
		Dot1XIDleTimeout:           util.DurationValue(30, time.Second),
		EgressRateLimitKbps:        types.Int64Value(64),
		EgressRateLimitKbpsEnabled: types.BoolValue(true),
		ExcludedNetworkIDs:         reconcileStringSetOf(t, "net-prior"),
		FecMode:                    types.StringValue("rs-fec"),
		FlowControlEnabled:         types.BoolValue(true),
		Forward:                    types.StringValue("native"),
		FullDuplex:                 types.BoolValue(true),
		Isolation:                  types.BoolValue(true),
		LldpmedEnabled:             types.BoolValue(true),
		LldpmedNotifyEnabled:       types.BoolValue(true),
		MirrorPortIDX:              types.Int64Value(2),
		MulticastRouterNetworkIDs:  reconcileStringSetOf(t, "mcast-prior"),
		NativeNetworkID:            types.StringValue("native-prior"),
		PortKeepaliveEnabled:       types.BoolValue(true),
		PortSecurityEnabled:        types.BoolValue(true),
		PortSecurityMACAddress:     reconcileStringListOf(t, "aa:bb:cc:dd:ee:01"),
		PriorityQueue1Level:        types.Int64Value(1),
		PriorityQueue2Level:        types.Int64Value(2),
		PriorityQueue3Level:        types.Int64Value(3),
		PriorityQueue4Level:        types.Int64Value(4),
		SettingPreference:          types.StringValue("manual"),
		Speed:                      types.Int64Value(100),
		StormctrlBroadcastEnabled:  types.BoolValue(true),
		StormctrlBroadcastLevel:    types.Int64Value(31),
		StormctrlBroadcastRate:     types.Int64Value(32),
		StormctrlMcastEnabled:      types.BoolValue(true),
		StormctrlMcastLevel:        types.Int64Value(33),
		StormctrlMcastRate:         types.Int64Value(34),
		StormctrlType:              types.StringValue("level"),
		StormctrlUcastEnabled:      types.BoolValue(true),
		StormctrlUcastLevel:        types.Int64Value(35),
		StormctrlUcastRate:         types.Int64Value(36),
		StpPortMode:                types.BoolValue(true),
		TaggedNetworkIDs:           reconcileStringSetOf(t, "tagged-prior"),
		TaggedVLANMgmt:             types.StringValue("auto"),
		VoiceNetworkID:             types.StringValue("voice-prior"),
	}
}

func portOverrideSetOf(t *testing.T, models ...portOverrideModel) types.Set {
	t.Helper()
	ctx := context.Background()
	elems := make([]attr.Value, len(models))
	for i, m := range models {
		obj, d := types.ObjectValueFrom(ctx, portOverrideAttrTypes(), m)
		if d.HasError() {
			t.Fatalf("building a port_override object: %v", d)
		}
		elems[i] = obj
	}
	set, d := types.SetValue(types.ObjectType{AttrTypes: portOverrideAttrTypes()}, elems)
	if d.HasError() {
		t.Fatalf("building the port_override set: %v", d)
	}
	return set
}

func reconcileOnePort(
	t *testing.T, prior portOverrideModel, api []ui.DevicePortOverrides,
) types.Object {
	t.Helper()
	got, diags := deviceReconcilePortOverrides(
		context.Background(), portOverrideSetOf(t, prior), api)
	if diags.HasError() {
		t.Fatalf("reconciling: %v", diags)
	}
	elems := got.Elements()
	if len(elems) != 1 {
		t.Fatalf("reconciled to %d elements, want 1", len(elems))
	}
	obj, ok := elems[0].(types.Object)
	if !ok {
		t.Fatalf("reconciled element is %T, want types.Object", elems[0])
	}
	return obj
}

func assertPortOverrideEquals(t *testing.T, got types.Object, want portOverrideModel) {
	t.Helper()
	wantObj, d := types.ObjectValueFrom(context.Background(), portOverrideAttrTypes(), want)
	if d.HasError() {
		t.Fatalf("building the expected object: %v", d)
	}
	wantAttrs := wantObj.Attributes()
	for name, gotVal := range got.Attributes() {
		if !gotVal.Equal(wantAttrs[name]) {
			t.Errorf("%s = %s, want %s", name, gotVal, wantAttrs[name])
		}
	}
}

// Every declared member surfaces the controller's answer. The second loop
// is the completeness guard: the answer differs from prior on every
// reconcilable member, so a member the walk skips fails there by name
// instead of hiding behind the ones it does cover.
func Test_deviceReconcilePortOverrides_everyDeclaredMemberTakesTheAnswer(t *testing.T) {
	prior := fullyDeclaredPortOverride(t, 7)
	api := ui.DevicePortOverrides{
		PortIDX:                      ptrInt64(7),
		Name:                         "api-name",
		PortProfileID:                "api-prof",
		OpMode:                       "aggregate",
		PoeMode:                      "auto",
		AggregateMembers:             []int64{8, 9},
		Autoneg:                      false,
		Dot1XCtrl:                    "mac_based",
		Dot1XIDleTimeout:             ptrInt64(60),
		EgressRateLimitKbps:          ptrInt64(128),
		EgressRateLimitKbpsEnabled:   false,
		ExcludedNetworkIDs:           []string{"net-b", "net-a"}, // unsorted: reconcile sorts
		FecMode:                      "fc-fec",
		FlowControlEnabled:           false,
		Forward:                      "customize",
		FullDuplex:                   false,
		Isolation:                    false,
		LldpmedEnabled:               false,
		LldpmedNotifyEnabled:         false,
		MirrorPortIDX:                ptrInt64(3),
		MulticastRouterNetworkIDs:    []string{"mcast-b", "mcast-a"},
		NATiveNetworkID:              "native-api",
		PortKeepaliveEnabled:         false,
		PortSecurityEnabled:          false,
		PortSecurityMACAddress:       []string{"aa:bb:cc:dd:ee:02", "aa:bb:cc:dd:ee:03"},
		PriorityQueue1Level:          ptrInt64(11),
		PriorityQueue2Level:          ptrInt64(12),
		PriorityQueue3Level:          ptrInt64(13),
		PriorityQueue4Level:          ptrInt64(14),
		SettingPreference:            "auto",
		Speed:                        ptrInt64(1000),
		StormctrlBroadcastastEnabled: false,
		StormctrlBroadcastastLevel:   ptrInt64(41),
		StormctrlBroadcastastRate:    ptrInt64(42),
		StormctrlMcastEnabled:        false,
		StormctrlMcastLevel:          ptrInt64(43),
		StormctrlMcastRate:           ptrInt64(44),
		StormctrlType:                "rate",
		StormctrlUcastEnabled:        false,
		StormctrlUcastLevel:          ptrInt64(45),
		StormctrlUcastRate:           ptrInt64(46),
		StpPortMode:                  false,
		TaggedNetworkIDs:             []string{"tagged-api"},
		TaggedVLANMgmt:               "custom",
		VoiceNetworkID:               "voice-api",
	}

	want := portOverrideModel{
		Index:                      types.Int64Value(7),
		Name:                       types.StringValue("api-name"),
		PortProfileID:              types.StringValue("api-prof"),
		OpMode:                     types.StringValue("aggregate"),
		PoeMode:                    types.StringValue("auto"),
		AggregateMembers:           reconcileInt64ListOf(t, 8, 9),
		Autoneg:                    types.BoolValue(false),
		Dot1XCtrl:                  types.StringValue("mac_based"),
		Dot1XIDleTimeout:           util.DurationValue(60, time.Second),
		EgressRateLimitKbps:        types.Int64Value(128),
		EgressRateLimitKbpsEnabled: types.BoolValue(false),
		ExcludedNetworkIDs:         reconcileStringSetOf(t, "net-a", "net-b"),
		FecMode:                    types.StringValue("fc-fec"),
		FlowControlEnabled:         types.BoolValue(false),
		Forward:                    types.StringValue("customize"),
		FullDuplex:                 types.BoolValue(false),
		Isolation:                  types.BoolValue(false),
		LldpmedEnabled:             types.BoolValue(false),
		LldpmedNotifyEnabled:       types.BoolValue(false),
		MirrorPortIDX:              types.Int64Value(3),
		MulticastRouterNetworkIDs:  reconcileStringSetOf(t, "mcast-a", "mcast-b"),
		NativeNetworkID:            types.StringValue("native-api"),
		PortKeepaliveEnabled:       types.BoolValue(false),
		PortSecurityEnabled:        types.BoolValue(false),
		PortSecurityMACAddress:     reconcileStringListOf(t, "aa:bb:cc:dd:ee:02", "aa:bb:cc:dd:ee:03"),
		PriorityQueue1Level:        types.Int64Value(11),
		PriorityQueue2Level:        types.Int64Value(12),
		PriorityQueue3Level:        types.Int64Value(13),
		PriorityQueue4Level:        types.Int64Value(14),
		SettingPreference:          types.StringValue("auto"),
		Speed:                      types.Int64Value(1000),
		StormctrlBroadcastEnabled:  types.BoolValue(false),
		StormctrlBroadcastLevel:    types.Int64Value(41),
		StormctrlBroadcastRate:     types.Int64Value(42),
		StormctrlMcastEnabled:      types.BoolValue(false),
		StormctrlMcastLevel:        types.Int64Value(43),
		StormctrlMcastRate:         types.Int64Value(44),
		StormctrlType:              types.StringValue("rate"),
		StormctrlUcastEnabled:      types.BoolValue(false),
		StormctrlUcastLevel:        types.Int64Value(45),
		StormctrlUcastRate:         types.Int64Value(46),
		StpPortMode:                types.BoolValue(false),
		TaggedNetworkIDs:           reconcileStringSetOf(t, "tagged-prior"), // inert: stays prior
		TaggedVLANMgmt:             types.StringValue("custom"),
		VoiceNetworkID:             types.StringValue("voice-api"),
	}

	got := reconcileOnePort(t, prior, []ui.DevicePortOverrides{api})
	assertPortOverrideEquals(t, got, want)

	priorObj, d := types.ObjectValueFrom(context.Background(), portOverrideAttrTypes(), prior)
	if d.HasError() {
		t.Fatalf("building the prior object: %v", d)
	}
	priorAttrs := priorObj.Attributes()
	for name, gotVal := range got.Attributes() {
		// index addresses the entry; tagged_networkconf_ids is
		// declarable-but-inert and deliberately not read back.
		if name == "index" || name == "tagged_networkconf_ids" {
			continue
		}
		if gotVal.Equal(priorAttrs[name]) {
			t.Errorf("%s still holds the prior value %s -- not reconciled", name, gotVal)
		}
	}
}

// The measured discard: the controller returns the port entry with every
// member empty. Declared members must go null (empty for collections, the
// answer itself for bools, the default for op_mode) -- keeping prior is
// what parked discarded fec_mode/voice/mcast_router values in state.
func Test_deviceReconcilePortOverrides_discardedWriteSurfaces(t *testing.T) {
	prior := fullyDeclaredPortOverride(t, 7)
	api := ui.DevicePortOverrides{PortIDX: ptrInt64(7)}

	want := portOverrideModel{
		Index:                      types.Int64Value(7),
		Name:                       types.StringNull(),
		PortProfileID:              types.StringNull(),
		OpMode:                     types.StringValue("switch"),
		PoeMode:                    types.StringNull(),
		AggregateMembers:           reconcileInt64ListOf(t),
		Autoneg:                    types.BoolValue(false),
		Dot1XCtrl:                  types.StringNull(),
		Dot1XIDleTimeout:           timetypes.NewGoDurationNull(),
		EgressRateLimitKbps:        types.Int64Null(),
		EgressRateLimitKbpsEnabled: types.BoolValue(false),
		ExcludedNetworkIDs:         reconcileStringSetOf(t),
		FecMode:                    types.StringNull(),
		FlowControlEnabled:         types.BoolValue(false),
		Forward:                    types.StringNull(),
		FullDuplex:                 types.BoolValue(false),
		Isolation:                  types.BoolValue(false),
		LldpmedEnabled:             types.BoolValue(false),
		LldpmedNotifyEnabled:       types.BoolValue(false),
		MirrorPortIDX:              types.Int64Null(),
		MulticastRouterNetworkIDs:  reconcileStringSetOf(t),
		NativeNetworkID:            types.StringNull(),
		PortKeepaliveEnabled:       types.BoolValue(false),
		PortSecurityEnabled:        types.BoolValue(false),
		PortSecurityMACAddress:     reconcileStringListOf(t),
		PriorityQueue1Level:        types.Int64Null(),
		PriorityQueue2Level:        types.Int64Null(),
		PriorityQueue3Level:        types.Int64Null(),
		PriorityQueue4Level:        types.Int64Null(),
		SettingPreference:          types.StringNull(),
		Speed:                      types.Int64Null(),
		StormctrlBroadcastEnabled:  types.BoolValue(false),
		StormctrlBroadcastLevel:    types.Int64Null(),
		StormctrlBroadcastRate:     types.Int64Null(),
		StormctrlMcastEnabled:      types.BoolValue(false),
		StormctrlMcastLevel:        types.Int64Null(),
		StormctrlMcastRate:         types.Int64Null(),
		StormctrlType:              types.StringNull(),
		StormctrlUcastEnabled:      types.BoolValue(false),
		StormctrlUcastLevel:        types.Int64Null(),
		StormctrlUcastRate:         types.Int64Null(),
		StpPortMode:                types.BoolValue(false),
		TaggedNetworkIDs:           reconcileStringSetOf(t, "tagged-prior"), // inert: stays prior
		TaggedVLANMgmt:             types.StringNull(),
		VoiceNetworkID:             types.StringNull(),
	}

	got := reconcileOnePort(t, prior, []ui.DevicePortOverrides{api})
	assertPortOverrideEquals(t, got, want)

	// The three members the live measurement proved discarded, by name, so
	// this test fails legibly if any of them regresses to prior-keeping.
	attrs := got.Attributes()
	if !attrs["fec_mode"].IsNull() {
		t.Errorf("fec_mode = %s, want null after the controller stored nothing", attrs["fec_mode"])
	}
	if !attrs["voice_networkconf_id"].IsNull() {
		t.Errorf("voice_networkconf_id = %s, want null after the controller stored nothing",
			attrs["voice_networkconf_id"])
	}
	if mcast, ok := attrs["multicast_router_networkconf_ids"].(types.Set); !ok ||
		mcast.IsNull() || len(mcast.Elements()) != 0 {
		t.Errorf("multicast_router_networkconf_ids = %s, want an empty set after the "+
			"controller stored nothing", attrs["multicast_router_networkconf_ids"])
	}
}

// Undeclared members are the practitioner's "not managed": whatever the
// controller answers for them stays out of state.
func Test_deviceReconcilePortOverrides_undeclaredMembersStayUntouched(t *testing.T) {
	prior := undeclaredPortOverride(7)
	prior.Name = types.StringValue("prior-name")
	api := ui.DevicePortOverrides{
		PortIDX:                   ptrInt64(7),
		Name:                      "api-name",
		FecMode:                   "rs-fec",
		VoiceNetworkID:            "voice-api",
		MulticastRouterNetworkIDs: []string{"mcast-api"},
		Autoneg:                   true,
		Speed:                     ptrInt64(1000),
		Dot1XIDleTimeout:          ptrInt64(60),
		OpMode:                    "aggregate",
		AggregateMembers:          []int64{8, 9},
	}

	want := undeclaredPortOverride(7)
	want.Name = types.StringValue("api-name")
	got := reconcileOnePort(t, prior, []ui.DevicePortOverrides{api})
	assertPortOverrideEquals(t, got, want)
}

// A port the API answer omits entirely keeps its prior state verbatim --
// the pre-existing whitelist behaviour, pinned.
func Test_deviceReconcilePortOverrides_portMissingFromAnswerKeepsPrior(t *testing.T) {
	priorSet := portOverrideSetOf(t, fullyDeclaredPortOverride(t, 7))
	api := []ui.DevicePortOverrides{{PortIDX: ptrInt64(3), Name: "someone-else"}}

	got, diags := deviceReconcilePortOverrides(context.Background(), priorSet, api)
	if diags.HasError() {
		t.Fatalf("reconciling: %v", diags)
	}
	if !got.Equal(priorSet) {
		t.Errorf("reconciled = %s,\nwant the prior set unchanged %s", got, priorSet)
	}
}

// An unknown prior member counts as declared, exactly like the old
// six-member reconcile: an Optional+Computed member is unknown in a
// create's plan, and the controller's answer is what resolves it -- to a
// value, or to null/empty when the answer is empty.
func Test_deviceReconcilePortOverrides_unknownResolvesToTheAnswer(t *testing.T) {
	prior := undeclaredPortOverride(5)
	prior.Name = types.StringUnknown()
	prior.Autoneg = types.BoolUnknown()
	prior.Speed = types.Int64Unknown()
	prior.ExcludedNetworkIDs = types.SetUnknown(types.StringType)
	api := ui.DevicePortOverrides{
		PortIDX: ptrInt64(5),
		Name:    "api-name",
		Autoneg: true,
	}

	want := undeclaredPortOverride(5)
	want.Name = types.StringValue("api-name")
	want.Autoneg = types.BoolValue(true)
	want.Speed = types.Int64Null()
	want.ExcludedNetworkIDs = reconcileStringSetOf(t)
	got := reconcileOnePort(t, prior, []ui.DevicePortOverrides{api})
	assertPortOverrideEquals(t, got, want)
}
