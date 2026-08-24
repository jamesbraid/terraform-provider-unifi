package unifi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
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
	diags := r.clientToModel(context.Background(), client, &model)
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
// Terraform reports as a spurious "1 to change". The earlier hand-written
// clientResource.Read defaulted both attributes for exactly this reason,
// right before resp.State.Set; AfterReceive is this surface's equivalent
// hook, run on Read as well as Create.
func TestClientToModel_DefaultsImportOnlyAttributes(t *testing.T) {
	r := newClientKitResource()
	client := &unifi.Client{ID: "61d1...", MAC: "02:00:00:de:ad:04", Name: "tf-test"}

	model := clientModel{
		AllowExisting:       types.BoolNull(),
		SkipForgetOnDestroy: types.BoolNull(),
	}
	diags := r.clientToModel(context.Background(), client, &model)
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
	if diags := r.clientToModel(context.Background(), client, &model); diags.HasError() {
		t.Fatalf("clientToModel returned errors: %v", diags)
	}
	if model.Blocked.ValueBool() != true {
		t.Errorf("blocked: want true, got %v", model.Blocked.ValueBool())
	}
}

// TestClientToModel_FixedIPDisabledIgnoresEcho: the controller keeps echoing
// the last fixed IP after use_fixedip turns off, and a read that trusted
// the echo would make an explicit fixed_ip = "" (the documented way to
// clear it) never round-trip.
func TestClientToModel_FixedIPDisabledIgnoresEcho(t *testing.T) {
	r := newClientKitResource()
	client := &unifi.Client{
		MAC:        "02:00:00:de:ad:06",
		FixedIP:    "192.168.1.50", // stale: what a formerly-fixed client still echoes
		UseFixedIP: false,
	}

	var model clientModel
	if diags := r.clientToModel(context.Background(), client, &model); diags.HasError() {
		t.Fatalf("clientToModel returned errors: %v", diags)
	}
	if model.FixedIP.IsNull() || model.FixedIP.IsUnknown() {
		t.Fatalf("fixed_ip: want a known value, got null/unknown (%#v)", model.FixedIP)
	}
	if got := model.FixedIP.ValueString(); got != "" {
		t.Errorf(`fixed_ip: want "" (the disabled/cleared state), got %q`, got)
	}
}

// TestClientToModel_FixedIPEnabledKeepsValue is the enabled-side twin: a real
// fixed IP still reads back as itself.
func TestClientToModel_FixedIPEnabledKeepsValue(t *testing.T) {
	r := newClientKitResource()
	client := &unifi.Client{
		MAC:        "02:00:00:de:ad:07",
		FixedIP:    "192.168.1.50",
		UseFixedIP: true,
	}

	var model clientModel
	if diags := r.clientToModel(context.Background(), client, &model); diags.HasError() {
		t.Fatalf("clientToModel returned errors: %v", diags)
	}
	if got := model.FixedIP.ValueString(); got != "192.168.1.50" {
		t.Errorf("fixed_ip: want the real address, got %q", got)
	}
}

// TestClientToModel_LocalDNSRecordDisabledIgnoresEcho covers the same
// stale-echo shape as fixed_ip, one attribute over.
func TestClientToModel_LocalDNSRecordDisabledIgnoresEcho(t *testing.T) {
	r := newClientKitResource()
	client := &unifi.Client{
		MAC:                   "02:00:00:de:ad:08",
		LocalDNSRecord:        "mqtt.home.arpa",
		LocalDNSRecordEnabled: false,
	}

	var model clientModel
	if diags := r.clientToModel(context.Background(), client, &model); diags.HasError() {
		t.Fatalf("clientToModel returned errors: %v", diags)
	}
	if model.LocalDNSRecord.IsNull() || model.LocalDNSRecord.IsUnknown() {
		t.Fatalf("local_dns_record: want a known value, got null/unknown (%#v)", model.LocalDNSRecord)
	}
	if got := model.LocalDNSRecord.ValueString(); got != "" {
		t.Errorf(`local_dns_record: want "" (the disabled/cleared state), got %q`, got)
	}
}

// TestClientToModel_LocalDNSRecordEnabledKeepsValue is the enabled-side twin.
func TestClientToModel_LocalDNSRecordEnabledKeepsValue(t *testing.T) {
	r := newClientKitResource()
	client := &unifi.Client{
		MAC:                   "02:00:00:de:ad:09",
		LocalDNSRecord:        "mqtt.home.arpa",
		LocalDNSRecordEnabled: true,
	}

	var model clientModel
	if diags := r.clientToModel(context.Background(), client, &model); diags.HasError() {
		t.Fatalf("clientToModel returned errors: %v", diags)
	}
	if got := model.LocalDNSRecord.ValueString(); got != "mqtt.home.arpa" {
		t.Errorf("local_dns_record: want the real record, got %q", got)
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
			// allow_existing and skip_forget_on_destroy have no Field -- the
			// controller has never heard of either -- so nothing in the
			// Fields walk ToModel and AfterReceive run ever carries a change
			// to them forward, and an update touching only these two used to
			// report "Provider produced inconsistent result after apply"
			// even though the UniFi update had nothing to do. Spec.ApplyPlanToState
			// overlays every set-and-uncovered plan value back onto state
			// after Update, which is the generic fix for the same hole
			// network's vlan had.
			//
			// This step has to be LAST: neither attribute has a wire
			// representation, so nothing survives an import for either --
			// ImportState always starts a fresh Read from just id/site, and
			// AfterReceive's import-only defaulting makes the schema's
			// default the only value import can ever produce.
			{
				Config: testAccClientFrameworkConfig_controlFlags(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("unifi_client.test", "allow_existing", "false"),
					resource.TestCheckResourceAttr(
						"unifi_client.test",
						"skip_forget_on_destroy",
						"true",
					),
				),
			},
			// Reverts to the defaults, proving the overlay holds in both
			// directions, and so the test's own teardown actually forgets
			// this MAC: left true, skip_forget_on_destroy would make the
			// automatic destroy leave the client behind for whichever test
			// reuses this MAC against the same controller.
			{
				Config: testAccClientFrameworkConfig_basic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("unifi_client.test", "allow_existing", "true"),
					resource.TestCheckResourceAttr(
						"unifi_client.test",
						"skip_forget_on_destroy",
						"false",
					),
				),
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

func testAccClientFrameworkConfig_controlFlags() string {
	return `
resource "unifi_client" "test" {
	name                   = "tfacc-client"
	mac                    = "01:23:45:67:89:ab"
	allow_existing         = false
	skip_forget_on_destroy = true
}
`
}

// TestAccClientFramework_importByMAC is the regression test for the parity
// gap the kit cutover opened: the earlier hand-written resource imported a
// client by mac alone, both via the CLI's bare
// `terraform import unifi_client.x <mac>` and an identity block naming only
// mac. The kit's generic ImportState only ever understood id, so
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
			// fixed_ip = "" is the documented way to clear a previously
			// assigned address. A naive fix that only allowed the empty
			// string would still re-send the old address, because clearing
			// has to turn use_fixedip off too (see clientKitBeforeSend's
			// companion-flag derivation).
			{
				Config: testAccClientFrameworkConfig_fixedIPCleared(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("unifi_client.test", "fixed_ip", ""),
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

func testAccClientFrameworkConfig_fixedIPCleared() string {
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
	fixed_ip   = ""
	network_id = unifi_network.test.id
}
`
}

// TestAccClientFramework_localDNSRecord: like fixed_ip, local_dns_record = ""
// is the documented way to clear it, and the controller keeps echoing the
// old record after local_dns_record_enabled turns off. A read that trusted
// the echo produced a perpetual "Provider produced inconsistent result
// after apply".
//
// A local DNS record names a fixed IP, so the controller requires one
// (api.err.LocalDnsRecordRequiresFixedIp otherwise); this config carries a
// fixed_ip throughout, including the clearing step, to isolate the
// local_dns_record round-trip from fixed_ip's own (see
// TestAccClientFramework_fixedIP for that one).
func TestAccClientFramework_localDNSRecord(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccClientFrameworkConfig_localDNSRecord("mqtt.home.arpa"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_client.test",
						"local_dns_record",
						"mqtt.home.arpa",
					),
					resource.TestCheckResourceAttr("unifi_client.test", "fixed_ip", "192.168.3.100"),
				),
			},
			{
				Config: testAccClientFrameworkConfig_localDNSRecord(""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("unifi_client.test", "local_dns_record", ""),
				),
			},
		},
	})
}

func testAccClientFrameworkConfig_localDNSRecord(record string) string {
	return fmt.Sprintf(`
resource "unifi_network" "dns_test" {
	name    = "TestDNS"
	subnet  = "192.168.3.1/24"
	vlan    = 3

	dhcp_server = {
		enabled = true
		start   = "192.168.3.6"
		stop    = "192.168.3.254"
	}
}

resource "unifi_client" "test" {
	name              = "tfacc-local-dns-client"
	mac               = "01:23:45:67:89:b0"
	fixed_ip          = "192.168.3.100"
	network_id        = unifi_network.dns_test.id
	local_dns_record  = %q
}
`, record)
}

func TestAccClientFramework_groups(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
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
			{
				ResourceName:            "unifi_client.test",
				ImportState:             true,
				ImportStateKind:         resource.ImportBlockWithResourceIdentity,
				ImportStateVerifyIgnore: []string{"allow_existing", "skip_forget_on_destroy"},
			},
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

			// mac must be in the identity schema: List sets identity by mac
			// rather than id, since a listed client's mac is the handle the
			// practitioner recognizes, and writing to an attribute the
			// identity schema doesn't declare is a hard "Resource Identity
			// Write Error", not a diff.
			//
			// Neither attribute is RequiredForImport: a client can be
			// imported by mac alone, no id in sight, or by id alone.
			// Marking either required would make an import block naming
			// only the other one invalid before ImportState ever runs.
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
// TestAccClientFramework_importByMAC for the API call itself). The earlier
// hand-written resource imported a client by mac two ways -- the CLI's bare
// `terraform import unifi_client.x <mac>` and an identity block naming only
// mac -- and both have to keep working alongside the id-only import every
// other kit surface gets.
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

// TestClientImportHandleRejectsAnEmptyHandle guards a hole that
// OptionalForImport opened on both id and mac (necessary so either alone
// satisfies the schema): a client-specific ImportState that silently
// accepted an empty handle would send id="" through to Read, which hits
// GetClient(site, "") -- the LIST endpoint, which go-unifi answers with the
// site's one client whenever there is exactly one. That's a silent, wrong
// import with no diagnostic, which is why this asserts on Diagnostics
// rather than a "not found" message.
func TestClientImportHandleRejectsAnEmptyHandle(t *testing.T) {
	for _, testCase := range []struct {
		name string
		req  func(t *testing.T) fwresource.ImportStateRequest
	}{
		{
			name: "empty CLI import string",
			req: func(*testing.T) fwresource.ImportStateRequest {
				return fwresource.ImportStateRequest{ID: ""}
			},
		},
		{
			name: "empty identity block, neither id nor mac set",
			req: func(t *testing.T) fwresource.ImportStateRequest {
				identity := clientTestIdentity(t)
				return fwresource.ImportStateRequest{Identity: &identity}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			handle, isMAC, diags := clientImportHandle(t.Context(), testCase.req(t))
			if !diags.HasError() {
				t.Fatalf("clientImportHandle: want an error for an empty handle, "+
					"got handle=%q isMAC=%v and no diagnostics", handle, isMAC)
			}
		})
	}
}

// TestClientImportHandleRejectsSiteMAC: a "site:mac" handle is not
// supported (mac import always uses the provider's own site, never a
// per-import override), but falling through to the kit's generic "Import ID
// must be in format 'site:id' or 'id'" says nothing about mac. This asserts
// the client-specific error names mac explicitly instead.
func TestClientImportHandleRejectsSiteMAC(t *testing.T) {
	_, _, diags := clientImportHandle(t.Context(), fwresource.ImportStateRequest{
		ID: "default:01:23:45:67:89:ab",
	})
	if !diags.HasError() {
		t.Fatal(`clientImportHandle: want an error for "site:mac", got none`)
	}
	found := false
	for _, d := range diags.Errors() {
		if strings.Contains(d.Detail(), "mac") {
			found = true
		}
	}
	if !found {
		t.Errorf(`clientImportHandle: want an error mentioning "mac", got: %v`, diags.Errors())
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
			if tt.args.resp.Diagnostics.HasError() {
				t.Fatalf("Schema() diagnostics: %v", tt.args.resp.Diagnostics.Errors())
			}
			if len(tt.args.resp.Schema.Attributes) == 0 {
				t.Fatal("Schema() returned no attributes")
			}
		})
	}
}

// TestClientBeforeSendDerivesTheCompanionFlags asserts BeforeSend's
// derivation of use_fixedip from whether fixed_ip is set, and
// local_dns_record_enabled likewise -- the controller ignores the value
// without the flag. The clearing direction matters most: an emptied
// fixed_ip has to turn the flag off, or the controller keeps applying the
// old address.
func TestClientBeforeSendDerivesTheCompanionFlags(t *testing.T) {
	for _, testCase := range []struct {
		name         string
		fixedIP      types.String
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
			fixedIP:    types.StringValue("192.168.1.100"),
			fixedApMAC: hwtypes.NewMACAddressNull(),
			dnsRecord:  types.StringNull(),
			networkID:  types.StringNull(),
			wantFixed:  true,
		},
		{
			name:       "an emptied fixed_ip turns the flag off",
			fixedIP:    types.StringValue(""),
			fixedApMAC: hwtypes.NewMACAddressNull(),
			dnsRecord:  types.StringNull(),
			networkID:  types.StringNull(),
			wantFixed:  false,
		},
		{
			name:       "a null fixed_ip turns the flag off",
			fixedIP:    types.StringNull(),
			fixedApMAC: hwtypes.NewMACAddressNull(),
			dnsRecord:  types.StringNull(),
			networkID:  types.StringNull(),
			wantFixed:  false,
		},
		{
			name:        "local_dns_record carries its own flag",
			fixedIP:     types.StringNull(),
			fixedApMAC:  hwtypes.NewMACAddressNull(),
			dnsRecord:   types.StringValue("host.example"),
			networkID:   types.StringNull(),
			wantDNSFlag: true,
		},
		{
			name:         "a set network_id turns the override flag on",
			fixedIP:      types.StringNull(),
			fixedApMAC:   hwtypes.NewMACAddressNull(),
			dnsRecord:    types.StringNull(),
			networkID:    types.StringValue("6a8b3cd94c934471f6b6ff20"),
			wantOverride: true,
		},
		{
			name:         "a null network_id turns the override flag off, not absent",
			fixedIP:      types.StringNull(),
			fixedApMAC:   hwtypes.NewMACAddressNull(),
			dnsRecord:    types.StringNull(),
			networkID:    types.StringNull(),
			wantOverride: false,
		},
		{
			name:        "a set fixed_ap_mac turns its own flag on",
			fixedIP:     types.StringNull(),
			fixedApMAC:  hwtypes.NewMACAddressValue("02:00:00:de:ad:05"),
			dnsRecord:   types.StringNull(),
			networkID:   types.StringNull(),
			wantFixedAp: true,
		},
		{
			name:        "a null fixed_ap_mac turns its own flag off",
			fixedIP:     types.StringNull(),
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
			// A nil api and mutex are safe here: with qos_rate and groups both
			// null there is nothing for BeforeSend to look up or create.
			hook := clientKitBeforeSend(nil, "default", &sync.Mutex{})
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
			// Never nil: virtual_network_override_enabled is in AlwaysWire, so
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

// TestClientBeforeSendSerializesNetworkMembersGroupCreateOnMiss guards a
// race: N concurrent unifi_client creates sharing one groups = [...] name
// each start from their own prefetch snapshot with that name absent (no
// cache across RPCs), so without groupMu serializing the create-on-miss
// path, each of the N would create its own network-members group of the
// same name -- with it, only the first does, and the rest discover it on
// their locked re-list.
//
// The concurrency is simulated deterministically: each goroutine gets its
// own empty *clientGroups rather than sharing one, standing in for "the
// framework builds a fresh clientResource per RPC" without racing real
// goroutines.
func TestClientBeforeSendSerializesNetworkMembersGroupCreateOnMiss(t *testing.T) {
	const groupName = "tfacc-shared-group"
	const workers = 8

	var (
		mu      sync.Mutex
		exist   []unifi.NetworkMembersGroup
		creates int
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/proxy/network/status" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"meta":{"server_version":"10.4.57"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/network-members-groups"):
			// The re-list, taken under groupMu, is what lets the second
			// worker see the first worker's create -- so it has to answer
			// from the same shared slice every create appends to, not a
			// fixed fixture.
			mu.Lock()
			snapshot := append([]unifi.NetworkMembersGroup(nil), exist...)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(snapshot)
		case req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/network-members-group"):
			var in unifi.NetworkMembersGroup
			if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			mu.Lock()
			creates++
			in.ID = fmt.Sprintf("grp-%d", creates)
			exist = append(exist, in)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(in)
		default:
			t.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	api, err := unifi.New(context.Background(), &unifi.Config{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("create the API client: %v", err)
	}

	groupsValue, diags := types.ListValueFrom(context.Background(), types.StringType, []string{groupName})
	if diags.HasError() {
		t.Fatalf("building groups list: %v", diags)
	}

	var groupMu sync.Mutex
	hook := clientKitBeforeSend(api, "default", &groupMu)

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			model := clientModel{
				FixedIP:        types.StringNull(),
				FixedApMAC:     hwtypes.NewMACAddressNull(),
				LocalDNSRecord: types.StringNull(),
				NetworkID:      types.StringNull(),
				QOSRate:        types.ObjectNull(qosRateModel{}.AttributeTypes()),
				Groups:         groupsValue,
			}
			sdk := &unifi.Client{}
			prefetched := &clientGroups{memberIDByName: map[string]string{}}
			if d := hook(context.Background(), &model, &model, sdk, prefetched); d.HasError() {
				errs <- fmt.Errorf("BeforeSend: %v", d)
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	if creates != 1 {
		t.Errorf("network members group %q: want exactly 1 create across %d concurrent "+
			"unifi_client writes sharing the name, got %d", groupName, workers, creates)
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
			if tt.args.resp.Diagnostics.HasError() {
				t.Fatalf("ListResourceConfigSchema() diagnostics: %v", tt.args.resp.Diagnostics.Errors())
			}
			if len(tt.args.resp.Schema.Attributes) == 0 {
				t.Fatal("ListResourceConfigSchema() returned no attributes")
			}
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
		case basetypes.SetType:
			vals[name] = types.SetNull(tt.ElemType)
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

// TestFrameworkToPortOverrides_AggregateOpMode: to form an SFP+ link
// aggregation the port's op_mode must be written as "aggregate" alongside
// the aggregate_members. op_mode is otherwise skipped (default "switch") so
// gateway devices that reject op_mode on PUT keep working.
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

// TestFrameworkToPortOverrides_SwitchOpModeOmitted ensures the default
// "switch" op_mode is not sent on the wire (it has omitempty), preserving
// the gateway write fix.
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

// clientToModel is a shim: the original method is gone (ToModel now fills
// the Fields and AfterReceive derives qos_rate and groups), and this
// preserves it so the read-path tests above keep asserting exactly what
// they asserted before the surface moved onto the kit.
//
// nil prefetched is the point of these two cases rather than an omission:
// this client has no usergroup and no member groups, so there is nothing
// to look up and the typed nulls are what the assertions are about.
func (r *clientKitResource) clientToModel(
	ctx context.Context,
	client *unifi.Client,
	model *clientModel,
) diag.Diagnostics {
	diags := r.Spec.ToModel(ctx, client, model, "default")
	diags.Append(r.Spec.AfterReceive(ctx, client, model, clientModel{}, nil)...)
	return diags
}
