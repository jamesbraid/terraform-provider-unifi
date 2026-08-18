package unifi

// The differential for the second descriptor.
//
// IT EXISTS BECAUSE THE CONTRACT CHECK CANNOT SEE READ-ONLY-NESS. That check
// compares field NAMES between the mapping and the descriptor; dropping the
// ReadOnly wrapper from zone_key leaves every name identical and changes what
// the provider sends. Only a comparison against the hand-written path catches
// it, which is the same reason dns_record has one.
//
// DELETE THIS FILE IN THE COMMIT THAT DELETES THE HAND-WRITTEN RESOURCE.

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

func firewallZoneKitCases(t *testing.T) map[string]firewallZoneResourceModel {
	t.Helper()
	ids, diags := types.ListValueFrom(context.Background(), types.StringType,
		[]string{"net-a", "net-b"})
	if diags.HasError() {
		t.Fatalf("build the network id list: %v", diags)
	}
	return map[string]firewallZoneResourceModel{
		"every attribute set": {
			ID: types.StringValue("zone-1"), Site: types.StringValue("default"),
			Name: types.StringValue("Trusted"), NetworkIDs: ids,
			ZoneKey: types.StringValue("trusted"), DefaultZone: types.BoolValue(true),
		},
		// THE CASE THE READ-ONLY WRAPPER IS ABOUT. zone_key and default_zone
		// hold values the controller assigned; the send path must ignore both.
		"controller-owned values present": {
			ID: types.StringValue("zone-1"), Site: types.StringValue("default"),
			Name: types.StringValue("Trusted"), NetworkIDs: types.ListNull(types.StringType),
			ZoneKey: types.StringValue("trusted"), DefaultZone: types.BoolValue(true),
		},
		"a null network list": {
			ID: types.StringValue("zone-1"), Site: types.StringValue("default"),
			Name: types.StringValue("Trusted"), NetworkIDs: types.ListNull(types.StringType),
			ZoneKey: types.StringNull(), DefaultZone: types.BoolNull(),
		},
	}
}

func toFirewallZoneKitModel(m firewallZoneResourceModel) firewallZoneKitModel {
	return firewallZoneKitModel{
		ID: m.ID, Site: m.Site, Name: m.Name, NetworkIDs: m.NetworkIDs,
		ZoneKey: m.ZoneKey, DefaultZone: m.DefaultZone, Timeouts: m.Timeouts,
	}
}

func sameZone(a, b *ui.FirewallZone) bool {
	if (a.DefaultZone == nil) != (b.DefaultZone == nil) {
		return false
	}
	if a.DefaultZone != nil && *a.DefaultZone != *b.DefaultZone {
		return false
	}
	if len(a.NetworkIDs) != len(b.NetworkIDs) {
		return false
	}
	for i := range a.NetworkIDs {
		if a.NetworkIDs[i] != b.NetworkIDs[i] {
			return false
		}
	}
	// The remaining scalars, named rather than compared as a struct:
	// ui.FirewallZone holds a []string, so == is not defined on it.
	return a.ID == b.ID && a.SiteID == b.SiteID && a.Name == b.Name &&
		a.ZoneKey == b.ZoneKey && a.CloudTemplate == b.CloudTemplate &&
		a.ExternalID == b.ExternalID && a.Hidden == b.Hidden &&
		a.HiddenID == b.HiddenID && a.NoDelete == b.NoDelete && a.NoEdit == b.NoEdit
}

func TestFirewallZoneKitSendsWhatTheHandWrittenPathSends(t *testing.T) {
	hand := &firewallZoneResource{}
	spec := firewallZoneKitSpec()
	for name, model := range firewallZoneKitCases(t) {
		t.Run(name, func(t *testing.T) {
			byHand, diags := hand.modelToFirewallZone(context.Background(), &model)
			if diags.HasError() {
				t.Fatalf("the hand-written path reported: %v", diags)
			}
			kitModel := toFirewallZoneKitModel(model)
			byKit, kitDiags := spec.ToSDK(context.Background(), &kitModel)
			if kitDiags.HasError() {
				t.Fatalf("the generated path reported: %v", kitDiags)
			}
			if !sameZone(byHand, byKit) {
				t.Fatalf("the two paths send different objects:\n  hand %+v\n  kit  %+v", *byHand, *byKit)
			}
		})
	}
}

func TestFirewallZoneKitReadsWhatTheHandWrittenPathReads(t *testing.T) {
	hand := &firewallZoneResource{}
	spec := firewallZoneKitSpec()
	yes, no := true, false
	for name, zone := range map[string]*ui.FirewallZone{
		"a default zone": {
			ID: "zone-1", Name: "Trusted", ZoneKey: "trusted",
			DefaultZone: &yes, NetworkIDs: []string{"net-a"},
		},
		"not a default zone": {
			ID: "zone-2", Name: "Guest", ZoneKey: "guest",
			DefaultZone: &no, NetworkIDs: []string{},
		},
		// THE POINTER'S THIRD STATE, which a bool cannot express: the controller
		// did not say. Read through BoolField this would come back false.
		"the controller did not say": {
			ID: "zone-3", Name: "Silent", ZoneKey: "silent", NetworkIDs: []string{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			var handModel firewallZoneResourceModel
			if diags := hand.firewallZoneToModel(context.Background(), zone, &handModel, "default"); diags.HasError() {
				t.Fatalf("the hand-written path reported: %v", diags)
			}
			var kitModel firewallZoneKitModel
			if diags := spec.ToModel(context.Background(), zone, &kitModel, "default"); diags.HasError() {
				t.Fatalf("the generated path reported: %v", diags)
			}
			got := toFirewallZoneKitModel(handModel)
			if got.ID != kitModel.ID || got.Site != kitModel.Site || got.Name != kitModel.Name ||
				got.ZoneKey != kitModel.ZoneKey || got.DefaultZone != kitModel.DefaultZone ||
				!got.NetworkIDs.Equal(kitModel.NetworkIDs) {
				t.Fatalf("the two paths write different state:\n  hand %+v\n  kit  %+v", got, kitModel)
			}
		})
	}
}
