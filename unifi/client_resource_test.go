package unifi

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	"github.com/hashicorp/terraform-plugin-framework-nettypes/iptypes"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwlist "github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

// TestClientToModel_DefaultsWhenAPIOmitsFields proves the fix for the spurious
// in-place diff on every create/import: when the controller omits blocked / groups /
// qos_rate (as UniFi OS 5.x / Network App 10.x does for fixed-IP-only clients),
// Read must store the documented default (blocked=false) rather than null, and leave
// groups / qos_rate null so the UseStateForUnknown plan modifiers can keep the plan
// clean. For this minimal client clientToModel makes no API calls, so no live
// controller (and no mock) is needed.
func TestClientToModel_DefaultsWhenAPIOmitsFields(t *testing.T) {
	r := newClientKitResource()

	client := &unifi.Client{
		ID:                     "61d1...",
		MAC:                    "02:00:00:de:ad:01",
		Name:                   "tf-test",
		FixedIP:                "192.168.40.251",
		Blocked:                nil, // controller omitted "blocked"
		UserGroupID:            "",  // no qos_rate / usergroup
		NetworkMembersGroupIDs: nil, // no groups
	}

	var model clientModel
	diags := r.clientToModel(context.Background(), client, &model, "default")
	if diags.HasError() {
		t.Fatalf("clientToModel returned errors: %v", diags)
	}

	if model.Blocked.IsNull() || model.Blocked.IsUnknown() {
		t.Errorf("blocked: want concrete value, got null/unknown (%#v)", model.Blocked)
	}
	if model.Blocked.ValueBool() != false {
		t.Errorf("blocked: want false, got %v", model.Blocked.ValueBool())
	}
	if !model.Groups.IsNull() {
		t.Errorf("groups: want null, got %#v", model.Groups)
	}
	if !model.QOSRate.IsNull() {
		t.Errorf("qos_rate: want null, got %#v", model.QOSRate)
	}
}

// TestClientToModel_DefaultsImportOnlyAttributes is the regression test for
// the import diff: ImportState seeds only id/site (see ImportState in
// resourcekit's resource.go), so the model Read starts from has
// allow_existing and skip_forget_on_destroy null -- neither is a Field, so
// nothing in ToModel touches them, and they stayed null through the rest of
// Read. The schema's Default() only fires on Create (no prior state); on the
// plan that follows an import, prior state now genuinely holds null, so
// Default() applies there too and proposes a change FROM that null, which
// Terraform reports as a spurious "1 to change". The hand-written Read
// defaulted both attributes for exactly this reason (v0.102.0
// clientResource.Read, right before resp.State.Set); AfterReceive is this
// surface's equivalent hook, run on Read as well as Create.
func TestClientToModel_DefaultsImportOnlyAttributes(t *testing.T) {
	r := newClientKitResource()
	client := &unifi.Client{ID: "61d1...", MAC: "02:00:00:de:ad:04", Name: "tf-test"}

	model := clientModel{
		AllowExisting:       types.BoolNull(),
		SkipForgetOnDestroy: types.BoolNull(),
	}
	diags := r.clientToModel(context.Background(), client, &model, "default")
	if diags.HasError() {
		t.Fatalf("clientToModel returned errors: %v", diags)
	}

	if model.AllowExisting.IsNull() || model.AllowExisting.IsUnknown() {
		t.Errorf("allow_existing: want concrete value, got null/unknown (%#v)", model.AllowExisting)
	}
	if !model.AllowExisting.ValueBool() {
		t.Errorf("allow_existing: want true (the schema default), got false")
	}
	if model.SkipForgetOnDestroy.IsNull() || model.SkipForgetOnDestroy.IsUnknown() {
		t.Errorf("skip_forget_on_destroy: want concrete value, got null/unknown (%#v)",
			model.SkipForgetOnDestroy)
	}
	if model.SkipForgetOnDestroy.ValueBool() {
		t.Errorf("skip_forget_on_destroy: want false (the schema default), got true")
	}
}

// TestClientToModel_PreservesBlockedTrue ensures a blocked client still round-trips.
func TestClientToModel_PreservesBlockedTrue(t *testing.T) {
	r := newClientKitResource()
	blocked := true
	client := &unifi.Client{MAC: "02:00:00:de:ad:02", Blocked: &blocked}

	var model clientModel
	if diags := r.clientToModel(context.Background(), client, &model, "default"); diags.HasError() {
		t.Fatalf("clientToModel returned errors: %v", diags)
	}
	if model.Blocked.ValueBool() != true {
		t.Errorf("blocked: want true, got %v", model.Blocked.ValueBool())
	}
}

func TestAccClientFramework_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccClientFrameworkConfig_basic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("unifi_client.test", "name", "tfacc-client"),
					resource.TestCheckResourceAttr("unifi_client.test", "mac", "01:23:45:67:89:ab"),
					resource.TestCheckResourceAttr("unifi_client.test", "blocked", "false"),
				),
			},
			{
				ResourceName:    "unifi_client.test",
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
			},
		},
	})
}

func testAccClientFrameworkConfig_basic() string {
	return `
resource "unifi_client" "test" {
	name = "tfacc-client"
	mac  = "01:23:45:67:89:ab"
}
`
}

// TestAccClientFramework_importByMAC is the regression test for the parity
// gap the kit cutover opened: v0.102.0 imported a client by mac alone, both
// via the CLI's bare `terraform import unifi_client.x <mac>` and an identity
// block naming only mac -- see git show v0.102.0:unifi/client_resource.go
// around line 700. The kit's generic ImportState only ever understood id, so
// a mac handle here used to land straight in the id attribute and fail the
// read that followed with "Cannot import non-existent remote object". This
// exercises the CLI/id-argument shape (ImportStateId); TestClientImportHandle
// covers the identity-block-naming-only-mac shape without a live controller.
func TestAccClientFramework_importByMAC(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccClientFrameworkConfig_importByMAC(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_client.test",
						"name",
						"tfacc-import-by-mac-client",
					),
					resource.TestCheckResourceAttr("unifi_client.test", "mac", "01:23:45:67:89:af"),
				),
			},
			{
				ResourceName:            "unifi_client.test",
				ImportState:             true,
				ImportStateId:           "01:23:45:67:89:af",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"allow_existing", "skip_forget_on_destroy"},
			},
		},
	})
}

func testAccClientFrameworkConfig_importByMAC() string {
	return `
resource "unifi_client" "test" {
	name = "tfacc-import-by-mac-client"
	mac  = "01:23:45:67:89:af"
}
`
}

func TestAccClientFramework_blocked(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccClientFrameworkConfig_blocked(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_client.test",
						"name",
						"tfacc-blocked-client",
					),
					resource.TestCheckResourceAttr("unifi_client.test", "blocked", "true"),
					resource.TestCheckResourceAttr(
						"unifi_client.test",
						"note",
						"Blocked for testing",
					),
				),
			},
		},
	})
}

func testAccClientFrameworkConfig_blocked() string {
	return `
resource "unifi_client" "test" {
	name    = "tfacc-blocked-client"
	mac     = "01:23:45:67:89:ac"
	blocked = true
	note    = "Blocked for testing"
}
`
}

func TestAccClientFramework_fixedIP(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccClientFrameworkConfig_fixedIP(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_client.test",
						"name",
						"tfacc-fixed-ip-client",
					),
					resource.TestCheckResourceAttr(
						"unifi_client.test",
						"fixed_ip",
						"192.168.2.100",
					),
				),
			},
		},
	})
}

func testAccClientFrameworkConfig_fixedIP() string {
	return `
resource "unifi_network" "test" {
	name    = "Test"
	subnet  = "192.168.2.1/24"
	vlan    = 2

	dhcp_server = {
		enabled    = true
		start = "192.168.2.6"
		stop  = "192.168.2.254"
	}
}

resource "unifi_client" "test" {
	name       = "tfacc-fixed-ip-client"
	mac        = "01:23:45:67:89:ad"
	fixed_ip   = "192.168.2.100"
	network_id = unifi_network.test.id
}
`
}

func TestAccClientFramework_groups(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create client with one group
			{
				Config: testAccClientFrameworkConfig_groups_one(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_client.test",
						"name",
						"tfacc-groups-client",
					),
					resource.TestCheckResourceAttr("unifi_client.test", "mac", "01:23:45:67:89:ae"),
					resource.TestCheckResourceAttr("unifi_client.test", "groups.#", "1"),
					resource.TestCheckResourceAttr(
						"unifi_client.test",
						"groups.0",
						"tfacc-group-a",
					),
				),
			},
			// Step 2: Import the client and verify groups survive
			{
				ResourceName:            "unifi_client.test",
				ImportState:             true,
				ImportStateKind:         resource.ImportBlockWithResourceIdentity,
				ImportStateVerifyIgnore: []string{"allow_existing", "skip_forget_on_destroy"},
			},
			// Step 3: Add another group
			{
				Config: testAccClientFrameworkConfig_groups_two(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_client.test",
						"name",
						"tfacc-groups-client",
					),
					resource.TestCheckResourceAttr("unifi_client.test", "groups.#", "2"),
					resource.TestCheckResourceAttr(
						"unifi_client.test",
						"groups.0",
						"tfacc-group-a",
					),
					resource.TestCheckResourceAttr(
						"unifi_client.test",
						"groups.1",
						"tfacc-group-b",
					),
				),
			},
		},
	})
}

func testAccClientFrameworkConfig_groups_one() string {
	return `
resource "unifi_client" "test" {
	name   = "tfacc-groups-client"
	mac    = "01:23:45:67:89:ae"
	groups = ["tfacc-group-a"]
}
`
}

func testAccClientFrameworkConfig_groups_two() string {
	return `
resource "unifi_client" "test" {
	name   = "tfacc-groups-client"
	mac    = "01:23:45:67:89:ae"
	groups = ["tfacc-group-a", "tfacc-group-b"]
}
`
}

func TestNewClientResource(t *testing.T) {
	// A populated Spec carries closures, which are never DeepEqual, so the
	// thing worth asserting is the type the provider registers.
	got := NewClientResource()
	if got == nil {
		t.Fatal("NewClientResource() = nil")
	}
	if _, ok := got.(*clientKitResource); !ok {
		t.Errorf("NewClientResource() = %T, want *clientKitResource", got)
	}
}

func TestNewClientListResource(t *testing.T) {
	// A populated Spec carries closures, which are never DeepEqual, so the
	// thing worth asserting is the type the provider registers.
	got := NewClientListResource()
	if got == nil {
		t.Fatal("NewClientListResource() = nil")
	}
	if _, ok := got.(*clientKitResource); !ok {
		t.Errorf("NewClientListResource() = %T, want *clientKitResource", got)
	}
}

func Test_qosRateModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    qosRateModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct attribute types",
			m:    qosRateModel{},
			want: map[string]attr.Type{
				"id":       types.StringType,
				"name":     types.StringType,
				"max_up":   types.Int64Type,
				"max_down": types.Int64Type,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("qosRateModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_clientResource_IdentitySchema(t *testing.T) {
	type args struct {
		in0  context.Context
		in1  fwresource.IdentitySchemaRequest
		resp *fwresource.IdentitySchemaResponse
	}
	tests := []struct {
		name string
		r    *clientKitResource
		args args
	}{
		{
			name: "returns identity schema",
			r:    newClientKitResource(),
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

			// mac MUST be in the identity schema: List (see client_resource.go)
			// sets identity by mac rather than id, because a listed client's mac
			// is the handle the practitioner recognizes, and writing to an
			// attribute the identity schema does not declare is a hard
			// "Resource Identity Write Error", not a diff.
			//
			// NEITHER attribute is RequiredForImport: v0.102.0 imported a client
			// by mac alone, no id in sight, and every other kit surface imports
			// by id alone. Marking either required would make an import block
			// naming only the other one invalid before ImportState ever runs --
			// exactly the "identity = { mac = ... }" gap this schema exists to
			// close.
			id, ok := tt.args.resp.IdentitySchema.Attributes["id"]
			if !ok {
				t.Fatal(`identity schema is missing "id"`)
			}
			if id.IsRequiredForImport() {
				t.Error(`"id" should not be required for import: a mac-only ` +
					"identity block must be valid on its own")
			}
			if !id.IsOptionalForImport() {
				t.Error(`"id" should be optional for import`)
			}
			mac, ok := tt.args.resp.IdentitySchema.Attributes["mac"]
			if !ok {
				t.Fatal(`identity schema is missing "mac", which List sets`)
			}
			if mac.IsRequiredForImport() {
				t.Error(`"mac" should not be required for import: the generic ` +
					"Create/Read/Update path never writes it")
			}
			if !mac.IsOptionalForImport() {
				t.Error(`"mac" should be optional for import`)
			}
		})
	}
}

// clientTestIdentity builds an empty resource identity bound to client's own
// identity schema (id and mac, both optional-for-import -- see IdentitySchema
// in client_kit_resource.go), the way a real import block's `identity = {...}`
// argument would arrive.
func clientTestIdentity(t *testing.T) tfsdk.ResourceIdentity {
	t.Helper()
	ctx := context.Background()
	r := newClientKitResource()
	resp := &fwresource.IdentitySchemaResponse{}
	r.IdentitySchema(ctx, fwresource.IdentitySchemaRequest{}, resp)
	identity := tfsdk.ResourceIdentity{Schema: resp.IdentitySchema}
	identity.Raw = tftypes.NewValue(resp.IdentitySchema.Type().TerraformType(ctx), nil)
	return identity
}

// TestClientImportHandle is the unit coverage for the routing half of mac
// import: clientImportHandle decides WHAT to resolve and WHETHER it needs a
// mac lookup, without making one -- that part needs a live api.ApiClient (see
// TestAccClientFramework_importByMAC for the API call itself). v0.102.0
// imported a client by mac two ways -- the CLI's bare
// `terraform import unifi_client.x <mac>` and an identity block naming only
// mac (git show v0.102.0:unifi/client_resource.go around line 700) -- and both
// have to keep working alongside the id-only import every other kit surface
// gets.
func TestClientImportHandle(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		req        func(t *testing.T) fwresource.ImportStateRequest
		wantHandle string
		wantIsMAC  bool
	}{
		{
			name: "a bare 24-hex id from the CLI is not a mac",
			req: func(*testing.T) fwresource.ImportStateRequest {
				return fwresource.ImportStateRequest{ID: "6a8b3cd94c934471f6b6ff20"}
			},
			wantHandle: "6a8b3cd94c934471f6b6ff20",
			wantIsMAC:  false,
		},
		{
			name: "a site:id pair from the CLI is not a mac",
			req: func(*testing.T) fwresource.ImportStateRequest {
				return fwresource.ImportStateRequest{ID: "default:6a8b3cd94c934471f6b6ff20"}
			},
			wantHandle: "default:6a8b3cd94c934471f6b6ff20",
			wantIsMAC:  false,
		},
		{
			name: "a bare mac from the CLI routes to resolution",
			req: func(*testing.T) fwresource.ImportStateRequest {
				return fwresource.ImportStateRequest{ID: "01:23:45:67:89:ab"}
			},
			wantHandle: "01:23:45:67:89:ab",
			wantIsMAC:  true,
		},
		{
			name: "an identity block naming only id is not a mac",
			req: func(t *testing.T) fwresource.ImportStateRequest {
				ctx := context.Background()
				identity := clientTestIdentity(t)
				if diags := identity.SetAttribute(ctx, path.Root("id"),
					"6a8b3cd94c934471f6b6ff20"); diags.HasError() {
					t.Fatalf("seeding identity: %v", diags)
				}
				return fwresource.ImportStateRequest{Identity: &identity}
			},
			wantHandle: "6a8b3cd94c934471f6b6ff20",
			wantIsMAC:  false,
		},
		{
			name: "an identity block naming only mac routes to resolution",
			req: func(t *testing.T) fwresource.ImportStateRequest {
				ctx := context.Background()
				identity := clientTestIdentity(t)
				if diags := identity.SetAttribute(ctx, path.Root("mac"),
					hwtypes.NewMACAddressValue("01:23:45:67:89:ab")); diags.HasError() {
					t.Fatalf("seeding identity: %v", diags)
				}
				return fwresource.ImportStateRequest{Identity: &identity}
			},
			wantHandle: "01:23:45:67:89:ab",
			wantIsMAC:  true,
		},
		{
			name: "id wins when an identity block somehow carries both",
			req: func(t *testing.T) fwresource.ImportStateRequest {
				ctx := context.Background()
				identity := clientTestIdentity(t)
				if diags := identity.SetAttribute(ctx, path.Root("id"),
					"6a8b3cd94c934471f6b6ff20"); diags.HasError() {
					t.Fatalf("seeding identity id: %v", diags)
				}
				if diags := identity.SetAttribute(ctx, path.Root("mac"),
					hwtypes.NewMACAddressValue("01:23:45:67:89:ab")); diags.HasError() {
					t.Fatalf("seeding identity mac: %v", diags)
				}
				return fwresource.ImportStateRequest{Identity: &identity}
			},
			wantHandle: "6a8b3cd94c934471f6b6ff20",
			wantIsMAC:  false,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			handle, isMAC, diags := clientImportHandle(t.Context(), testCase.req(t))
			if diags.HasError() {
				t.Fatalf("clientImportHandle: %v", diags)
			}
			if handle != testCase.wantHandle {
				t.Errorf("handle = %q, want %q", handle, testCase.wantHandle)
			}
			if isMAC != testCase.wantIsMAC {
				t.Errorf("isMAC = %v, want %v", isMAC, testCase.wantIsMAC)
			}
		})
	}
}

func Test_clientResource_Schema(t *testing.T) {
	type args struct {
		ctx  context.Context
		req  fwresource.SchemaRequest
		resp *fwresource.SchemaResponse
	}
	tests := []struct {
		name string
		r    *clientKitResource
		args args
	}{
		{
			name: "returns schema",
			r:    newClientKitResource(),
			args: args{
				ctx:  context.Background(),
				req:  fwresource.SchemaRequest{},
				resp: &fwresource.SchemaResponse{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.Schema(tt.args.ctx, tt.args.req, tt.args.resp)
		})
	}
}

// THE COMPANION FLAGS, which is the half of mergeClient that did not become
// structural.
//
// mergeClient started from the fetched object and overlaid the planned writable
// fields, so UniFi's internal fields survived a write. The field mask does that
// by construction now: a field the mask does not name is never in the body, so
// there is nothing to preserve it FROM. That half needs no test.
//
// What it also did was derive use_fixedip from whether fixed_ip was set, and
// local_dns_record_enabled likewise -- the controller ignores the value without
// the flag. That is a derivation, it lives in BeforeSend now, and it is what
// this asserts. The clearing direction is the one that mattered: an emptied
// fixed_ip has to turn the flag OFF, or the controller keeps applying the old
// address.
func TestClientBeforeSendDerivesTheCompanionFlags(t *testing.T) {
	for _, testCase := range []struct {
		name         string
		fixedIP      iptypes.IPv4Address
		fixedApMAC   hwtypes.MACAddress
		dnsRecord    types.String
		networkID    types.String
		wantFixed    bool
		wantFixedAp  bool
		wantDNSFlag  bool
		wantOverride bool
	}{
		{
			name:       "a set fixed_ip turns the flag on",
			fixedIP:    iptypes.NewIPv4AddressValue("192.168.1.100"),
			fixedApMAC: hwtypes.NewMACAddressNull(),
			dnsRecord:  types.StringNull(),
			networkID:  types.StringNull(),
			wantFixed:  true,
		},
		{
			name:       "an emptied fixed_ip turns the flag off",
			fixedIP:    iptypes.NewIPv4AddressValue(""),
			fixedApMAC: hwtypes.NewMACAddressNull(),
			dnsRecord:  types.StringNull(),
			networkID:  types.StringNull(),
			wantFixed:  false,
		},
		{
			name:       "a null fixed_ip turns the flag off",
			fixedIP:    iptypes.NewIPv4AddressNull(),
			fixedApMAC: hwtypes.NewMACAddressNull(),
			dnsRecord:  types.StringNull(),
			networkID:  types.StringNull(),
			wantFixed:  false,
		},
		{
			name:        "local_dns_record carries its own flag",
			fixedIP:     iptypes.NewIPv4AddressNull(),
			fixedApMAC:  hwtypes.NewMACAddressNull(),
			dnsRecord:   types.StringValue("host.example"),
			networkID:   types.StringNull(),
			wantDNSFlag: true,
		},
		{
			name:         "a set network_id turns the override flag on",
			fixedIP:      iptypes.NewIPv4AddressNull(),
			fixedApMAC:   hwtypes.NewMACAddressNull(),
			dnsRecord:    types.StringNull(),
			networkID:    types.StringValue("6a8b3cd94c934471f6b6ff20"),
			wantOverride: true,
		},
		{
			name:         "a null network_id turns the override flag off, not absent",
			fixedIP:      iptypes.NewIPv4AddressNull(),
			fixedApMAC:   hwtypes.NewMACAddressNull(),
			dnsRecord:    types.StringNull(),
			networkID:    types.StringNull(),
			wantOverride: false,
		},
		{
			name:        "a set fixed_ap_mac turns its own flag on",
			fixedIP:     iptypes.NewIPv4AddressNull(),
			fixedApMAC:  hwtypes.NewMACAddressValue("02:00:00:de:ad:05"),
			dnsRecord:   types.StringNull(),
			networkID:   types.StringNull(),
			wantFixedAp: true,
		},
		{
			name:        "a null fixed_ap_mac turns its own flag off",
			fixedIP:     iptypes.NewIPv4AddressNull(),
			fixedApMAC:  hwtypes.NewMACAddressNull(),
			dnsRecord:   types.StringNull(),
			networkID:   types.StringNull(),
			wantFixedAp: false,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			model := clientModel{
				FixedIP:        testCase.fixedIP,
				FixedApMAC:     testCase.fixedApMAC,
				LocalDNSRecord: testCase.dnsRecord,
				NetworkID:      testCase.networkID,
				QOSRate:        types.ObjectNull(qosRateModel{}.AttributeTypes()),
				Groups:         types.ListNull(types.StringType),
			}
			sdk := &unifi.Client{}
			// A nil api is safe here: with qos_rate and groups both null there
			// is nothing for BeforeSend to look up or create.
			hook := clientKitBeforeSend(nil, "default")
			if diags := hook(t.Context(), &model, &model, sdk, &clientGroups{}); diags.HasError() {
				t.Fatalf("BeforeSend: %v", diags)
			}
			if sdk.UseFixedIP != testCase.wantFixed {
				t.Errorf("use_fixedip = %v, want %v", sdk.UseFixedIP, testCase.wantFixed)
			}
			if sdk.FixedApEnabled != testCase.wantFixedAp {
				t.Errorf("fixed_ap_enabled = %v, want %v", sdk.FixedApEnabled, testCase.wantFixedAp)
			}
			if sdk.LocalDNSRecordEnabled != testCase.wantDNSFlag {
				t.Errorf("local_dns_record_enabled = %v, want %v",
					sdk.LocalDNSRecordEnabled, testCase.wantDNSFlag)
			}
			// NEVER NIL: virtual_network_override_enabled is in AlwaysWire, so
			// every update sends it, and a nil *bool serializes as a literal
			// JSON null the controller rejects with api.err.InvalidValue.
			if sdk.VirtualNetworkOverrideEnabled == nil {
				t.Fatal("virtual_network_override_enabled = nil, want a concrete bool")
			}
			if *sdk.VirtualNetworkOverrideEnabled != testCase.wantOverride {
				t.Errorf("virtual_network_override_enabled = %v, want %v",
					*sdk.VirtualNetworkOverrideEnabled, testCase.wantOverride)
			}
		})
	}
}

func Test_clientResource_ListResourceConfigSchema(t *testing.T) {
	type args struct {
		ctx  context.Context
		req  fwlist.ListResourceSchemaRequest
		resp *fwlist.ListResourceSchemaResponse
	}
	tests := []struct {
		name string
		r    *clientKitResource
		args args
	}{
		{
			name: "returns list schema",
			r:    newClientKitResource(),
			args: args{
				ctx:  context.Background(),
				req:  fwlist.ListResourceSchemaRequest{},
				resp: &fwlist.ListResourceSchemaResponse{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.ListResourceConfigSchema(tt.args.ctx, tt.args.req, tt.args.resp)
		})
	}
}

// nullPortOverrideAttrValues returns every port-override attribute set to its
// typed null, so a test only has to override the few fields it cares about.
func nullPortOverrideAttrValues() map[string]attr.Value {
	attrs := portOverrideAttrTypes()
	vals := make(map[string]attr.Value, len(attrs))
	for name, t := range attrs {
		switch tt := t.(type) {
		case basetypes.StringType:
			vals[name] = types.StringNull()
		case basetypes.Int64Type:
			vals[name] = types.Int64Null()
		case basetypes.BoolType:
			vals[name] = types.BoolNull()
		case basetypes.ListType:
			vals[name] = types.ListNull(tt.ElemType)
		case timetypes.GoDurationType:
			vals[name] = timetypes.NewGoDurationNull()
		}
		// Any unhandled attr type is intentionally left out so ObjectValue fails
		// loudly (signalling the helper needs updating) rather than silently.
	}
	return vals
}

func portOverrideSetWith(t *testing.T, overrides map[string]attr.Value) types.Set {
	t.Helper()
	attrs := nullPortOverrideAttrValues()
	for k, v := range overrides {
		attrs[k] = v
	}
	obj, d := types.ObjectValue(portOverrideAttrTypes(), attrs)
	if d.HasError() {
		t.Fatalf("building port override object: %v", d)
	}
	set, d := types.SetValue(
		types.ObjectType{AttrTypes: portOverrideAttrTypes()},
		[]attr.Value{obj},
	)
	if d.HasError() {
		t.Fatalf("building port override set: %v", d)
	}
	return set
}

// TestFrameworkToPortOverrides_AggregateOpMode guards #177: to form an SFP+ link
// aggregation the port's op_mode must be written as "aggregate" alongside the
// aggregate_members. op_mode is otherwise skipped (default "switch") so gateway
// devices that reject op_mode on PUT keep working (#213).
func TestFrameworkToPortOverrides_AggregateOpMode(t *testing.T) {
	ctx := context.Background()

	members, d := types.ListValue(types.Int64Type, []attr.Value{
		types.Int64Value(9),
		types.Int64Value(10),
	})
	if d.HasError() {
		t.Fatalf("building members list: %v", d)
	}

	set := portOverrideSetWith(t, map[string]attr.Value{
		"index":             types.Int64Value(9),
		"op_mode":           types.StringValue("aggregate"),
		"aggregate_members": members,
	})

	pos, diags := devicePortOverridesFromModel(ctx, set)
	if diags.HasError() {
		t.Fatalf("devicePortOverridesFromModel errored: %v", diags)
	}
	if len(pos) != 1 {
		t.Fatalf("got %d port overrides, want 1", len(pos))
	}
	po := pos[0]
	if po.OpMode != "aggregate" {
		t.Errorf("OpMode = %q, want aggregate (LAG would not engage)", po.OpMode)
	}
	if len(po.AggregateMembers) != 2 || po.AggregateMembers[0] != 9 ||
		po.AggregateMembers[1] != 10 {
		t.Errorf("AggregateMembers = %v, want [9 10]", po.AggregateMembers)
	}
}

// TestFrameworkToPortOverrides_SwitchOpModeOmitted ensures the default "switch"
// op_mode is not sent on the wire (it has omitempty), preserving the gateway
// write fix (#213).
func TestFrameworkToPortOverrides_SwitchOpModeOmitted(t *testing.T) {
	ctx := context.Background()

	set := portOverrideSetWith(t, map[string]attr.Value{
		"index":   types.Int64Value(1),
		"op_mode": types.StringValue("switch"),
	})

	pos, diags := devicePortOverridesFromModel(ctx, set)
	if diags.HasError() {
		t.Fatalf("devicePortOverridesFromModel errored: %v", diags)
	}
	if len(pos) != 1 {
		t.Fatalf("got %d port overrides, want 1", len(pos))
	}
	if pos[0].OpMode != "" {
		t.Errorf("OpMode = %q, want empty (omitted) for the switch default", pos[0].OpMode)
	}
}

// TestPortOverridesToFramework_TaggedNetworkIDsTypedNull is a regression test
// for #235. portOverridesToFramework must initialize the tagged_networkconf_ids
// model field to a typed null list. Previously it was left as an untyped
// zero-value types.List, which made types.ObjectValueFrom fail with a
// "types.ListType[!!! MISSING TYPE !!!]" Value Conversion Error during the
// Read/refresh (and import) of any unifi_device that has port overrides.
func TestPortOverridesToFramework_TaggedNetworkIDsTypedNull(t *testing.T) {
	obj, diags := devicePortOverrideDecode(
		context.Background(), unifi.DevicePortOverrides{Name: "Port 1"},
	)

	if diags.HasError() {
		t.Fatalf(
			"devicePortOverrideDecode returned diagnostics (regression #235): %v",
			diags.Errors(),
		)
	}
	if obj.IsNull() {
		t.Fatal("expected a non-null port_override object for a single override")
	}

	taggedAttr, ok := obj.Attributes()["tagged_networkconf_ids"]
	if !ok {
		t.Fatal("port_override is missing the tagged_networkconf_ids attribute")
	}

	list, ok := taggedAttr.(types.List)
	if !ok {
		t.Fatalf("expected tagged_networkconf_ids to be types.List, got %T", taggedAttr)
	}
	if !list.IsNull() {
		t.Errorf("expected tagged_networkconf_ids to be a null list, got %v", list)
	}
	if et := list.ElementType(context.Background()); !et.Equal(types.StringType) {
		t.Errorf("expected tagged_networkconf_ids element type to be string, got %v", et)
	}
}

func TestAccClientList_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_14_0),
		},
		Steps: []resource.TestStep{
			{
				Config: testAccClientFrameworkConfig_basic(),
			},
			{
				Query: true,
				Config: `
					provider "unifi" {}
					list "unifi_client" "test" {
						provider = unifi
						config {
							filter {
								name  = "name"
								value = "tfacc-client"
						  }
					  }
					}
				`,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("unifi_client.test", 1),
					querycheck.ExpectIdentity("unifi_client.test", map[string]knownvalue.Check{
						"mac": knownvalue.StringExact("01:23:45:67:89:ab"),
						"id":  knownvalue.NotNull(),
					}),
				},
			},
		},
	})
}

// A SHIM, so the two read-path tests keep asserting exactly what they asserted
// before the surface moved onto the kit.
//
// clientToModel is gone; ToModel fills the Fields and AfterReceive derives
// qos_rate and groups. Rewriting the call sites would have meant re-deriving
// their expectations by hand, which is how a migration quietly changes what a
// test checks.
//
// nil prefetched is the point of these two cases rather than an omission: this
// client has no usergroup and no member groups, so there is nothing to look up
// and the typed nulls are what the assertions are about.
func (r *clientKitResource) clientToModel(
	ctx context.Context,
	client *unifi.Client,
	model *clientModel,
	site string,
) diag.Diagnostics {
	diags := r.Spec.ToModel(ctx, client, model, site)
	diags.Append(r.Spec.AfterReceive(ctx, client, model, clientModel{}, nil)...)
	return diags
}
