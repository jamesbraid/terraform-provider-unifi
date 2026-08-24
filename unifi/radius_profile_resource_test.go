package unifi

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	fwlist "github.com/hashicorp/terraform-plugin-framework/list"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

func TestAccRadiusProfile_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccRadiusProfileConfig_basic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_radius_profile.test", "id"),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"name",
						"tfacc-radius-profile",
					),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"accounting_enabled",
						"false",
					),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"interim_update_enabled",
						"false",
					),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"interim_update_interval",
						"1h0m0s",
					),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"use_usg_acct_server",
						"false",
					),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"use_usg_auth_server",
						"false",
					),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"vlan_enabled",
						"false",
					),
				),
			},
			{
				ResourceName:      "unifi_radius_profile.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccRadiusProfile_withAuthServer(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccRadiusProfileConfig_withAuthServer(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_radius_profile.test", "id"),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"name",
						"tfacc-radius-profile-auth",
					),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"auth_server.#",
						"1",
					),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"auth_server.0.ip",
						"192.168.1.100",
					),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"auth_server.0.port",
						"1812",
					),
				),
			},
			{
				ResourceName:      "unifi_radius_profile.test",
				ImportState:       true,
				ImportStateVerify: true,
				// Secrets are not returned by the API on read
				ImportStateVerifyIgnore: []string{"auth_server.0.secret"},
			},
		},
	})
}

func TestAccRadiusProfile_withAuthServerCustomPort(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccRadiusProfileConfig_withAuthServerCustomPort(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"auth_server.#",
						"1",
					),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"auth_server.0.ip",
						"10.0.0.1",
					),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"auth_server.0.port",
						"1822",
					),
				),
			},
		},
	})
}

func TestAccRadiusProfile_withAcctServer(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccRadiusProfileConfig_withAcctServer(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_radius_profile.test", "id"),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"name",
						"tfacc-radius-profile-acct",
					),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"accounting_enabled",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"acct_server.#",
						"1",
					),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"acct_server.0.ip",
						"192.168.1.101",
					),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"acct_server.0.port",
						"1813",
					),
				),
			},
			{
				ResourceName:      "unifi_radius_profile.test",
				ImportState:       true,
				ImportStateVerify: true,
				// Secrets are not returned by the API on read
				ImportStateVerifyIgnore: []string{"acct_server.0.secret"},
			},
		},
	})
}

func TestAccRadiusProfile_withAuthAndAcctServers(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccRadiusProfileConfig_withAuthAndAcctServers(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_radius_profile.test", "id"),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"auth_server.#",
						"1",
					),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"acct_server.#",
						"1",
					),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"accounting_enabled",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"auth_server.0.ip",
						"192.168.1.100",
					),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"acct_server.0.ip",
						"192.168.1.101",
					),
				),
			},
			{
				ResourceName:      "unifi_radius_profile.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"auth_server.0.secret",
					"acct_server.0.secret",
				},
			},
		},
	})
}

func TestAccRadiusProfile_withInterimUpdate(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccRadiusProfileConfig_withInterimUpdate(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"interim_update_enabled",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"interim_update_interval",
						"30m0s",
					),
				),
			},
			{
				ResourceName:      "unifi_radius_profile.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccRadiusProfile_withVlan(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccRadiusProfileConfig_withVlan(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"vlan_enabled",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"vlan_wlan_mode",
						"required",
					),
				),
			},
			{
				ResourceName:      "unifi_radius_profile.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccRadiusProfile_update(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccRadiusProfileConfig_basic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"name",
						"tfacc-radius-profile",
					),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"accounting_enabled",
						"false",
					),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"interim_update_interval",
						"1h0m0s",
					),
				),
			},
			{
				Config: testAccRadiusProfileConfig_updated(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"name",
						"tfacc-radius-profile-updated",
					),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"accounting_enabled",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"interim_update_enabled",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_radius_profile.test",
						"interim_update_interval",
						"30m0s",
					),
				),
			},
		},
	})
}

func TestAccRadiusProfile_importWithSite(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccRadiusProfileConfig_basic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_radius_profile.test", "id"),
					resource.TestCheckResourceAttrSet("unifi_radius_profile.test", "site"),
				),
			},
			{
				ResourceName:      "unifi_radius_profile.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccRadiusProfileConfig_basic() string {
	return `
resource "unifi_radius_profile" "test" {
  name = "tfacc-radius-profile"
}
`
}

func testAccRadiusProfileConfig_withAuthServer() string {
	return `
resource "unifi_radius_profile" "test" {
  name = "tfacc-radius-profile-auth"

  auth_server {
    ip     = "192.168.1.100"
    secret = "test-auth-secret"
  }
}
`
}

func testAccRadiusProfileConfig_withAuthServerCustomPort() string {
	return `
resource "unifi_radius_profile" "test" {
  name = "tfacc-radius-profile-auth-port"

  auth_server {
    ip     = "10.0.0.1"
    port   = 1822
    secret = "test-auth-secret"
  }
}
`
}

func testAccRadiusProfileConfig_withAcctServer() string {
	return `
resource "unifi_radius_profile" "test" {
  name               = "tfacc-radius-profile-acct"
  accounting_enabled = true

  acct_server {
    ip     = "192.168.1.101"
    secret = "test-acct-secret"
  }
}
`
}

func testAccRadiusProfileConfig_withAuthAndAcctServers() string {
	return `
resource "unifi_radius_profile" "test" {
  name               = "tfacc-radius-profile-full"
  accounting_enabled = true

  auth_server {
    ip     = "192.168.1.100"
    secret = "test-auth-secret"
  }

  acct_server {
    ip     = "192.168.1.101"
    secret = "test-acct-secret"
  }
}
`
}

func testAccRadiusProfileConfig_withInterimUpdate() string {
	return `
resource "unifi_radius_profile" "test" {
  name                    = "tfacc-radius-profile-interim"
  accounting_enabled      = true
  interim_update_enabled  = true
  interim_update_interval = "30m0s"
}
`
}

func testAccRadiusProfileConfig_withVlan() string {
	return `
resource "unifi_radius_profile" "test" {
  name           = "tfacc-radius-profile-vlan"
  vlan_enabled   = true
  vlan_wlan_mode = "required"
}
`
}

func testAccRadiusProfileConfig_updated() string {
	return `
resource "unifi_radius_profile" "test" {
  name                    = "tfacc-radius-profile-updated"
  accounting_enabled      = true
  interim_update_enabled  = true
  interim_update_interval = "30m0s"
}
`
}

func TestNewRadiusProfileResource(t *testing.T) {
	r := NewRadiusProfileResource()
	if r == nil {
		t.Fatal("NewRadiusProfileResource() returned nil")
	}
	if _, ok := r.(fwresource.ResourceWithImportState); !ok {
		t.Error("expected ResourceWithImportState interface")
	}
	if _, ok := r.(fwresource.ResourceWithUpgradeState); !ok {
		t.Error("expected ResourceWithUpgradeState interface")
	}
}

func TestNewRadiusProfileListResource(t *testing.T) {
	r := NewRadiusProfileListResource()
	if r == nil {
		t.Fatal("NewRadiusProfileListResource() returned nil")
	}
	if _, ok := r.(fwlist.ListResourceWithConfigure); !ok {
		t.Error("expected ListResourceWithConfigure interface")
	}
}

func Test_radiusProfileKitResource_IdentitySchema(t *testing.T) {
	r := newRadiusProfileKitResource()
	resp := &fwresource.IdentitySchemaResponse{}
	r.IdentitySchema(context.Background(), fwresource.IdentitySchemaRequest{}, resp)
	if _, ok := resp.IdentitySchema.Attributes["id"]; !ok {
		t.Error("IdentitySchema missing 'id' attribute")
	}
}

// Test_radiusProfileKitResource_UpgradeState guards the half of a migration
// that is easiest to drop: state written by an older provider does not stop
// existing because the resource moved to the kit. v0 stored
// interim_update_interval as integer seconds.
func Test_radiusProfileKitResource_UpgradeState(t *testing.T) {
	r := newRadiusProfileKitResource()
	upgraders := r.UpgradeState(context.Background())
	if _, ok := upgraders[0]; !ok {
		t.Fatal("no upgrader from schema version 0; state written before the duration " +
			"change would fail to read")
	}
}

func Test_radiusProfileKitResource_ListResourceConfigSchema(t *testing.T) {
	r := newRadiusProfileKitResource()
	resp := &fwlist.ListResourceSchemaResponse{}
	r.ListResourceConfigSchema(context.Background(), fwlist.ListResourceSchemaRequest{}, resp)
	if _, ok := resp.Schema.Attributes["site"]; !ok {
		t.Error("ListResourceConfigSchema missing 'site' attribute")
	}
}

// radiusServerList builds a list of server objects the way the schema types
// them, so the tests below drive the real ObjectListField rather than a
// convenient stand-in.
func radiusServerList(t *testing.T, servers ...[3]string) types.List {
	t.Helper()
	ctx := context.Background()
	elements := make([]attr.Value, 0, len(servers))
	for _, s := range servers {
		port := types.Int64Null()
		if s[1] != "" {
			var n int64
			if _, err := fmt.Sscanf(s[1], "%d", &n); err != nil {
				t.Fatalf("port %q: %v", s[1], err)
			}
			port = types.Int64Value(n)
		}
		object, d := types.ObjectValue(radiusServerAttrTypes(), map[string]attr.Value{
			"ip": types.StringValue(s[0]), "port": port, "secret": types.StringValue(s[2]),
		})
		if d.HasError() {
			t.Fatal(d)
		}
		elements = append(elements, object)
	}
	list, d := types.ListValue(types.ObjectType{AttrTypes: radiusServerAttrTypes()}, elements)
	if d.HasError() {
		t.Fatal(d)
	}
	_ = ctx
	return list
}

// Test_radiusProfileKit_writePath checks that scalars reach the SDK and the
// server blocks arrive as elements rather than being dropped.
func Test_radiusProfileKit_writePath(t *testing.T) {
	ctx := context.Background()
	spec := radiusProfileKitSpec()

	model := radiusProfileKitModel{
		Name:              types.StringValue("profile-1"),
		AccountingEnabled: types.BoolValue(true),
		VlanEnabled:       types.BoolValue(true),
		VlanWlanMode:      types.StringValue("required"),
		AuthServer:        radiusServerList(t, [3]string{"1.2.3.4", "1812", "s3cret"}),
		AcctServer:        radiusServerList(t),
	}
	sdk, diags := spec.ToSDK(ctx, &model)
	if diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}
	if sdk.Name != "profile-1" || !sdk.AccountingEnabled || !sdk.VLANEnabled {
		t.Errorf("scalars did not reach the SDK: %+v", sdk)
	}
	if len(sdk.AuthServers) != 1 {
		t.Fatalf("auth_servers has %d element(s), want 1", len(sdk.AuthServers))
	}
	if sdk.AuthServers[0].IP != "1.2.3.4" || sdk.AuthServers[0].Secret != "s3cret" {
		t.Errorf("auth server = %+v, want the configured IP and secret", sdk.AuthServers[0])
	}
	if sdk.AuthServers[0].Port == nil || *sdk.AuthServers[0].Port != 1812 {
		t.Errorf("auth server port = %v, want 1812", sdk.AuthServers[0].Port)
	}
}

func Test_radiusProfileKit_readPath(t *testing.T) {
	ctx := context.Background()
	spec := radiusProfileKitSpec()
	port := int64(1813)

	var model radiusProfileKitModel
	profile := &unifi.RADIUSProfile{
		ID: "prof-1", Name: "profile-1", AccountingEnabled: true,
		AcctServers: []unifi.RADIUSProfileAcctServers{{IP: "5.6.7.8", Port: &port, Secret: "a"}},
	}
	if diags := spec.ToModel(ctx, profile, &model, "default"); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	if model.Name.ValueString() != "profile-1" || !model.AccountingEnabled.ValueBool() {
		t.Errorf("scalars did not reach the model: %+v", model)
	}
	if n := len(model.AcctServer.Elements()); n != 1 {
		t.Fatalf("acct_server has %d element(s), want 1", n)
	}
	// An absent list stays null (not an empty block) -- what NullZero on the
	// field means: no auth_server block, so state should say so too.
	if !model.AuthServer.IsNull() {
		t.Errorf("auth_server = %v, want null when the controller returned none",
			model.AuthServer)
	}
}

// Test_radiusProfileKit_planOverridesStatePerField checks the kit's
// ApplyPlanToState, which copies field by field from Spec.Fields: a field
// missing from the descriptor would silently stop following the plan while
// every other field kept working.
func Test_radiusProfileKit_planOverridesStatePerField(t *testing.T) {
	spec := radiusProfileKitSpec()

	plan := radiusProfileKitModel{
		Name:              types.StringValue("new"),
		AccountingEnabled: types.BoolValue(true),
		VlanWlanMode:      types.StringValue("required"),
		AuthServer:        radiusServerList(t, [3]string{"1.1.1.1", "1812", "x"}),
	}
	state := radiusProfileKitModel{
		ID:                types.StringValue("prof-1"),
		Name:              types.StringValue("old"),
		AccountingEnabled: types.BoolValue(false),
		VlanWlanMode:      types.StringValue("disabled"),
		AuthServer:        types.ListNull(types.ObjectType{AttrTypes: radiusServerAttrTypes()}),
	}
	spec.ApplyPlanToState(&plan, &state)

	if state.Name.ValueString() != "new" {
		t.Errorf("name = %q, want the plan's", state.Name.ValueString())
	}
	if !state.AccountingEnabled.ValueBool() {
		t.Error("accounting_enabled did not follow the plan")
	}
	if state.VlanWlanMode.ValueString() != "required" {
		t.Errorf("vlan_wlan_mode = %q, want the plan's", state.VlanWlanMode.ValueString())
	}
	if state.AuthServer.IsNull() {
		t.Error("auth_server did not follow the plan; a block missing from Spec.Fields " +
			"stops following the plan while every scalar keeps working")
	}
	// The control: a field the plan does NOT set keeps what state held, which is
	// what preserves a value the controller assigned.
	if state.ID.ValueString() != "prof-1" {
		t.Errorf("id = %q, want the state's; an unset plan value must not clear it",
			state.ID.ValueString())
	}
}

// tls_enabled is force-emitted by go-unifi and the provider does not model
// it, so a whole-object write would reset it on every apply. The kit's mask
// comes from Spec.Fields, so a field with no entry has no wire name to send.
func Test_radiusProfileKit_neverWritesTLSEnabled(t *testing.T) {
	spec := radiusProfileKitSpec()
	names := spec.WireNames()
	for _, name := range names {
		if name == "tls_enabled" {
			t.Fatal("tls_enabled is a declared field, so it goes on the wire and a write " +
				"resets it on every apply")
		}
	}
	// The control: the derivation produces the names it should, so the check
	// above is not passing because WireNames() came back empty.
	want := map[string]bool{
		"name": true, "accounting_enabled": true, "interim_update_enabled": true,
		"interim_update_interval": true, "use_usg_acct_server": true,
		"use_usg_auth_server": true, "vlan_enabled": true, "vlan_wlan_mode": true,
		"acct_servers": true, "auth_servers": true,
	}
	got := map[string]bool{}
	for _, name := range names {
		got[name] = true
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("WireNames() = %v, want %v", got, want)
	}
}

func TestAccRadiusProfileList_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_14_0),
		},
		Steps: []resource.TestStep{
			{
				Config: testAccRadiusProfileConfig_basic(),
			},
			{
				Query: true,
				Config: `
					provider "unifi" {}
					list "unifi_radius_profile" "test" {
						provider = unifi
						config {
							filter {
								name  = "name"
								value = "tfacc-radius-profile"
						  }
					  }
					}
				`,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("unifi_radius_profile.test", 1),
				},
			},
		},
	})
}
