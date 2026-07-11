package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

func Test_modelToContentFiltering(t *testing.T) {
	ctx := context.Background()

	categories, d := types.SetValueFrom(ctx, types.StringType,
		[]string{"ADVERTISEMENT"})
	if d.HasError() {
		t.Fatal(d)
	}
	// Upper/dash MAC: the write path must normalize to lower/colon.
	macs, d := types.SetValueFrom(ctx, hwtypes.MACAddressType{},
		[]string{"AA-BB-CC-DD-EE-FF"})
	if d.HasError() {
		t.Fatal(d)
	}
	blockList, d := types.SetValueFrom(ctx, types.StringType,
		[]string{"example.com"})
	if d.HasError() {
		t.Fatal(d)
	}

	m := contentFilteringResourceModel{
		ID:         types.StringValue("cf1"),
		Name:       types.StringValue("kids"),
		Enabled:    types.BoolValue(true),
		Categories: categories,
		ClientMACs: macs,
		NetworkIDs: types.SetNull(types.StringType),
		AllowList:  types.SetNull(types.StringType),
		BlockList:  blockList,
		SafeSearch: types.SetNull(types.StringType),
		Schedule:   types.ObjectNull(contentFilteringScheduleAttrTypes),
	}

	cf, diags := modelToContentFiltering(ctx, m)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if cf.ID != "cf1" || cf.Name != "kids" || !cf.Enabled {
		t.Fatalf("scalars wrong: %+v", cf)
	}
	if len(cf.Categories) != 1 || cf.Categories[0] != "ADVERTISEMENT" {
		t.Fatalf("categories = %v", cf.Categories)
	}
	if len(cf.ClientMACs) != 1 || cf.ClientMACs[0] != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("client_macs not normalized: %v", cf.ClientMACs)
	}
	if len(cf.BlockList) != 1 || cf.BlockList[0] != "example.com" {
		t.Fatalf("block_list = %v", cf.BlockList)
	}
	// Unset collections must serialize as explicit empty arrays, matching the
	// live objects (which always carry every key).
	if cf.NetworkIDs == nil || len(cf.NetworkIDs) != 0 {
		t.Fatalf("network_ids should be an empty slice, got %#v", cf.NetworkIDs)
	}
	if cf.AllowList == nil || cf.SafeSearch == nil {
		t.Fatalf("allow_list/safe_search should be empty slices: %#v %#v",
			cf.AllowList, cf.SafeSearch)
	}
	// A null schedule defaults to ALWAYS.
	if cf.Schedule == nil || cf.Schedule.Mode != "ALWAYS" {
		t.Fatalf("schedule = %+v, want mode ALWAYS", cf.Schedule)
	}
}

func Test_contentFilteringToModel(t *testing.T) {
	ctx := context.Background()

	cf := &unifi.ContentFiltering{
		ID:         "cf1",
		Name:       "kids",
		Enabled:    true,
		Categories: []string{"FAMILY", "ADVERTISEMENT"},
		ClientMACs: []string{"aa:bb:cc:dd:ee:ff"},
		SafeSearch: []string{"GOOGLE", "YOUTUBE", "BING"},
		Schedule:   &unifi.ContentFilteringSchedule{Mode: "ALWAYS"},
	}

	var m contentFilteringResourceModel
	diags := contentFilteringToModel(ctx, cf, &m, "default")
	if diags.HasError() {
		t.Fatal(diags)
	}

	if m.ID.ValueString() != "cf1" || m.Name.ValueString() != "kids" ||
		!m.Enabled.ValueBool() {
		t.Fatalf("scalars wrong: %+v", m)
	}
	if m.Site.ValueString() != "default" {
		t.Fatalf("site = %v", m.Site)
	}
	var cats []string
	diags.Append(m.Categories.ElementsAs(ctx, &cats, false)...)
	if len(cats) != 2 {
		t.Fatalf("categories = %v", cats)
	}
	var search []string
	diags.Append(m.SafeSearch.ElementsAs(ctx, &search, false)...)
	if len(search) != 3 {
		t.Fatalf("safe_search = %v", search)
	}
	// nil slices map to empty sets (not null) so `x = []` round-trips.
	if m.NetworkIDs.IsNull() || len(m.NetworkIDs.Elements()) != 0 {
		t.Fatalf("network_ids should be an empty set, got %v", m.NetworkIDs)
	}
	if m.AllowList.IsNull() || m.BlockList.IsNull() {
		t.Fatalf("allow/block lists should be empty sets: %v %v",
			m.AllowList, m.BlockList)
	}
	var sched contentFilteringScheduleModel
	d := m.Schedule.As(ctx, &sched, objectAsOptions)
	if d.HasError() {
		t.Fatal(d)
	}
	if sched.Mode.ValueString() != "ALWAYS" {
		t.Fatalf("schedule mode = %v", sched.Mode)
	}
}

func Test_contentFilteringResource_Schema(t *testing.T) {
	r := &contentFilteringResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	for _, name := range []string{
		"id", "site", "name", "enabled", "categories", "client_macs",
		"network_ids", "allow_list", "block_list", "safe_search",
		"schedule", "timeouts",
	} {
		if _, ok := resp.Schema.Attributes[name]; !ok {
			t.Errorf("schema missing attribute %q", name)
		}
	}
}

func Test_contentFilteringResource_Metadata(t *testing.T) {
	r := &contentFilteringResource{}
	var resp fwresource.MetadataResponse
	r.Metadata(context.Background(),
		fwresource.MetadataRequest{ProviderTypeName: "unifi"}, &resp)
	if resp.TypeName != "unifi_content_filtering" {
		t.Fatalf("TypeName = %q, want unifi_content_filtering", resp.TypeName)
	}
}

func TestNewContentFilteringResource(t *testing.T) {
	got := NewContentFilteringResource()
	if _, ok := got.(fwresource.ResourceWithImportState); !ok {
		t.Error("NewContentFilteringResource() does not implement resource.ResourceWithImportState")
	}
}
