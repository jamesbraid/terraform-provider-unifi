package unifi

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	unifi "github.com/ubiquiti-community/go-unifi/unifi"
)

// THESE REPLACE THE buildMinimalUpdateDevice TESTS, one mechanism later.
//
// The hand-written resource built a whole PUT body by hand and the question was
// always "did this field make it into the body". unifi_device now sends a field
// mask, so the same question is "is this field named in the mask" -- a field
// the mask does not name is filtered out of the body by the SDK regardless of
// what the struct holds. The behaviours guarded here are the ones those tests
// guarded, restated against the mechanism that now decides them.
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

// #329: a configured mgmt_network_id (the UI "Network Override") must reach the
// controller, and a null one must stay off the wire so it never reintroduces
// the #177 zero-value rejection.
func Test_deviceMask_mgmtNetworkID(t *testing.T) {
	t.Run("configured mgmt_network_id is named in the mask", func(t *testing.T) {
		mask := deviceMaskFor(t, deviceKitModel{
			MgmtNetworkID: types.StringValue("net-mgmt"),
		})
		if !deviceMaskHas(mask, "mgmt_network_id") {
			t.Errorf("mask = %v, want it to name mgmt_network_id (override dropped, #329)", mask)
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

// port_overrides IS ALWAYS IN THE MASK, and that is what makes the merge
// load-bearing rather than an optimisation.
//
// It is derived by BeforeSend rather than mapped from a Field, so nothing in
// the plan can name it and AlwaysWire has to. Being in the mask means it is
// always in the body, and a nil slice marshals to [] -- which the controller
// reads as a full replace with nothing. If either half of this stops holding,
// mergePortOverridesByIndex is no longer protecting anything.
func Test_devicePortOverridesAreAlwaysOnTheWire(t *testing.T) {
	mask := deviceMaskFor(t, deviceKitModel{})
	if !deviceMaskHas(mask, "port_overrides") {
		t.Fatalf("port_overrides is not in the mask of an otherwise empty plan: %v", mask)
	}

	body, err := json.Marshal(unifi.Device{ID: "d1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"port_overrides":[]`) {
		t.Fatalf("a nil port_overrides no longer marshals to []; the body was %s", body)
	}
}

// Test_deviceForceEmittedFieldsAreStillJustThree pins the fields of unifi.Device
// that carry no omitempty.
//
// It used to pin a COINCIDENCE: buildMinimalUpdateDevice sent a whole object, so
// every force-emitted field it did not fill in by hand was written as a Go zero
// on every apply, and exactly three needed rescuing. The mask retires that --
// a field the mask does not name never reaches the body, omitempty or not.
//
// The set is still worth pinning, because force-emitted plus IN the mask is the
// combination that writes a zero. port_overrides is exactly that today, by way
// of AlwaysWire, which is why the merge above exists. A fourth entry appearing
// here is a new candidate for the same treatment, so it should name itself.
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
			"returned; port_overrides is derived and always masked, which is what "+
			"the merge protects. A new entry here needs the same decision made "+
			"about it, then update this list.",
			got, want)
	}
}

// TYPE TRAVELS ON EVERY WRITE, AND NOTHING HAS MEASURED WHETHER IT MUST.
//
// The hand-written resource echoed `type` from a fresh GET because the API was
// said to require it in the PUT body. That claim is about the controller, so no
// amount of reading settles it, and provider CI has dispatched nothing for days
// (#214) -- there is no acceptance run to defer to.
//
// It is therefore kept and pinned here rather than left to a comment. On an
// update `type` would be masked anyway, since the plan carries what the last
// read returned. On a CREATE it would not: `type` is Computed, so the plan
// holds it unknown and SetInPlan drops it. This asserts the create case, which
// is the one that would break if the requirement is real.
//
// If something ever measures the controller and finds `type` is not required,
// delete the AlwaysWire entry, the echo in BeforeSend, and this test together.
func Test_deviceTypeIsAlwaysOnTheWire(t *testing.T) {
	mask := deviceMaskFor(t, deviceKitModel{})
	if !deviceMaskHas(mask, "type") {
		t.Fatalf("type is not in the mask of a plan that does not carry it: %v", mask)
	}
}
