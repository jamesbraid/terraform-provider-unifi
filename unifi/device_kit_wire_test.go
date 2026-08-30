package unifi

import (
	"encoding/json"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	unifi "github.com/ubiquiti-community/go-unifi/unifi"
)

// unifi_device sends a field mask rather than a hand-built PUT body, so the
// question these guard is "is this field named in the mask" -- a field the
// mask doesn't name is filtered out of the body by the SDK regardless of
// what the struct holds.
func deviceMaskFor(t *testing.T, plan deviceKitModel) []string {
	t.Helper()
	mask, err := deviceKitSpec().WireFields(&plan)
	if err != nil {
		t.Fatalf("WireFields: %v", err)
	}
	return mask
}

func deviceMaskHas(mask []string, name string) bool {
	for _, got := range mask {
		if got == name {
			return true
		}
	}
	return false
}

// A configured mgmt_network_id (the UI "Network Override") must reach the
// controller, and a null one must stay off the wire so it never
// reintroduces the zero-value rejection.
func Test_deviceMask_mgmtNetworkID(t *testing.T) {
	t.Run("configured mgmt_network_id is named in the mask", func(t *testing.T) {
		mask := deviceMaskFor(t, deviceKitModel{
			MgmtNetworkID: types.StringValue("net-mgmt"),
		})
		if !deviceMaskHas(mask, "mgmt_network_id") {
			t.Errorf("mask = %v, want it to name mgmt_network_id (override dropped)", mask)
		}
	})
	t.Run("null mgmt_network_id stays off the wire", func(t *testing.T) {
		mask := deviceMaskFor(t, deviceKitModel{})
		if deviceMaskHas(mask, "mgmt_network_id") {
			t.Errorf("a null mgmt_network_id was named in the mask: %v", mask)
		}
	})
}

// The switch_vlan_enabled bug class: a configured "Port VLAN" toggle must reach
// the controller, else it keeps its old value and the post-apply read conflicts
// with the plan.
func Test_deviceMask_switchVLANEnabled(t *testing.T) {
	t.Run("configured switch_vlan_enabled is named in the mask", func(t *testing.T) {
		mask := deviceMaskFor(t, deviceKitModel{
			SwitchVLANEnabled: types.BoolValue(true),
		})
		if !deviceMaskHas(mask, "switch_vlan_enabled") {
			t.Errorf("mask = %v, want it to name switch_vlan_enabled", mask)
		}
	})
	t.Run("an unset switch_vlan_enabled stays off the wire", func(t *testing.T) {
		mask := deviceMaskFor(t, deviceKitModel{})
		if deviceMaskHas(mask, "switch_vlan_enabled") {
			t.Errorf("an unset switch_vlan_enabled was named in the mask: %v", mask)
		}
	})
}

func Test_deviceMask_meshStaVapEnabled(t *testing.T) {
	t.Run("configured mesh_sta_vap_enabled is named in the mask", func(t *testing.T) {
		mask := deviceMaskFor(t, deviceKitModel{
			MeshStaVapEnabled: types.BoolValue(true),
		})
		if !deviceMaskHas(mask, "mesh_sta_vap_enabled") {
			t.Errorf("mask = %v, want it to name mesh_sta_vap_enabled", mask)
		}
	})
	t.Run("an unset mesh_sta_vap_enabled stays off the wire", func(t *testing.T) {
		mask := deviceMaskFor(t, deviceKitModel{})
		if deviceMaskHas(mask, "mesh_sta_vap_enabled") {
			t.Errorf("an unset mesh_sta_vap_enabled was named in the mask: %v", mask)
		}
	})
}

// port_overrides must never be in the general device write mask, on an
// empty plan or a populated one -- it is written through its own keyed
// overlay (updateDevicePortOverridesGrouped, driven from
// deviceKitBeforeSend), never through the masked device PUT that carries
// this mask. Regressing this would resurrect the whole-array write the
// port-overrides fix exists to remove: unifi.Device.PortOverrides carries
// no omitempty (pinned by Test_deviceForceEmittedFieldsAreStillJustThree
// below), so naming it in the mask force-emits whatever
// *ui.Device.PortOverrides happens to hold -- nil marshals to [], which
// the controller reads as clearing every port.
func Test_devicePortOverridesNeverInTheGeneralWireMask(t *testing.T) {
	t.Run("empty plan", func(t *testing.T) {
		mask := deviceMaskFor(t, deviceKitModel{})
		if deviceMaskHas(mask, "port_overrides") {
			t.Fatalf("port_overrides is in the general write mask: %v", mask)
		}
	})
	// A populated plan drives every ordinary Field's SetInPlan branch, the
	// same code path a future Field wrongly wired to "port_overrides" would
	// have to go through -- an empty plan alone would only prove this holds
	// when nothing else is set.
	t.Run("fully populated plan", func(t *testing.T) {
		mask := deviceMaskFor(t, deviceKitModel{
			Name:                       types.StringValue("device-1"),
			Disabled:                   types.BoolValue(true),
			AllowAdoption:              types.BoolValue(true),
			ForgetOnDestroy:            types.BoolValue(true),
			LedOverride:                types.StringValue("on"),
			LedOverrideColor:           types.StringValue("#ffffff"),
			LedOverrideColorBrightness: types.Int64Value(80),
			BandsteeringMode:           types.StringValue("off"),
			FlowctrlEnabled:            types.BoolValue(true),
			JumboframeEnabled:          types.BoolValue(true),
			StpVersion:                 types.StringValue("rstp"),
			StpPriority:                types.Int64Value(32768),
			Locked:                     types.BoolValue(true),
			PoeMode:                    types.StringValue("auto"),
			SwitchVLANEnabled:          types.BoolValue(true),
			MeshStaVapEnabled:          types.BoolValue(true),
			OutdoorModeOverride:        types.StringValue("default"),
			Volume:                     types.Int64Value(50),
			BaresipPassword:            types.StringValue("secret"),
			LcmBrightness:              types.Int64Value(100),
			LcmBrightnessOverride:      types.BoolValue(true),
			LcmIDleTimeoutOverride:     types.BoolValue(true),
			LcmNightModeBegins:         types.StringValue("22:00"),
			LcmNightModeEnds:           types.StringValue("06:00"),
			OutletEnabled:              types.BoolValue(true),
			MgmtNetworkID:              types.StringValue("net-1"),
			Type:                       types.StringValue("usw"),
		})
		if deviceMaskHas(mask, "port_overrides") {
			t.Fatalf("port_overrides is in the general write mask of a fully "+
				"populated plan: %v", mask)
		}
	})

	body, err := json.Marshal(unifi.Device{ID: "d1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"port_overrides":[]`) {
		t.Fatalf("a nil port_overrides no longer marshals to []; the body was %s", body)
	}
}

// Test_deviceForceEmittedFieldsAreStillJustThree pins the fields of
// unifi.Device that carry no omitempty. Force-emitted plus in the mask is
// the combination that writes a zero on every update: adopted and state are
// Fields, so they carry what the last read returned rather than a zero.
// port_overrides must stay out of the mask entirely (see the test above) --
// a fourth entry appearing here is a new candidate for the same care.
func Test_deviceForceEmittedFieldsAreStillJustThree(t *testing.T) {
	_, forceEmits := wireTagsOf(unifi.Device{})

	got := make([]string, 0, len(forceEmits))
	for name, unconditional := range forceEmits {
		if unconditional && name != "_id" && name != "site_id" {
			got = append(got, name)
		}
	}
	sort.Strings(got)

	want := []string{"adopted", "port_overrides", "state"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unifi.Device force-emits %v, not %v.\n\n"+
			"A force-emitted field writes a Go zero whenever the mask names it. "+
			"adopted and state are Fields, so they carry what the last read "+
			"returned; port_overrides must never be in the mask at all -- see "+
			"Test_devicePortOverridesNeverInTheGeneralWireMask. A new entry here "+
			"needs the same decision made about it, then update this list.",
			got, want)
	}
}

// Test_deviceTypeIsAlwaysOnTheWire pins an unverified claim about the
// controller: the hand-written resource echoed `type` from a fresh GET
// because the API was said to require it in the PUT body, and nothing has
// measured whether that's true.
//
// It's kept and pinned here rather than left to a comment. On an update
// `type` would be masked anyway, since the plan carries what the last read
// returned. On a create it would not: `type` is Computed, so the plan
// holds it unknown and SetInPlan drops it. This asserts the create case,
// the one that would break if the requirement is real.
//
// If something measures the controller and finds `type` isn't required,
// delete the AlwaysWire entry, the echo in BeforeSend, and this test
// together.
func Test_deviceTypeIsAlwaysOnTheWire(t *testing.T) {
	mask := deviceMaskFor(t, deviceKitModel{})
	if !deviceMaskHas(mask, "type") {
		t.Fatalf("type is not in the mask of a plan that does not carry it: %v", mask)
	}
}

// wireTagsOf reads the json tags off an SDK struct by reflection, returning
// the Go-field-to-wire-name map and the set of wire names that are emitted
// unconditionally (no omitempty).
func wireTagsOf(object any) (map[string]string, map[string]bool) {
	typ := reflect.TypeOf(object)
	tags := make(map[string]string, typ.NumField())
	forceEmits := make(map[string]bool, typ.NumField())
	for i := range typ.NumField() {
		field := typ.Field(i)
		tag, ok := field.Tag.Lookup("json")
		if !ok || tag == "-" {
			continue
		}
		parts := strings.Split(tag, ",")
		name := parts[0]
		if name == "" {
			continue
		}
		tags[field.Name] = name
		forceEmits[name] = !slices.Contains(parts[1:], "omitempty")
	}
	return tags, forceEmits
}
