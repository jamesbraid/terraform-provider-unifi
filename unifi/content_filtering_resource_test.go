package unifi

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
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

// testAccContentFilteringPreCheck skips when the controller lacks the v2
// content-filtering API (the docker demo controller predates it). A plain
// list is not sufficient: as with NAT (see testAccNatRulePreCheck), the
// demo image's v2 GET endpoints can return 200 while POST (create) returns
// a bare HTTP 500, so the probe also attempts a throwaway create — matching
// the shape TestAccContentFiltering_basic itself submits — and deletes it
// on success. Any failure in that round-trip means "unsupported here" and
// skips the whole test.
func testAccContentFilteringPreCheck(t *testing.T) {
	preCheck(t)
	ctx := context.Background()
	apiClient, err := unifi.New(ctx, &unifi.Config{
		BaseURL:       os.Getenv("UNIFI_API"),
		Username:      os.Getenv("UNIFI_USERNAME"),
		Password:      os.Getenv("UNIFI_PASSWORD"),
		AllowInsecure: true,
	})
	if err != nil {
		t.Fatalf("could not build probe client: %v", err)
	}
	c := &Client{ApiClient: apiClient, Site: "default"}
	if _, err := c.ListContentFiltering(ctx, c.Site); err != nil {
		t.Skipf("controller does not support the v2 content-filtering API: %v", err)
	}

	probe := &unifi.ContentFiltering{
		Name:       "tf-acc-precheck-probe",
		Enabled:    false,
		Categories: []string{"ADVERTISEMENT"},
		BlockList:  []string{"example.com"},
		Schedule:   &unifi.ContentFilteringSchedule{Mode: "ALWAYS"},
	}
	created, err := c.CreateContentFiltering(ctx, c.Site, probe)
	if err != nil {
		t.Skipf("controller does not support creating v2 content-filtering policies: %v", err)
	}
	if err := c.DeleteContentFiltering(ctx, c.Site, created.ID); err != nil {
		t.Logf(
			"warning: failed to clean up precheck probe content-filtering policy %s: %v",
			created.ID,
			err,
		)
	}
}

func testAccContentFilteringCheckDestroy(s *terraform.State) error {
	ctx := context.Background()
	apiURL := os.Getenv("UNIFI_API")
	if apiURL == "" {
		return nil
	}
	apiClient, err := unifi.New(ctx, &unifi.Config{
		BaseURL:       apiURL,
		Username:      os.Getenv("UNIFI_USERNAME"),
		Password:      os.Getenv("UNIFI_PASSWORD"),
		AllowInsecure: true,
	})
	if err != nil {
		return nil //nolint:nilerr // best-effort check; skip when no live client
	}
	c := &Client{ApiClient: apiClient, Site: "default"}
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "unifi_content_filtering" {
			continue
		}
		site := rs.Primary.Attributes["site"]
		if site == "" {
			site = c.Site
		}
		_, err := c.GetContentFiltering(ctx, site, rs.Primary.ID)
		if err == nil {
			return fmt.Errorf("unifi_content_filtering %s still exists", rs.Primary.ID)
		}
		if _, ok := err.(*unifi.NotFoundError); !ok {
			return err
		}
	}
	return nil
}

func TestAccContentFiltering_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccContentFilteringPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccContentFilteringCheckDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccContentFilteringConfig("tf-acc-cf", true),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_content_filtering.test", "id"),
					resource.TestCheckResourceAttr(
						"unifi_content_filtering.test", "name", "tf-acc-cf",
					),
					resource.TestCheckResourceAttr(
						"unifi_content_filtering.test", "enabled", "true",
					),
					resource.TestCheckResourceAttr(
						"unifi_content_filtering.test", "block_list.#", "1",
					),
					resource.TestCheckResourceAttr(
						"unifi_content_filtering.test", "schedule.mode", "ALWAYS",
					),
				),
			},
			// In-place update: rename and disable.
			{
				Config: testAccContentFilteringConfig("tf-acc-cf-2", false),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_content_filtering.test", "name", "tf-acc-cf-2",
					),
					resource.TestCheckResourceAttr(
						"unifi_content_filtering.test", "enabled", "false",
					),
				),
			},
			// Import round-trip.
			{
				ResourceName:      "unifi_content_filtering.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccContentFilteringConfig(name string, enabled bool) string {
	return fmt.Sprintf(`
resource "unifi_content_filtering" "test" {
  name       = %q
  enabled    = %t
  categories = ["ADVERTISEMENT"]
  block_list = ["example.com"]
}
`, name, enabled)
}
