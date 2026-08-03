package unifi

import (
	"context"
	"fmt"
	"reflect"
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

func Test_portProfileResource_Metadata(t *testing.T) {
	type args struct {
		ctx  context.Context
		req  fwresource.MetadataRequest
		resp *fwresource.MetadataResponse
	}
	tests := []struct {
		name         string
		r            *portProfileResource
		args         args
		wantTypeName string
	}{
		{
			name: "type name with provider prefix",
			r:    &portProfileResource{},
			args: args{
				ctx:  context.Background(),
				req:  fwresource.MetadataRequest{ProviderTypeName: "unifi"},
				resp: &fwresource.MetadataResponse{},
			},
			wantTypeName: "unifi_port_profile",
		},
		{
			name: "type name with empty provider prefix",
			r:    &portProfileResource{},
			args: args{
				ctx:  context.Background(),
				req:  fwresource.MetadataRequest{ProviderTypeName: ""},
				resp: &fwresource.MetadataResponse{},
			},
			wantTypeName: "_port_profile",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.Metadata(tt.args.ctx, tt.args.req, tt.args.resp)
			if tt.args.resp.TypeName != tt.wantTypeName {
				t.Errorf(
					"Metadata() TypeName = %q, want %q",
					tt.args.resp.TypeName,
					tt.wantTypeName,
				)
			}
		})
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
		r    *portProfileResource
		args args
	}{
		{
			name: "does not panic",
			r:    &portProfileResource{},
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
		r              *portProfileResource
		args           args
		wantAttributes []string
	}{
		{
			name: "schema contains key attributes",
			r:    &portProfileResource{},
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
	r := &portProfileResource{}
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
		r    *portProfileResource
		args args
	}{
		{
			name: "returns non-nil map",
			r:    &portProfileResource{},
			args: args{ctx: context.Background()},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.r.UpgradeState(tt.args.ctx)
			if got == nil {
				t.Error("portProfileResource.UpgradeState() returned nil")
			}
		})
	}
}

func Test_portProfileResource_Configure(t *testing.T) {
	type args struct {
		ctx  context.Context
		req  fwresource.ConfigureRequest
		resp *fwresource.ConfigureResponse
	}
	tests := []struct {
		name       string
		r          *portProfileResource
		args       args
		wantErr    bool
		wantClient bool
	}{
		{
			name: "nil provider data produces no error",
			r:    &portProfileResource{},
			args: args{
				ctx:  context.Background(),
				req:  fwresource.ConfigureRequest{ProviderData: nil},
				resp: &fwresource.ConfigureResponse{},
			},
			wantErr:    false,
			wantClient: false,
		},
		{
			name: "wrong type produces error",
			r:    &portProfileResource{},
			args: args{
				ctx:  context.Background(),
				req:  fwresource.ConfigureRequest{ProviderData: "wrong-type"},
				resp: &fwresource.ConfigureResponse{},
			},
			wantErr:    true,
			wantClient: false,
		},
		{
			name: "correct Client type sets client",
			r:    &portProfileResource{},
			args: args{
				ctx:  context.Background(),
				req:  fwresource.ConfigureRequest{ProviderData: &Client{}},
				resp: &fwresource.ConfigureResponse{},
			},
			wantErr:    false,
			wantClient: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.Configure(tt.args.ctx, tt.args.req, tt.args.resp)
			if tt.wantErr && !tt.args.resp.Diagnostics.HasError() {
				t.Error("Configure() expected error diagnostic, got none")
			}
			if !tt.wantErr && tt.args.resp.Diagnostics.HasError() {
				t.Errorf("Configure() unexpected error: %v", tt.args.resp.Diagnostics.Errors())
			}
			if tt.wantClient && tt.r.client == nil {
				t.Error("Configure() expected client to be set, got nil")
			}
		})
	}
}

func Test_portProfileResource_modelToAPIPortProfile(t *testing.T) {
	type args struct {
		ctx   context.Context
		model *portProfileResourceModel
	}
	tests := []struct {
		name  string
		r     *portProfileResource
		args  args
		want  *unifi.PortProfile
		want1 diag.Diagnostics
	}{
		{
			name: "minimal model conversion",
			r:    &portProfileResource{},
			args: args{
				ctx: context.Background(),
				model: &portProfileResourceModel{
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
			got, got1 := tt.r.modelToAPIPortProfile(tt.args.ctx, tt.args.model)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf(
					"portProfileResource.modelToAPIPortProfile() got = %+v, want %+v",
					got,
					tt.want,
				)
			}
			if !reflect.DeepEqual(got1, tt.want1) {
				t.Errorf(
					"portProfileResource.modelToAPIPortProfile() got1 = %v, want %v",
					got1,
					tt.want1,
				)
			}
		})
	}
}

func Test_portProfileResource_portProfileToModel(t *testing.T) {
	type args struct {
		ctx   context.Context
		api   *unifi.PortProfile
		model *portProfileResourceModel
		site  string
	}
	tests := []struct {
		name      string
		r         *portProfileResource
		args      args
		want      diag.Diagnostics
		checkFunc func(t *testing.T, model *portProfileResourceModel)
	}{
		{
			name: "minimal API to model conversion",
			r:    &portProfileResource{},
			args: args{
				ctx: context.Background(),
				api: &unifi.PortProfile{
					ID:     "abc123",
					Name:   "test-profile",
					OpMode: "switch",
				},
				model: &portProfileResourceModel{},
				site:  "default",
			},
			want: nil,
			checkFunc: func(t *testing.T, model *portProfileResourceModel) {
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
			r:    &portProfileResource{},
			args: args{
				ctx: context.Background(),
				api: &unifi.PortProfile{
					ID:             "custom-empty",
					Name:           "custom-empty",
					OpMode:         "switch",
					TaggedVLANMgmt: "custom",
				},
				model: &portProfileResourceModel{},
				site:  "default",
			},
			want: nil,
			checkFunc: func(t *testing.T, model *portProfileResourceModel) {
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
			got := tt.r.portProfileToModel(tt.args.ctx, tt.args.api, tt.args.model, tt.args.site)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("portProfileResource.portProfileToModel() = %v, want %v", got, tt.want)
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
			model := &portProfileResourceModel{}
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
		r    *portProfileResource
		args args
	}{
		{
			name: "does not panic",
			r:    &portProfileResource{},
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
