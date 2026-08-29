package unifi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// TestIgmpSnoopingSettingRoundTrip ports the "basic fields mapped" case of
// the deleted Test_settingResource_igmpSnoopingSettingToModel and the
// "enabled/network_ids overlaid onto base" cases of the deleted
// Test_settingResource_igmpSnoopingModelToSetting: model -> go-unifi
// setting -> model preserves enabled and network_ids. It now drives the
// Spec's own ToSDK/ToModel instead of the deleted
// igmpSnoopingModelToSetting/igmpSnoopingSettingToModel mappers -- which
// built a fresh settings.IgmpSnooping rather than overlaying onto a
// pre-read base, since a masked write no longer needs one (see this
// descriptor's own comment).
func TestIgmpSnoopingSettingRoundTrip(t *testing.T) {
	ctx := context.Background()
	spec := igmpSnoopingKitSpec()

	networkIDs, diags := types.ListValueFrom(ctx, types.StringType, []string{"net-1", "net-2"})
	if diags.HasError() {
		t.Fatalf("building network_ids: %v", diags)
	}
	in := &settingIgmpSnoopingModel{
		Enabled:    types.BoolValue(true),
		NetworkIDs: networkIDs,
	}
	sdk, diags := spec.ToSDK(ctx, in)
	if diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}
	if !sdk.Enabled || len(sdk.NetworkIDs) != 2 || sdk.NetworkIDs[0] != "net-1" {
		t.Fatalf("ToSDK = %+v, want enabled network_ids=[net-1 net-2]", sdk)
	}

	var out settingIgmpSnoopingModel
	if diags := spec.ToModel(ctx, sdk, &out, ""); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	if !out.Enabled.ValueBool() {
		t.Error("Enabled should be true")
	}
	var ids []string
	if diags := out.NetworkIDs.ElementsAs(ctx, &ids, false); diags.HasError() {
		t.Fatalf("reading network_ids: %v", diags)
	}
	if len(ids) != 2 || ids[0] != "net-1" {
		t.Errorf("network_ids = %v, want [net-1 net-2]", ids)
	}
}

// TestIgmpSnoopingSpecReadsEmptyNetworkIDs ports the "empty network ids"
// case of the deleted Test_settingResource_igmpSnoopingSettingToModel: a
// controller read with no network_ids still decodes enabled correctly and
// produces no diagnostics. It now drives the Spec's own ToModel.
func TestIgmpSnoopingSpecReadsEmptyNetworkIDs(t *testing.T) {
	ctx := context.Background()
	spec := igmpSnoopingKitSpec()

	sdk := &settings.IgmpSnooping{Enabled: false, NetworkIDs: nil}
	var out settingIgmpSnoopingModel
	if diags := spec.ToModel(ctx, sdk, &out, ""); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	if out.Enabled.ValueBool() {
		t.Error("Enabled should be false")
	}
}

// TestIgmpSnoopingModelMerge and the "overlaid onto base"/"advanced fields
// preserved" assertions it and Test_settingResource_igmpSnoopingModelToSetting
// pinned are retired outright, not ported: they proved a Go-level
// read-modify-write kept querier_mode/querier_switches/flood_known_protocols
// alive across an update, and that merge no longer exists. What replaces it
// is TestIgmpSnoopingSpecMasksOnlyEnabled below, at a different level --
// UpdateSettingFields' field mask is what now keeps every advanced field
// untouched, not a Go struct overlay -- the same substitution
// TestMgmtAfterReceive's own comment records for mgmt's WifimanEnabled case.

// TestIgmpSnoopingSpecMasksOnlyEnabled pins the point of the migration for
// this section: a plan that configures only "enabled" -- network_ids left
// unset -- must mask exactly one wire. The thirteen SDK members this schema
// never modelled (querier_mode, switches, flood options, ...) can't appear
// on the mask at all, since nothing in Fields carries their names; this
// proves the plan-driven half of that (SetInPlan), the httptest-backed
// TestIgmpSnoopingBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey proves
// the wire half.
func TestIgmpSnoopingSpecMasksOnlyEnabled(t *testing.T) {
	spec := igmpSnoopingKitSpec()
	plan := &settingIgmpSnoopingModel{
		Enabled:    types.BoolValue(true),
		NetworkIDs: types.ListNull(types.StringType),
	}
	fields, err := spec.WireFields(plan)
	if err != nil {
		t.Fatalf("WireFields: %v", err)
	}
	if len(fields) != 1 || fields[0] != "enabled" {
		t.Errorf("WireFields = %v, want exactly [enabled]", fields)
	}
}

// TestIgmpSnoopingBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey is the
// unit half of igmp_snooping's masked-write gate, shaped exactly like
// TestMgmtBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey
// (setting_mgmt_descriptor_test.go): it runs igmpSnoopingKitBackend's
// UpdateFields closure -- the same one Configure wires into the live
// resource -- against an httptest server that keeps the raw, undecoded PUT
// body, and asserts it carries exactly the field the mask named plus "key":
// no force-emitted sibling (auto_unknown_traffic_handling, which carries no
// omitempty on settings.IgmpSnooping) and none of the other twelve
// unmodelled fields either.
func TestIgmpSnoopingBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey(t *testing.T) {
	var body map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/proxy/network/status" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"meta":{"server_version":"10.4.57"}}`))
			return
		}
		raw, _ := io.ReadAll(req.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("the provider sent a body that is not an object: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(append(append([]byte(`{"data":[`), raw...), []byte(`]}`)...))
	}))
	t.Cleanup(server.Close)

	api, err := ui.New(context.Background(), &ui.Config{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("create the API client: %v", err)
	}

	backend := igmpSnoopingKitBackend(api)
	sdk := &settings.IgmpSnooping{Enabled: true, AutoUnknownTrafficHandling: true}
	if _, err := backend.UpdateFields(context.Background(), "default", sdk, "enabled"); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	want := map[string]bool{"key": true, "enabled": true}
	if len(body) != len(want) {
		t.Fatalf("PUT body has %d key(s) %v, want exactly %v", len(body), keysOf(body), want)
	}
	for name := range want {
		if _, ok := body[name]; !ok {
			t.Errorf("PUT body is missing %q; got %v", name, keysOf(body))
		}
	}
	// auto_unknown_traffic_handling has no omitempty on settings.IgmpSnooping
	// -- an unmasked encode would always carry it. Its absence here is the
	// assertion this test exists for.
	if _, ok := body["auto_unknown_traffic_handling"]; ok {
		t.Error(`PUT body carries "auto_unknown_traffic_handling", which the mask never named; ` +
			"the masked write is supposed to leave it out")
	}
}

// TestIgmpSnoopingKitSpecConformance runs the same conformance instruments
// every other kit descriptor's test applies (see e.g. dns_record's case in
// descriptor_elide_test.go), scoped to igmp_snooping's own nested schema
// rather than a whole resource's, since igmp_snooping is one section of
// unifi_setting rather than a surface of its own. igmp_snooping's own
// top-level attribute is Optional-only (not Computed, unlike every other
// section migrated so far); this passing is what confirms
// resourcekit.SpecSection's Configured -- keyed on the object being
// non-null, needing no Computed flag -- accepts that shape.
func TestIgmpSnoopingKitSpecConformance(t *testing.T) {
	ctx := context.Background()
	spec := igmpSnoopingKitSpec()
	for _, problem := range resourcekit.WireNameProblems(spec) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.NestedProblems(spec) {
		t.Error(problem)
	}
	built := igmpSnoopingNestedSchema(ctx)
	for _, problem := range resourcekit.ElideProblems(spec, built) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.ZeroReadProblems(spec, built) {
		t.Error(problem)
	}
}

// TestIgmpSnoopingNestedSchemaHasExactlyItsAttributes guards
// igmpSnoopingNestedSchema's type assertion against a generator regression:
// "igmp_snooping" moving off SingleNestedAttribute would panic every
// conformance test above instead of naming the actual problem, so this pins
// the shape ahead of that.
func TestIgmpSnoopingNestedSchemaHasExactlyItsAttributes(t *testing.T) {
	ctx := context.Background()
	built := resource_setting.SettingResourceSchema(ctx)
	attr, ok := built.Attributes["igmp_snooping"]
	if !ok {
		t.Fatal(`the generated setting schema has no "igmp_snooping" attribute`)
	}
	if attr.IsComputed() {
		t.Error(`"igmp_snooping" is Computed; it is expected to stay Optional-only`)
	}
	nested := igmpSnoopingNestedSchema(ctx)
	if len(nested.Attributes) != 2 {
		t.Errorf("igmp_snooping has %d attribute(s), want 2; update igmpSnoopingKitSpec and this count together",
			len(nested.Attributes))
	}
}
