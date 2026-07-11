package unifi

import (
	"context"
	"fmt"
	"os"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

func Test_modelToNat_dnat(t *testing.T) {
	ctx := context.Background()

	groups, d := types.SetValueFrom(ctx, types.StringType, []string{"fg1"})
	if d.HasError() {
		t.Fatal(d)
	}
	srcFilter, d := types.ObjectValueFrom(ctx, natRuleFilterModel{}.AttributeTypes(),
		natRuleFilterModel{
			FilterType:       types.StringValue("FIREWALL_GROUPS"),
			Address:          types.StringValue(""),
			FirewallGroupIDs: groups,
			InvertAddress:    types.BoolValue(false),
			InvertPort:       types.BoolValue(false),
			NetworkID:        types.StringValue(""),
			Port:             types.Int64Null(),
		})
	if d.HasError() {
		t.Fatal(d)
	}
	dstFilter, d := types.ObjectValueFrom(ctx, natRuleFilterModel{}.AttributeTypes(),
		natRuleFilterModel{
			FilterType:       types.StringValue("ADDRESS_AND_PORT"),
			Address:          types.StringValue("192.0.2.10"),
			FirewallGroupIDs: types.SetNull(types.StringType),
			InvertAddress:    types.BoolValue(false),
			InvertPort:       types.BoolValue(true),
			NetworkID:        types.StringValue(""),
			Port:             types.Int64Value(8443),
		})
	if d.HasError() {
		t.Fatal(d)
	}

	m := natRuleResourceModel{
		ID:                    types.StringValue("abc123"),
		Description:           types.StringValue("dnat to web"),
		Type:                  types.StringValue("DNAT"),
		Enabled:               types.BoolValue(true),
		Exclude:               types.BoolValue(false),
		IPAddress:             types.StringValue("10.0.0.5"),
		InInterface:           types.StringValue("eth4"),
		OutInterface:          types.StringNull(),
		Logging:               types.BoolValue(true),
		Port:                  types.Int64Value(443),
		PppoeUseBaseInterface: types.BoolValue(false),
		Protocol:              types.StringValue("tcp"),
		RuleIndex:             types.Int64Value(2000),
		SettingPreference:     types.StringValue("manual"),
		IPVersion:             types.StringValue("IPV4"),
		SourceFilter:          srcFilter,
		DestinationFilter:     dstFilter,
	}

	nat, diags := modelToNat(ctx, m)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if nat.ID != "abc123" || nat.Type != "DNAT" || nat.Description != "dnat to web" {
		t.Fatalf("scalar fields wrong: %+v", nat)
	}
	if nat.IPAddress != "10.0.0.5" || nat.InInterface != "eth4" || nat.OutInterface != "" {
		t.Fatalf("address/interface fields wrong: %+v", nat)
	}
	if !nat.Enabled || nat.Exclude || !nat.Logging {
		t.Fatalf("bool fields wrong: %+v", nat)
	}
	if nat.Protocol != "tcp" || nat.Version != "IPV4" || nat.SettingPreference != "manual" {
		t.Fatalf("enum fields wrong: %+v", nat)
	}
	if nat.Port == nil || *nat.Port != 443 {
		t.Fatalf("port = %v, want 443", nat.Port)
	}
	if nat.RuleIndex == nil || *nat.RuleIndex != 2000 {
		t.Fatalf("rule_index = %v, want 2000", nat.RuleIndex)
	}
	if nat.SourceFilter == nil || nat.SourceFilter.FilterType != "FIREWALL_GROUPS" ||
		len(nat.SourceFilter.FirewallGroupIDs) != 1 ||
		nat.SourceFilter.FirewallGroupIDs[0] != "fg1" {
		t.Fatalf("source_filter wrong: %+v", nat.SourceFilter)
	}
	if nat.DestinationFilter == nil || nat.DestinationFilter.FilterType != "ADDRESS_AND_PORT" ||
		nat.DestinationFilter.Address != "192.0.2.10" ||
		!nat.DestinationFilter.InvertPort ||
		nat.DestinationFilter.Port == nil || *nat.DestinationFilter.Port != 8443 {
		t.Fatalf("destination_filter wrong: %+v", nat.DestinationFilter)
	}
}

func Test_modelToNat_masqueradeMinimal(t *testing.T) {
	ctx := context.Background()
	m := natRuleResourceModel{
		Type:              types.StringValue("MASQUERADE"),
		OutInterface:      types.StringValue("eth8"),
		Enabled:           types.BoolValue(true),
		Exclude:           types.BoolValue(false),
		Logging:           types.BoolValue(false),
		Description:       types.StringValue(""),
		Port:              types.Int64Null(),
		RuleIndex:         types.Int64Null(),
		SourceFilter:      types.ObjectNull(natRuleFilterModel{}.AttributeTypes()),
		DestinationFilter: types.ObjectNull(natRuleFilterModel{}.AttributeTypes()),
	}

	nat, diags := modelToNat(ctx, m)
	if diags.HasError() {
		t.Fatal(diags)
	}
	if nat.Type != "MASQUERADE" || nat.OutInterface != "eth8" {
		t.Fatalf("fields wrong: %+v", nat)
	}
	if nat.Port != nil {
		t.Fatalf("unset port must marshal as nil (omitted), got %v", *nat.Port)
	}
	if nat.RuleIndex != nil {
		t.Fatalf("unset rule_index must marshal as nil (omitted), got %v", *nat.RuleIndex)
	}
	if nat.SourceFilter != nil || nat.DestinationFilter != nil {
		t.Fatal("null filter objects must map to nil filters")
	}
}

func Test_natToModel_roundTrip(t *testing.T) {
	ctx := context.Background()
	port := int64(443)
	idx := int64(2010)
	nat := &unifi.Nat{
		ID:           "abc123",
		Type:         "SNAT",
		Description:  "snat out",
		Enabled:      true,
		Exclude:      false,
		Logging:      false,
		IPAddress:    "203.0.113.7",
		OutInterface: "eth8",
		Protocol:     "all",
		Port:         &port,
		RuleIndex:    &idx,
		Version:      "IPV4",
		SourceFilter: &unifi.NatSourceFilter{
			FilterType: "NETWORK_CONF", NetworkConfID: "net1",
		},
	}

	var m natRuleResourceModel
	diags := natToModel(ctx, nat, &m)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if m.ID.ValueString() != "abc123" || m.Type.ValueString() != "SNAT" {
		t.Fatalf("id/type wrong: %v %v", m.ID, m.Type)
	}
	if m.IPAddress.ValueString() != "203.0.113.7" || m.OutInterface.ValueString() != "eth8" {
		t.Fatalf("address fields wrong: %v %v", m.IPAddress, m.OutInterface)
	}
	if !m.InInterface.IsNull() {
		t.Fatalf("empty in_interface should be null, got %v", m.InInterface)
	}
	if m.Port.ValueInt64() != 443 || m.RuleIndex.ValueInt64() != 2010 {
		t.Fatalf("port/rule_index wrong: %v %v", m.Port, m.RuleIndex)
	}
	if m.SourceFilter.IsNull() {
		t.Fatal("source_filter should be set")
	}
	var fm natRuleFilterModel
	d := m.SourceFilter.As(ctx, &fm, objectAsOptions)
	if d.HasError() {
		t.Fatal(d)
	}
	if fm.FilterType.ValueString() != "NETWORK_CONF" || fm.NetworkID.ValueString() != "net1" {
		t.Fatalf("filter wrong: %+v", fm)
	}
	if !m.DestinationFilter.IsNull() {
		t.Fatal("nil destination filter should map to null object")
	}
}

func Test_natToModel_zeroPortIsNull(t *testing.T) {
	// go-unifi's UnmarshalJSON maps an empty-string port to *int64(0); the
	// model must treat 0 as "no port" so plans stay clean.
	ctx := context.Background()
	zero := int64(0)
	nat := &unifi.Nat{ID: "x", Type: "MASQUERADE", Port: &zero}

	var m natRuleResourceModel
	if diags := natToModel(ctx, nat, &m); diags.HasError() {
		t.Fatal(diags)
	}
	if !m.Port.IsNull() {
		t.Fatalf("zero port should be null, got %v", m.Port)
	}
}

func Test_natRuleResource_Schema(t *testing.T) {
	r := &natRuleResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	for _, name := range []string{
		"id", "site", "type", "description", "enabled", "exclude", "ip_address",
		"in_interface", "out_interface", "logging", "port",
		"pppoe_use_base_interface", "protocol", "rule_index",
		"setting_preference", "ip_version", "source_filter",
		"destination_filter", "timeouts",
	} {
		if _, ok := resp.Schema.Attributes[name]; !ok {
			t.Errorf("schema missing attribute %q", name)
		}
	}
	src, ok := resp.Schema.Attributes["source_filter"].(schema.SingleNestedAttribute)
	if !ok {
		t.Fatal("source_filter is not a SingleNestedAttribute")
	}
	for _, name := range []string{
		"filter_type", "address", "firewall_group_ids",
		"invert_address", "invert_port", "network_id", "port",
	} {
		if _, ok := src.Attributes[name]; !ok {
			t.Errorf("source_filter missing attribute %q", name)
		}
	}
}

func Test_natRuleResource_Metadata(t *testing.T) {
	r := &natRuleResource{}
	var resp fwresource.MetadataResponse
	r.Metadata(context.Background(),
		fwresource.MetadataRequest{ProviderTypeName: "unifi"}, &resp)
	if resp.TypeName != "unifi_nat_rule" {
		t.Fatalf("TypeName = %q, want unifi_nat_rule", resp.TypeName)
	}
}

func TestNewNatRuleResource(t *testing.T) {
	got := NewNatRuleResource()
	if _, ok := got.(fwresource.ResourceWithImportState); !ok {
		t.Error("NewNatRuleResource() does not implement resource.ResourceWithImportState")
	}
}

// testAccNatRulePreCheck skips when the controller does not support the v2
// NAT API (the docker demo controller predates or only partially implements
// it). A plain list is not sufficient: on the jacobalberty/unifi demo image
// the v2 NAT GET endpoint returns 200 but POST (create) returns a bare HTTP
// 500 (not an api.err.* validation error), so the probe also attempts a
// throwaway create — matching the shape TestAccNatRule_basic itself submits —
// and deletes it on success. Any failure in that round-trip means "unsupported
// here" and skips the whole test.
func testAccNatRulePreCheck(t *testing.T) {
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
	if _, err := c.ListNat(ctx, c.Site); err != nil {
		t.Skipf("controller does not support the v2 NAT API: %v", err)
	}

	probe := &unifi.Nat{
		Type:         "MASQUERADE",
		OutInterface: "eth8",
		Description:  "tf-acc-precheck-probe",
		Enabled:      false,
	}
	created, err := c.CreateNat(ctx, c.Site, probe)
	if err != nil {
		t.Skipf("controller does not support creating v2 NAT rules: %v", err)
	}
	if err := c.DeleteNat(ctx, c.Site, created.ID); err != nil {
		t.Logf(
			"warning: failed to clean up precheck probe NAT rule %s: %v",
			created.ID,
			err,
		)
	}
}

// testAccNatRuleCheckDestroy verifies every unifi_nat_rule in state is gone.
func testAccNatRuleCheckDestroy(s *terraform.State) error {
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
		if rs.Type != "unifi_nat_rule" {
			continue
		}
		site := rs.Primary.Attributes["site"]
		if site == "" {
			site = c.Site
		}
		_, err := c.GetNat(ctx, site, rs.Primary.ID)
		if err == nil {
			return fmt.Errorf("unifi_nat_rule %s still exists", rs.Primary.ID)
		}
		if _, ok := err.(*unifi.NotFoundError); !ok {
			return err
		}
	}
	return nil
}

func TestAccNatRule_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccNatRulePreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccNatRuleCheckDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccNatRuleConfig("tf-acc masquerade", false),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_nat_rule.test", "id"),
					resource.TestCheckResourceAttr(
						"unifi_nat_rule.test", "type", "MASQUERADE",
					),
					resource.TestCheckResourceAttr(
						"unifi_nat_rule.test", "description", "tf-acc masquerade",
					),
					resource.TestCheckResourceAttr(
						"unifi_nat_rule.test", "enabled", "false",
					),
				),
			},
			// In-place update: description and enabled.
			{
				Config: testAccNatRuleConfig("tf-acc masquerade v2", true),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_nat_rule.test", "description", "tf-acc masquerade v2",
					),
					resource.TestCheckResourceAttr(
						"unifi_nat_rule.test", "enabled", "true",
					),
				),
			},
			// Import round-trip.
			{
				ResourceName:      "unifi_nat_rule.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccNatRuleConfig(description string, enabled bool) string {
	return fmt.Sprintf(`
resource "unifi_nat_rule" "test" {
  type          = "MASQUERADE"
  description   = %q
  enabled       = %t
  out_interface = "eth8"
}
`, description, enabled)
}
