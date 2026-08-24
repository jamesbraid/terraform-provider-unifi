package unifi

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwlist "github.com/hashicorp/terraform-plugin-framework/list"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

func TestAccPortProfileFramework_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccPortProfileFrameworkConfig_basic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_port_profile.test", "id"),
					resource.TestCheckResourceAttr(
						"unifi_port_profile.test",
						"name",
						"Test Port Profile",
					),
					resource.TestCheckResourceAttr("unifi_port_profile.test", "autoneg", "true"),
				),
			},
			{
				ResourceName:      "unifi_port_profile.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccPortProfileFrameworkConfig_basic() string {
	return `
resource "unifi_port_profile" "test" {
	name     = "Test Port Profile"
	autoneg  = true
	op_mode  = "switch"
}
`
}

func TestAccPortProfileFramework_vlanFields(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccPortProfileFrameworkConfig_vlanFields(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_port_profile.vlan", "id"),
					// native_networkconf_id is now persisted/computed (was previously a no-op).
					resource.TestCheckResourceAttrSet(
						"unifi_port_profile.vlan",
						"native_networkconf_id",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_profile.vlan",
						"setting_preference",
						"manual",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_profile.vlan",
						"port_keepalive_enabled",
						"true",
					),
				),
			},
			{
				ResourceName:      "unifi_port_profile.vlan",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccPortProfileFrameworkConfig_vlanFields() string {
	return `
resource "unifi_port_profile" "vlan" {
	name                   = "Test Port Profile VLAN"
	op_mode                = "switch"
	setting_preference     = "manual"
	port_keepalive_enabled = true
}
`
}

func TestAccPortProfileFramework_exactTaggedNetworks(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccPortProfileFrameworkConfig_exactTaggedNetworks(false),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_port_profile.exact",
						"tagged_vlan_mgmt",
						"custom",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_profile.exact",
						"tagged_networkconf_ids.#",
						"1",
					),
					resource.TestCheckTypeSetElemAttrPair(
						"unifi_port_profile.exact",
						"tagged_networkconf_ids.*",
						"unifi_network.allowed",
						"id",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_profile.exact",
						"excluded_networkconf_ids.#",
						"2",
					),
					resource.TestCheckTypeSetElemAttrPair(
						"unifi_port_profile.exact",
						"excluded_networkconf_ids.*",
						"unifi_network.excluded",
						"id",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_profile.allow_all",
						"tagged_networkconf_ids.#",
						"3",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_profile.block_all",
						"tagged_vlan_mgmt",
						"block_all",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_profile.block_all",
						"tagged_networkconf_ids.#",
						"0",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_profile.raw_exclusions",
						"tagged_networkconf_ids.#",
						"2",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_profile.raw_exclusions",
						"excluded_networkconf_ids.#",
						"1",
					),
					resource.TestCheckResourceAttr(
						"data.unifi_port_profile.exact",
						"tagged_vlan_mgmt",
						"custom",
					),
					resource.TestCheckTypeSetElemAttrPair(
						"data.unifi_port_profile.exact",
						"tagged_networkconf_ids.*",
						"unifi_network.allowed",
						"id",
					),
				),
			},
			{
				ResourceName:      "unifi_port_profile.exact",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config:             testAccPortProfileFrameworkConfig_exactTaggedNetworks(true),
				ExpectNonEmptyPlan: true,
			},
			{
				// The prior step creates the new VLAN. On this next refresh/apply,
				// the configured include and exclusion sets reconcile the
				// controller's automatic treatment of that VLAN.
				Config: testAccPortProfileFrameworkConfig_exactTaggedNetworks(true),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_port_profile.exact",
						"tagged_networkconf_ids.#",
						"1",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_profile.exact",
						"excluded_networkconf_ids.#",
						"3",
					),
					resource.TestCheckTypeSetElemAttrPair(
						"unifi_port_profile.exact",
						"excluded_networkconf_ids.*",
						"unifi_network.added_later",
						"id",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_profile.allow_all",
						"tagged_networkconf_ids.#",
						"4",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_profile.block_all",
						"tagged_networkconf_ids.#",
						"0",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_profile.raw_exclusions",
						"tagged_networkconf_ids.#",
						"3",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_profile.raw_exclusions",
						"excluded_networkconf_ids.#",
						"1",
					),
				),
			},
		},
	})
}

func testAccPortProfileFrameworkConfig_exactTaggedNetworks(withAddedNetwork bool) string {
	addedNetwork := ""
	addedDependency := ""
	if withAddedNetwork {
		addedNetwork = `
resource "unifi_network" "added_later" {
	name                = "Port Profile Added Later"
	subnet              = "192.168.113.1/24"
	vlan                = 113
	third_party_gateway = true
}
`
		addedDependency = ", unifi_network.added_later"
	}

	return fmt.Sprintf(`
resource "unifi_network" "native" {
	name                = "Port Profile Native"
	subnet              = "192.168.110.1/24"
	vlan                = 110
	third_party_gateway = true
}

resource "unifi_network" "allowed" {
	name                = "Port Profile Allowed"
	subnet              = "192.168.111.1/24"
	vlan                = 111
	third_party_gateway = true
}

resource "unifi_network" "excluded" {
	name                = "Port Profile Excluded"
	subnet              = "192.168.112.1/24"
	vlan                = 112
	third_party_gateway = true
}

resource "unifi_network" "disabled" {
	name                = "Port Profile Disabled"
	subnet              = "192.168.114.1/24"
	vlan                = 114
	enabled             = false
	third_party_gateway = true
}
%s
resource "unifi_port_profile" "exact" {
	name                  = "Port Profile Exact Tags"
	native_networkconf_id = unifi_network.native.id
	tagged_networkconf_ids = [
		unifi_network.allowed.id,
	]
	depends_on = [unifi_network.excluded, unifi_network.disabled%s]
}

resource "unifi_port_profile" "allow_all" {
	name                  = "Port Profile Allow All"
	native_networkconf_id = unifi_network.native.id
	tagged_vlan_mgmt      = "auto"
	depends_on            = [unifi_network.allowed, unifi_network.excluded, unifi_network.disabled%s]
}

resource "unifi_port_profile" "block_all" {
	name                  = "Port Profile Block All"
	native_networkconf_id = unifi_network.native.id
	tagged_networkconf_ids = []
	depends_on = [
		unifi_network.allowed,
		unifi_network.excluded,
		unifi_network.disabled%s,
	]
}

resource "unifi_port_profile" "raw_exclusions" {
	name                  = "Port Profile Raw Exclusions"
	native_networkconf_id = unifi_network.native.id
	excluded_networkconf_ids = [
		unifi_network.excluded.id,
	]
	depends_on = [unifi_network.allowed, unifi_network.disabled%s]
}

data "unifi_port_profile" "exact" {
	name       = unifi_port_profile.exact.name
	depends_on = [unifi_port_profile.exact]
}
`, addedNetwork, addedDependency, addedDependency, addedDependency, addedDependency)
}

func TestNewPortProfileFrameworkResource(t *testing.T) {
	got := NewPortProfileFrameworkResource()
	if got == nil {
		t.Fatal("NewPortProfileFrameworkResource() returned nil")
	}
	if _, ok := got.(fwresource.ResourceWithImportState); !ok {
		t.Errorf(
			"NewPortProfileFrameworkResource() does not implement fwresource.ResourceWithImportState",
		)
	}
	if _, ok := got.(fwresource.ResourceWithIdentity); !ok {
		t.Errorf(
			"NewPortProfileFrameworkResource() does not implement fwresource.ResourceWithIdentity",
		)
	}
	if _, ok := got.(fwresource.ResourceWithUpgradeState); !ok {
		t.Errorf(
			"NewPortProfileFrameworkResource() does not implement fwresource.ResourceWithUpgradeState",
		)
	}
	if _, ok := got.(fwresource.ResourceWithConfigValidators); !ok {
		t.Errorf(
			"NewPortProfileFrameworkResource() does not implement fwresource.ResourceWithConfigValidators",
		)
	}
}

func TestNewPortProfileListResource(t *testing.T) {
	got := NewPortProfileListResource()
	if got == nil {
		t.Fatal("NewPortProfileListResource() returned nil")
	}
	if _, ok := got.(fwlist.ListResourceWithConfigure); !ok {
		t.Errorf("NewPortProfileListResource() does not implement fwlist.ListResourceWithConfigure")
	}
}

func Test_portProfileResource_IdentitySchema(t *testing.T) {
	type args struct {
		in0  context.Context
		in1  fwresource.IdentitySchemaRequest
		resp *fwresource.IdentitySchemaResponse
	}
	tests := []struct {
		name string
		r    *portProfileKitResource
		args args
	}{
		{
			name: "does not panic",
			r:    newPortProfileKitResource(),
			args: args{
				in0:  context.Background(),
				in1:  fwresource.IdentitySchemaRequest{},
				resp: &fwresource.IdentitySchemaResponse{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.IdentitySchema(tt.args.in0, tt.args.in1, tt.args.resp)
		})
	}
}

func Test_portProfileResource_Schema(t *testing.T) {
	type args struct {
		ctx  context.Context
		req  fwresource.SchemaRequest
		resp *fwresource.SchemaResponse
	}
	tests := []struct {
		name           string
		r              *portProfileKitResource
		args           args
		wantAttributes []string
	}{
		{
			name: "schema contains key attributes",
			r:    newPortProfileKitResource(),
			args: args{
				ctx:  context.Background(),
				req:  fwresource.SchemaRequest{},
				resp: &fwresource.SchemaResponse{},
			},
			wantAttributes: []string{"id", "name", "op_mode", "autoneg", "site"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.Schema(tt.args.ctx, tt.args.req, tt.args.resp)
			for _, attr := range tt.wantAttributes {
				if _, ok := tt.args.resp.Schema.Attributes[attr]; !ok {
					t.Errorf("Schema() missing attribute %q", attr)
				}
			}
		})
	}
}

func TestPortProfileResourceSchemaTaggedNetworksAreComputed(t *testing.T) {
	r := newPortProfileKitResource()
	resp := &fwresource.SchemaResponse{}
	r.Schema(context.Background(), fwresource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Schema() diagnostics: %v", resp.Diagnostics)
	}

	attr, ok := resp.Schema.Attributes["tagged_networkconf_ids"].(resourceschema.SetAttribute)
	if !ok {
		t.Fatalf(
			"tagged_networkconf_ids schema type = %T, want schema.SetAttribute",
			resp.Schema.Attributes["tagged_networkconf_ids"],
		)
	}
	if !attr.IsOptional() || !attr.IsComputed() {
		t.Fatalf(
			"tagged_networkconf_ids Optional=%t Computed=%t, want true/true",
			attr.IsOptional(),
			attr.IsComputed(),
		)
	}
}

func Test_portProfileResource_UpgradeState(t *testing.T) {
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name string
		r    *portProfileKitResource
		args args
	}{
		{
			name: "returns non-nil map",
			r:    newPortProfileKitResource(),
			args: args{ctx: context.Background()},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.r.UpgradeState(tt.args.ctx)
			if got == nil {
				t.Error("UpgradeState() returned nil")
			}
		})
	}
}

func Test_portProfileResource_modelToAPIPortProfile(t *testing.T) {
	type args struct {
		ctx   context.Context
		model *portProfileKitModel
	}
	tests := []struct {
		name  string
		r     *portProfileKitResource
		args  args
		want  *unifi.PortProfile
		want1 diag.Diagnostics
	}{
		{
			name: "minimal model conversion",
			r:    newPortProfileKitResource(),
			args: args{
				ctx: context.Background(),
				model: &portProfileKitModel{
					ID:                         types.StringNull(),
					Site:                       types.StringNull(),
					Name:                       types.StringValue("test"),
					OpMode:                     types.StringValue("switch"),
					Autoneg:                    types.BoolValue(true),
					Dot1XCtrl:                  types.StringNull(),
					Dot1XIdleTimeout:           timetypes.NewGoDurationNull(),
					EgressRateLimitKbps:        types.Int64Null(),
					EgressRateLimitKbpsEnabled: types.BoolNull(),
					Forward:                    types.StringNull(),
					FullDuplex:                 types.BoolNull(),
					Isolation:                  types.BoolNull(),
					LLDPMedEnabled:             types.BoolNull(),
					LLDPMedNotifyEnabled:       types.BoolNull(),
					NativeNetworkConfID:        types.StringNull(),
					PoeMode:                    types.StringNull(),
					PortSecurityEnabled:        types.BoolNull(),
					PortSecurityMacAddress:     types.SetNull(types.StringType),
					PriorityQueue1Level:        types.Int64Null(),
					PriorityQueue2Level:        types.Int64Null(),
					PriorityQueue3Level:        types.Int64Null(),
					PriorityQueue4Level:        types.Int64Null(),
					Speed:                      types.Int64Null(),
					StormctrlBcastEnabled:      types.BoolNull(),
					StormctrlBcastLevel:        types.Int64Null(),
					StormctrlBcastRate:         types.Int64Null(),
					StormctrlMcastEnabled:      types.BoolNull(),
					StormctrlMcastLevel:        types.Int64Null(),
					StormctrlMcastRate:         types.Int64Null(),
					StormctrlType:              types.StringNull(),
					StormctrlUcastEnabled:      types.BoolNull(),
					StormctrlUcastLevel:        types.Int64Null(),
					StormctrlUcastRate:         types.Int64Null(),
					STPPortMode:                types.BoolNull(),
					TaggedNetworkConfIDs:       types.SetNull(types.StringType),
					VoiceNetworkConfID:         types.StringNull(),
					ExcludedNetworkConfIDs:     types.SetNull(types.StringType),
					MulticastRouterNetworkIDs:  types.SetNull(types.StringType),
					TaggedVLANMgmt:             types.StringNull(),
					FecMode:                    types.StringNull(),
					SettingPreference:          types.StringNull(),
					PortKeepaliveEnabled:       types.BoolNull(),
				},
			},
			want: &unifi.PortProfile{
				Name:    "test",
				OpMode:  "switch",
				Autoneg: true,
			},
			want1: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, diags := portProfileKitSpec().ToSDK(tt.args.ctx, tt.args.model)
			if diags.HasError() != (tt.want1 != nil) {
				t.Errorf("ToSDK() diagnostics = %v, want error: %v", diags, tt.want1 != nil)
			}
			// COMPARED AS THE JSON THE CONTROLLER RECEIVES. The expectations
			// are unchanged; only the notion of equality is. DeepEqual
			// separates a nil slice from an empty one, and all three slice
			// fields here -- excluded_networkconf_ids,
			// multicast_router_networkconf_ids and port_security_mac_address --
			// are tagged omitempty, so the two produce identical requests.
			// Checked against the SDK's tags rather than assumed: FirewallZone
			// has a collection that is NOT tagged, where the distinction is
			// real.
			gotJSON, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("marshalling the built object: %v", err)
			}
			wantJSON, err := json.Marshal(tt.want)
			if err != nil {
				t.Fatalf("marshalling the expectation: %v", err)
			}
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("ToSDK() sends\n  %s\nwant\n  %s", gotJSON, wantJSON)
			}
		})
	}
}

func Test_portProfileResource_portProfileToModel(t *testing.T) {
	type args struct {
		ctx   context.Context
		api   *unifi.PortProfile
		model *portProfileKitModel
		site  string
	}
	tests := []struct {
		name      string
		r         *portProfileKitResource
		args      args
		want      diag.Diagnostics
		checkFunc func(t *testing.T, model *portProfileKitModel)
	}{
		{
			name: "minimal API to model conversion",
			r:    newPortProfileKitResource(),
			args: args{
				ctx: context.Background(),
				api: &unifi.PortProfile{
					ID:     "abc123",
					Name:   "test-profile",
					OpMode: "switch",
				},
				model: &portProfileKitModel{},
				site:  "default",
			},
			want: nil,
			checkFunc: func(t *testing.T, model *portProfileKitModel) {
				if model.ID.ValueString() != "abc123" {
					t.Errorf("ID = %q, want %q", model.ID.ValueString(), "abc123")
				}
				if model.Name.ValueString() != "test-profile" {
					t.Errorf("Name = %q, want %q", model.Name.ValueString(), "test-profile")
				}
				if model.OpMode.ValueString() != "switch" {
					t.Errorf("OpMode = %q, want %q", model.OpMode.ValueString(), "switch")
				}
				if model.Site.ValueString() != "default" {
					t.Errorf("Site = %q, want %q", model.Site.ValueString(), "default")
				}
				if !model.PortSecurityMacAddress.IsNull() {
					t.Error("PortSecurityMacAddress should be null for empty API field")
				}
			},
		},
		{
			name: "custom mode preserves an empty exclusion set",
			r:    newPortProfileKitResource(),
			args: args{
				ctx: context.Background(),
				api: &unifi.PortProfile{
					ID:             "custom-empty",
					Name:           "custom-empty",
					OpMode:         "switch",
					TaggedVLANMgmt: "custom",
				},
				model: &portProfileKitModel{},
				site:  "default",
			},
			want: nil,
			checkFunc: func(t *testing.T, model *portProfileKitModel) {
				if model.ExcludedNetworkConfIDs.IsNull() {
					t.Fatal("ExcludedNetworkConfIDs is null, want an empty set")
				}
				if len(model.ExcludedNetworkConfIDs.Elements()) != 0 {
					t.Fatalf(
						"ExcludedNetworkConfIDs has %d elements, want 0",
						len(model.ExcludedNetworkConfIDs.Elements()),
					)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := portProfileToModelWithHooks(
				tt.args.ctx,
				tt.args.api,
				tt.args.model,
				tt.args.site,
			)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ToModel() = %v, want %v", got, tt.want)
			}
			if tt.checkFunc != nil {
				tt.checkFunc(t, tt.args.model)
			}
		})
	}
}

func TestPortProfileTaggedNetworkUniverse(t *testing.T) {
	vlan10 := int64(10)
	vlan20 := int64(20)
	vlan30 := int64(30)
	vlan40 := int64(40)
	vlan50 := int64(50)

	networks := []unifi.Network{
		{ID: "native", Purpose: unifi.PurposeCorporate, VLAN: &vlan10, VLANEnabled: true},
		{ID: "corporate", Purpose: unifi.PurposeCorporate, VLAN: &vlan20, VLANEnabled: true},
		{ID: "guest", Purpose: unifi.PurposeGuest, VLAN: &vlan30, VLANEnabled: true},
		{ID: "disabled-vlan", Purpose: unifi.PurposeVLANOnly, VLAN: &vlan40},
		{ID: "wan", Purpose: unifi.PurposeWAN, VLAN: &vlan50, VLANEnabled: true},
		{ID: "vpn", Purpose: unifi.PurposeUserVPN, VLAN: &vlan50, VLANEnabled: true},
		{ID: "untagged", Purpose: unifi.PurposeCorporate},
		{Purpose: unifi.PurposeVLANOnly, VLAN: &vlan50, VLANEnabled: true},
	}

	got := portProfileTaggedNetworkUniverse(networks, "native")
	want := []string{"corporate", "disabled-vlan", "guest"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("portProfileTaggedNetworkUniverse() = %v, want %v", got, want)
	}
}

func TestPortProfileExcludedNetworkIDs(t *testing.T) {
	universe := []string{"corporate", "disabled-vlan", "guest"}

	tests := []struct {
		name         string
		included     []string
		wantExcluded []string
		wantInvalid  []string
	}{
		{
			name:         "one selected network",
			included:     []string{"guest"},
			wantExcluded: []string{"corporate", "disabled-vlan"},
		},
		{
			name:         "empty set excludes every network",
			included:     []string{},
			wantExcluded: []string{"corporate", "disabled-vlan", "guest"},
		},
		{
			name:         "invalid IDs are reported in stable order",
			included:     []string{"missing-z", "guest", "missing-a"},
			wantExcluded: []string{"corporate", "disabled-vlan"},
			wantInvalid:  []string{"missing-a", "missing-z"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotExcluded, gotInvalid := portProfileExcludedNetworkIDs(universe, tt.included)
			if !reflect.DeepEqual(gotExcluded, tt.wantExcluded) {
				t.Errorf("excluded IDs = %v, want %v", gotExcluded, tt.wantExcluded)
			}
			if !reflect.DeepEqual(gotInvalid, tt.wantInvalid) {
				t.Errorf("invalid IDs = %v, want %v", gotInvalid, tt.wantInvalid)
			}
		})
	}
}

func TestPortProfileActualTaggedNetworkIDs(t *testing.T) {
	universe := []string{"corporate", "disabled-vlan", "guest"}

	tests := []struct {
		name     string
		mode     string
		excluded []string
		want     []string
	}{
		{name: "allow all", mode: "auto", want: universe},
		{name: "block all", mode: "block_all", want: []string{}},
		{
			name:     "custom complement",
			mode:     "custom",
			excluded: []string{"corporate", "unknown"},
			want:     []string{"disabled-vlan", "guest"},
		},
		{name: "unset mode has no tagged-network value", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := portProfileActualTaggedNetworkIDs(tt.mode, universe, tt.excluded)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("portProfileActualTaggedNetworkIDs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolvePortProfileVLANMode(t *testing.T) {
	tests := []struct {
		name               string
		taggedConfigured   bool
		taggedCount        int
		excludedConfigured bool
		configuredMode     string
		want               string
		wantError          bool
	}{
		{
			name:             "non-empty include set derives custom",
			taggedConfigured: true,
			taggedCount:      1,
			want:             "custom",
		},
		{name: "empty include set derives block all", taggedConfigured: true, want: "block_all"},
		{
			name:             "matching custom mode is accepted",
			taggedConfigured: true,
			taggedCount:      1,
			configuredMode:   "custom",
			want:             "custom",
		},
		{
			name:             "matching block all mode is accepted",
			taggedConfigured: true,
			configuredMode:   "block_all",
			want:             "block_all",
		},
		{
			name:             "allow all conflicts with include set",
			taggedConfigured: true,
			taggedCount:      1,
			configuredMode:   "auto",
			wantError:        true,
		},
		{
			name:             "block all conflicts with non-empty include set",
			taggedConfigured: true,
			taggedCount:      1,
			configuredMode:   "block_all",
			wantError:        true,
		},
		{
			name:             "custom conflicts with empty include set",
			taggedConfigured: true,
			configuredMode:   "custom",
			wantError:        true,
		},
		{name: "exclusions derive custom", excludedConfigured: true, want: "custom"},
		{
			name:               "explicit custom exclusions are accepted",
			excludedConfigured: true,
			configuredMode:     "custom",
			want:               "custom",
		},
		{
			name:               "allow all conflicts with exclusions",
			excludedConfigured: true,
			configuredMode:     "auto",
			wantError:          true,
		},
		{
			name:               "include and exclude sets conflict",
			taggedConfigured:   true,
			taggedCount:        1,
			excludedConfigured: true,
			wantError:          true,
		},
		{name: "direct allow all mode", configuredMode: "auto", want: "auto"},
		{name: "unset VLAN configuration", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolvePortProfileVLANMode(
				tt.taggedConfigured,
				tt.taggedCount,
				tt.excludedConfigured,
				tt.configuredMode,
			)
			if tt.wantError {
				if err == nil {
					t.Fatalf("resolvePortProfileVLANMode() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolvePortProfileVLANMode() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("resolvePortProfileVLANMode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolvePortProfileForward(t *testing.T) {
	tests := []struct {
		name              string
		mode              string
		configuredForward string
		want              string
		wantError         bool
	}{
		{name: "allow all uses all", mode: "auto", want: "all"},
		{name: "block all uses native", mode: "block_all", want: "native"},
		{name: "custom uses customize", mode: "custom", want: "customize"},
		{
			name:              "matching explicit value",
			mode:              "custom",
			configuredForward: "customize",
			want:              "customize",
		},
		{
			name:              "conflicting explicit value",
			mode:              "block_all",
			configuredForward: "customize",
			wantError:         true,
		},
		{
			name:              "forward without VLAN mode passes through",
			configuredForward: "disabled",
			want:              "disabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolvePortProfileForward(tt.mode, tt.configuredForward)
			if tt.wantError {
				if err == nil {
					t.Fatal("resolvePortProfileForward() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolvePortProfileForward() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("resolvePortProfileForward() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestApplyPortProfileVLANConfig(t *testing.T) {
	universe := []string{"corporate", "guest", "iot"}

	tests := []struct {
		name        string
		config      portProfileVLANConfig
		wantMode    string
		wantForward string
		wantExclude []string
		wantError   bool
	}{
		{
			name: "exact include set becomes custom exclusions",
			config: portProfileVLANConfig{
				TaggedConfigured: true,
				TaggedIDs:        []string{"guest"},
				Mode:             "custom",
			},
			wantMode:    "custom",
			wantForward: "customize",
			wantExclude: []string{"corporate", "iot"},
		},
		{
			name: "empty include set becomes block all",
			config: portProfileVLANConfig{
				TaggedConfigured: true,
				TaggedIDs:        []string{},
				Mode:             "block_all",
			},
			wantMode:    "block_all",
			wantForward: "native",
			wantExclude: nil,
		},
		{
			name: "raw exclusions pass through",
			config: portProfileVLANConfig{
				ExcludedConfigured: true,
				ExcludedIDs:        []string{"iot"},
				Mode:               "custom",
			},
			wantMode:    "custom",
			wantForward: "customize",
			wantExclude: []string{"iot"},
		},
		{
			name:        "direct allow all mode",
			config:      portProfileVLANConfig{Mode: "auto"},
			wantMode:    "auto",
			wantForward: "all",
			wantExclude: nil,
		},
		{
			name: "unknown include ID fails",
			config: portProfileVLANConfig{
				TaggedConfigured: true,
				TaggedIDs:        []string{"missing", "guest"},
				Mode:             "custom",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &unifi.PortProfile{}
			err := applyPortProfileVLANConfig(tt.config, universe, api)
			if tt.wantError {
				if err == nil {
					t.Fatal("applyPortProfileVLANConfig() error = nil, want error")
				}
				if api.TaggedVLANMgmt != "" || api.ExcludedNetworkIDs != nil {
					t.Fatalf("API profile mutated on error: %+v", api)
				}
				return
			}
			if err != nil {
				t.Fatalf("applyPortProfileVLANConfig() error = %v", err)
			}
			if api.TaggedVLANMgmt != tt.wantMode {
				t.Errorf("TaggedVLANMgmt = %q, want %q", api.TaggedVLANMgmt, tt.wantMode)
			}
			if api.Forward != tt.wantForward {
				t.Errorf("Forward = %q, want %q", api.Forward, tt.wantForward)
			}
			if !reflect.DeepEqual(api.ExcludedNetworkIDs, tt.wantExclude) {
				t.Errorf(
					"ExcludedNetworkIDs = %v, want %v",
					api.ExcludedNetworkIDs,
					tt.wantExclude,
				)
			}
		})
	}
}

// TestApplyPortProfileVLANConfig_UnconfiguredLeavesTaggedStateAlone guards the
// clobber the native_networkconf_id acceptance test below exposed. When
// nothing about tagged-VLAN management is configured, config.Mode is "" --
// not a value the controller accepts for tagged_vlan_mgmt -- and
// applyPortProfileVLANConfig must leave the SDK object's existing
// TaggedVLANMgmt/ExcludedNetworkIDs alone rather than overwrite them with the
// empty/nil zero. Unlike TestApplyPortProfileVLANConfig's cases, api here
// starts non-empty, the way ToSDK leaves it when state already carries a mode
// forward from a prior read -- an api that starts empty cannot distinguish a
// preserved value from a clobbered one.
func TestApplyPortProfileVLANConfig_UnconfiguredLeavesTaggedStateAlone(t *testing.T) {
	api := &unifi.PortProfile{
		TaggedVLANMgmt:     "auto",
		ExcludedNetworkIDs: []string{"stale"},
		Forward:            "all",
	}
	if err := applyPortProfileVLANConfig(portProfileVLANConfig{}, nil, api); err != nil {
		t.Fatalf("applyPortProfileVLANConfig() error = %v", err)
	}
	if api.TaggedVLANMgmt != "auto" {
		t.Errorf("TaggedVLANMgmt = %q, want the untouched \"auto\"", api.TaggedVLANMgmt)
	}
	if !reflect.DeepEqual(api.ExcludedNetworkIDs, []string{"stale"}) {
		t.Errorf("ExcludedNetworkIDs = %v, want the untouched [stale]", api.ExcludedNetworkIDs)
	}
	if api.Forward != "all" {
		t.Errorf("Forward = %q, want the untouched %q", api.Forward, "all")
	}
}

func TestSetPortProfileTaggedNetworkState(t *testing.T) {
	vlan10 := int64(10)
	vlan20 := int64(20)
	vlan30 := int64(30)
	networks := []unifi.Network{
		{ID: "native", Purpose: unifi.PurposeCorporate, VLAN: &vlan10},
		{ID: "guest", Purpose: unifi.PurposeGuest, VLAN: &vlan20},
		{ID: "iot", Purpose: unifi.PurposeVLANOnly, VLAN: &vlan30},
	}

	tests := []struct {
		name string
		api  unifi.PortProfile
		want []string
	}{
		{
			name: "allow all returns every eligible network",
			api: unifi.PortProfile{
				NATiveNetworkID: "native",
				TaggedVLANMgmt:  "auto",
			},
			want: []string{"guest", "iot"},
		},
		{
			name: "block all returns an empty set",
			api: unifi.PortProfile{
				NATiveNetworkID: "native",
				TaggedVLANMgmt:  "block_all",
			},
			want: []string{},
		},
		{
			name: "custom returns the inverse of exclusions",
			api: unifi.PortProfile{
				NATiveNetworkID:    "native",
				TaggedVLANMgmt:     "custom",
				ExcludedNetworkIDs: []string{"iot"},
			},
			want: []string{"guest"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &portProfileKitModel{}
			diags := setPortProfileTaggedNetworkState(
				context.Background(),
				&tt.api,
				networks,
				model,
			)
			if diags.HasError() {
				t.Fatalf("setPortProfileTaggedNetworkState() diagnostics: %v", diags)
			}
			var got []string
			diags = model.TaggedNetworkConfIDs.ElementsAs(context.Background(), &got, false)
			if diags.HasError() {
				t.Fatalf("decoding tagged_networkconf_ids: %v", diags)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("tagged_networkconf_ids = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_portProfileResource_ListResourceConfigSchema(t *testing.T) {
	type args struct {
		in0  context.Context
		in1  fwlist.ListResourceSchemaRequest
		resp *fwlist.ListResourceSchemaResponse
	}
	tests := []struct {
		name string
		r    *portProfileKitResource
		args args
	}{
		{
			name: "does not panic",
			r:    newPortProfileKitResource(),
			args: args{
				in0:  context.Background(),
				in1:  fwlist.ListResourceSchemaRequest{},
				resp: &fwlist.ListResourceSchemaResponse{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.ListResourceConfigSchema(tt.args.in0, tt.args.in1, tt.args.resp)
		})
	}
}

func TestAccPortProfileList_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_14_0),
		},
		Steps: []resource.TestStep{
			{
				Config: testAccPortProfileFrameworkConfig_basic(),
			},
			{
				Query: true,
				Config: `
					provider "unifi" {}
					list "unifi_port_profile" "test" {
						provider = unifi
						config {
							filter {
								name  = "name"
								value = "Test Port Profile"
						  }
					  }
					}
				`,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("unifi_port_profile.test", 1),
				},
			},
		},
	})
}

// portProfileToModelWithHooks is what portProfileToModel became: the field
// mapping plus AfterReceive, which is where the tagged-network reconstruction
// and the conditional excluded_networkconf_ids now live.
//
// The tests below assert both halves, so calling only Spec.ToModel would leave
// them checking a model the provider never produces.
func portProfileToModelWithHooks(
	ctx context.Context,
	api *unifi.PortProfile,
	model *portProfileKitModel,
	site string,
) diag.Diagnostics {
	spec := portProfileKitSpec()
	diags := spec.ToModel(ctx, api, model, site)
	diags.Append(portProfileAfterReceive(ctx, api, model, portProfileKitModel{}, []unifi.Network(nil))...)
	return diags
}

// THE DERIVED VLAN FIELDS MUST BE IN THE UPDATE MASK, and nothing asserted it
// until removing AlwaysWire from the descriptor left the whole suite green.
//
// The practitioner writes tagged_networkconf_ids, which is not a Field and so
// cannot put anything in the mask. The three attributes that carry the change
// are computed by BeforeSend. Without AlwaysWire the mask names none of them
// and the update is accepted having changed nothing.
func TestPortProfileWireMaskCarriesTheDerivedVLANFields(t *testing.T) {
	ctx := context.Background()
	tagged, diags := types.SetValueFrom(ctx, types.StringType, []string{"net-a"})
	if diags.HasError() {
		t.Fatalf("building the tagged set: %v", diags)
	}
	plan := &portProfileKitModel{
		Name:                 types.StringValue("uplink"),
		TaggedNetworkConfIDs: tagged,
		// Every attribute that maps to one of the three derived wire fields is
		// left null, which is the state after an import or a create the
		// controller answered with empty values.
		TaggedVLANMgmt:         types.StringNull(),
		Forward:                types.StringNull(),
		ExcludedNetworkConfIDs: types.SetNull(types.StringType),
	}

	fields, err := portProfileKitSpec().WireFields(plan)
	if err != nil {
		t.Fatalf("WireFields: %v", err)
	}
	for _, wanted := range []string{"tagged_vlan_mgmt", "excluded_networkconf_ids", "forward"} {
		if !slices.Contains(fields, wanted) {
			t.Errorf("the mask omits %q, so a tagged-VLAN change would write nothing: %v",
				wanted, fields)
		}
	}
	// The control: an attribute nobody planned and nobody derives stays out,
	// or the assertions above would hold for a mask naming every field.
	if slices.Contains(fields, "poe_mode") {
		t.Errorf("the mask names poe_mode, which the plan never set: %v", fields)
	}
}

// The three read defaults, none of which had a test. Each is a value the
// hand-written mapper substituted when the controller reported nothing, and
// removing any of them from the descriptor left the suite green.
func TestPortProfileReadDefaults(t *testing.T) {
	ctx := context.Background()
	for _, testCase := range []struct {
		attribute string
		reported  string
		read      func(*portProfileKitModel) string
		want      string
	}{
		{"dot1x_ctrl", "", func(m *portProfileKitModel) string { return m.Dot1XCtrl.ValueString() }, "force_authorized"},
		{"forward", "", func(m *portProfileKitModel) string { return m.Forward.ValueString() }, "native"},
		{"op_mode", "", func(m *portProfileKitModel) string { return m.OpMode.ValueString() }, "switch"},
	} {
		t.Run(testCase.attribute+" defaults on an empty read", func(t *testing.T) {
			var model portProfileKitModel
			api := &unifi.PortProfile{}
			if d := portProfileKitSpec().ToModel(ctx, api, &model, "default"); d.HasError() {
				t.Fatalf("ToModel: %v", d)
			}
			if got := testCase.read(&model); got != testCase.want {
				t.Errorf(
					"%s = %q on an empty read, want %q",
					testCase.attribute,
					got,
					testCase.want,
				)
			}
		})
	}

	// The control: a value the controller DID report is not overwritten by the
	// default, or the cases above would hold for a descriptor that ignored the
	// controller entirely.
	var model portProfileKitModel
	api := &unifi.PortProfile{Dot1XCtrl: "auto", Forward: "customize", OpMode: "aggregate"}
	if d := portProfileKitSpec().ToModel(ctx, api, &model, "default"); d.HasError() {
		t.Fatalf("ToModel: %v", d)
	}
	for _, pair := range []struct{ name, got, want string }{
		{"dot1x_ctrl", model.Dot1XCtrl.ValueString(), "auto"},
		{"forward", model.Forward.ValueString(), "customize"},
		{"op_mode", model.OpMode.ValueString(), "aggregate"},
	} {
		if pair.got != pair.want {
			t.Errorf("%s = %q, want the reported %q", pair.name, pair.got, pair.want)
		}
	}
}

// lldpmed_notify_enabled is the only Optional-only bool this surface has: no
// Computed, no Default. BoolField's ToModel writes whatever the controller
// reports, unconditionally, which is right when a false IS a value -- but an
// Optional-only attribute commits the schema to returning exactly what the
// config said, so a config that never mentioned the attribute must come back
// null. The earlier hand-written read carried the prior value forward for
// exactly this reason (its "Only set lldpmed_notify_enabled if it was in the
// plan or if it's explicitly true" comment); AfterReceive is where the kit
// lets a surface do the same thing.
func TestPortProfileLLDPMedNotifyEnabledPreservesNull(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name     string
		prior    types.Bool
		reported bool
		want     types.Bool
	}{
		{"never configured, controller reports false", types.BoolNull(), false, types.BoolNull()},
		{"never configured, controller reports true", types.BoolNull(), true, types.BoolValue(true)},
		{"previously true, controller reports false", types.BoolValue(true), false, types.BoolValue(false)},
		{"previously false, controller reports false", types.BoolValue(false), false, types.BoolValue(false)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			prior := portProfileKitModel{LLDPMedNotifyEnabled: tt.prior}
			model := portProfileKitModel{LLDPMedNotifyEnabled: tt.prior}
			api := &unifi.PortProfile{LldpmedNotifyEnabled: tt.reported}
			if d := portProfileKitSpec().ToModel(ctx, api, &model, "default"); d.HasError() {
				t.Fatalf("ToModel: %v", d)
			}
			if d := portProfileAfterReceive(ctx, api, &model, prior, []unifi.Network(nil)); d.HasError() {
				t.Fatalf("AfterReceive: %v", d)
			}
			if !model.LLDPMedNotifyEnabled.Equal(tt.want) {
				t.Errorf("LLDPMedNotifyEnabled = %#v, want %#v", model.LLDPMedNotifyEnabled, tt.want)
			}
		})
	}
}

// TestPortProfileToModel_NativeNetworkClearedRoundTrips and
// TestPortProfileToModel_NativeNetworkAssignedKept are adapted from upstream's
// #383 fix (allow clearing native_networkconf_id to None). Upstream's tests
// called the hand-written portProfileResource.portProfileToModel directly;
// the kit has no such method, so the equivalent boundary is Spec.ToModel --
// which already surfaces the controller's "" as a known empty string rather
// than null, because the descriptor declares native_networkconf_id KeepZero
// (the pre-fix, hand-written behavior used NullZero's rule by hand instead:
// `if != "" {Value} else {Null}`). Kept as regression tests: the kit already
// had this fix, and nothing had pinned it.
func TestPortProfileToModel_NativeNetworkClearedRoundTrips(t *testing.T) {
	ctx := context.Background()
	var model portProfileKitModel
	api := &unifi.PortProfile{NATiveNetworkID: ""}
	if d := portProfileKitSpec().ToModel(ctx, api, &model, "default"); d.HasError() {
		t.Fatalf("ToModel: %v", d)
	}
	if model.NativeNetworkConfID.IsNull() || model.NativeNetworkConfID.IsUnknown() ||
		model.NativeNetworkConfID.ValueString() != "" {
		t.Errorf(
			"native_networkconf_id: want known empty string, got %#v",
			model.NativeNetworkConfID,
		)
	}
}

func TestPortProfileToModel_NativeNetworkAssignedKept(t *testing.T) {
	ctx := context.Background()
	var model portProfileKitModel
	api := &unifi.PortProfile{NATiveNetworkID: "net-123"}
	if d := portProfileKitSpec().ToModel(ctx, api, &model, "default"); d.HasError() {
		t.Fatalf("ToModel: %v", d)
	}
	if got := model.NativeNetworkConfID.ValueString(); got != "net-123" {
		t.Errorf("native_networkconf_id = %q, want net-123", got)
	}
}

// TestPortProfileWireMaskCarriesExplicitNativeNetworkClear guards the write
// half of #383: an explicit native_networkconf_id = "" is a known, non-null
// plan value like any other, so it must join the update mask. Without it, the
// controller never sees the clear at all -- go-unifi's maskedBody only
// substitutes the field's zero value onto the wire for a NAMED field the
// normal encoding dropped at its zero value (native_networkconf_id still
// carries `omitempty`); a mask that omitted the name would omit the field
// outright and the clear would silently no-op, which is the exact failure
// #383 reported.
func TestPortProfileWireMaskCarriesExplicitNativeNetworkClear(t *testing.T) {
	plan := &portProfileKitModel{
		Name:                types.StringValue("uplink"),
		NativeNetworkConfID: types.StringValue(""),
	}
	fields, err := portProfileKitSpec().WireFields(plan)
	if err != nil {
		t.Fatalf("WireFields: %v", err)
	}
	if !slices.Contains(fields, "native_networkconf_id") {
		t.Errorf("the mask omits native_networkconf_id for an explicit clear: %v", fields)
	}
}

// TestAccPortProfileFramework_nativeNetworkClear is #383's live end-to-end
// case: an explicit native_networkconf_id = "" must clear the native network
// on the controller, not merely disappear from Terraform state.
//
// State alone cannot tell the two apart. Update overlays a known plan value
// onto state unconditionally (internal/resourcekit/resource.go's
// ApplyPlanToState, "a set plan value always survives"), so even a write that
// silently failed to clear the controller's value would still show "" in
// state right after the apply that set it. The PlanOnly step re-reads the
// controller independently of that overlay -- Resource.Read never calls
// ApplyPlanToState -- and would produce a nonempty plan if the clear had not
// actually taken effect.
func TestAccPortProfileFramework_nativeNetworkClear(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccPortProfileFrameworkConfig_nativeNetworkClear(
					"unifi_network.native.id",
				),
				Check: resource.TestCheckResourceAttrPair(
					"unifi_port_profile.clear",
					"native_networkconf_id",
					"unifi_network.native",
					"id",
				),
			},
			{
				Config: testAccPortProfileFrameworkConfig_nativeNetworkClear(`""`),
				Check: resource.TestCheckResourceAttr(
					"unifi_port_profile.clear",
					"native_networkconf_id",
					"",
				),
			},
			{
				// The independent re-read: see the function comment.
				Config:   testAccPortProfileFrameworkConfig_nativeNetworkClear(`""`),
				PlanOnly: true,
			},
		},
	})
}

func testAccPortProfileFrameworkConfig_nativeNetworkClear(native string) string {
	return fmt.Sprintf(`
resource "unifi_network" "native" {
	name                = "Port Profile Native Clear"
	subnet              = "192.168.190.1/24"
	vlan                = 190
	third_party_gateway = true
}

resource "unifi_port_profile" "clear" {
	name                  = "Port Profile Native Clear"
	native_networkconf_id = %s
	depends_on            = [unifi_network.native]
}
`, native)
}
