package unifi

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	fwlist "github.com/hashicorp/terraform-plugin-framework/list"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/acctestenv"
)

// testAccAPGroupCheckDestroy verifies that every unifi_ap_group in state has
// been removed from the controller. It is a best-effort check that no-ops when
// no live controller is configured.
func testAccAPGroupCheckDestroy(s *terraform.State) error {
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
		if rs.Type != "unifi_ap_group" {
			continue
		}
		site := rs.Primary.Attributes["site"]
		if site == "" {
			site = c.Site
		}
		_, err := c.GetAPGroup(ctx, site, rs.Primary.ID)
		if err == nil {
			return fmt.Errorf("unifi_ap_group %s still exists", rs.Primary.ID)
		}
		if _, ok := err.(*unifi.NotFoundError); !ok {
			return err
		}
	}
	return nil
}

// TestAccAPGroupFramework_basic exercises the full CRUD + import lifecycle of an
// AP group with EMPTY membership. Empty groups are portable: a freshly-booted
// controller has no adopted access points, so any real device MAC would be
// rejected with api.err.InvalidDeviceInApGroup. An empty group has no such
// dependency, which lets create → read → update → import → delete run green
// against any controller. The read/refresh path is the one that previously
// returned HTTP 405 (surfacing as `invalid character '<'` when the HTML error
// page was parsed as JSON); driving it here proves the fix.
func TestAccAPGroupFramework_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccAPGroupCheckDestroy,
		Steps: []resource.TestStep{
			// Create an empty group.
			{
				Config: testAccAPGroupFrameworkConfig_basic("tf-acc-apgroup"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_ap_group.test", "id"),
					resource.TestCheckResourceAttr(
						"unifi_ap_group.test",
						"name",
						"tf-acc-apgroup",
					),
					resource.TestCheckResourceAttr("unifi_ap_group.test", "device_macs.#", "0"),
				),
			},
			// Update the name in place; membership stays empty.
			{
				Config: testAccAPGroupFrameworkConfig_basic("tf-acc-apgroup-2"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_ap_group.test", "id"),
					resource.TestCheckResourceAttr(
						"unifi_ap_group.test",
						"name",
						"tf-acc-apgroup-2",
					),
					resource.TestCheckResourceAttr("unifi_ap_group.test", "device_macs.#", "0"),
				),
			},
			// Import the group and verify the imported state matches.
			{
				ResourceName:      "unifi_ap_group.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// TestAccAPGroupFramework_withDevices asserts MAC semantic-equality: a member
// written in upper/dash form must not produce a spurious plan against the same
// address stored lowercase/colon form on the controller. It is skipped unless
// UNIFI_ACC_AP_MAC names a real adopted access point, since the controller
// rejects membership of any device it has not adopted.
func TestAccAPGroupFramework_withDevices(t *testing.T) {
	mac := os.Getenv(acctestenv.EnvAccAPMAC)
	if mac == "" {
		t.Skipf("%s not set; skipping adopted-device AP group test", acctestenv.EnvAccAPMAC)
	}
	upperDashMac := strings.ToUpper(strings.ReplaceAll(mac, ":", "-"))
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccAPGroupCheckDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccAPGroupFrameworkConfig_withDevice("tf-acc-apgroup-dev", mac),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_ap_group.test", "id"),
					resource.TestCheckResourceAttr("unifi_ap_group.test", "device_macs.#", "1"),
				),
			},
			// The same MAC in UPPER/dash form must be semantically equal, so the
			// plan is empty (no diff) even though the literal string differs.
			{
				Config: testAccAPGroupFrameworkConfig_withDevice(
					"tf-acc-apgroup-dev",
					upperDashMac,
				),
				// PlanOnly with no expected changes: the upper/dash MAC must
				// compare equal to the stored lowercase/colon form (semantic
				// equality), so the plan is empty.
				PlanOnly: true,
			},
		},
	})
}

func TestAccAPGroupList_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_14_0),
		},
		Steps: []resource.TestStep{
			{Config: testAccAPGroupFrameworkConfig_basic("tf-acc-apgroup-list")},
			{
				Query: true,
				Config: `
provider "unifi" {}
list "unifi_ap_group" "test" {
  provider = unifi
  config {
    filter {
      name  = "name"
      value = "tf-acc-apgroup-list"
    }
  }
}
`,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("unifi_ap_group.test", 1),
				},
			},
		},
	})
}

func testAccAPGroupFrameworkConfig_basic(name string) string {
	return fmt.Sprintf(`
resource "unifi_ap_group" "test" {
	name        = %q
	device_macs = []
}
`, name)
}

func testAccAPGroupFrameworkConfig_withDevice(name, mac string) string {
	return fmt.Sprintf(`
resource "unifi_ap_group" "test" {
	name        = %q
	device_macs = [%q]
}
`, name, mac)
}

func TestNewAPGroupResource(t *testing.T) {
	got := NewAPGroupResource()
	if got == nil {
		t.Fatal("NewAPGroupResource() returned nil")
	}
	if _, ok := got.(fwresource.ResourceWithImportState); !ok {
		t.Errorf("does not implement fwresource.ResourceWithImportState")
	}
	if _, ok := got.(fwresource.ResourceWithIdentity); !ok {
		t.Errorf("does not implement fwresource.ResourceWithIdentity")
	}
}

func TestNewAPGroupListResource(t *testing.T) {
	got := NewAPGroupListResource()
	if got == nil {
		t.Fatal("NewAPGroupListResource() returned nil")
	}
	_ = got
}

func Test_apGroupKitResource_IdentitySchema(t *testing.T) {
	r := newAPGroupKitResource()
	resp := &fwresource.IdentitySchemaResponse{}
	r.IdentitySchema(context.Background(), fwresource.IdentitySchemaRequest{}, resp)
	if _, ok := resp.IdentitySchema.Attributes["id"]; !ok {
		t.Error("IdentitySchema missing 'id' attribute")
	}
}

func Test_apGroupKitResource_ListResourceConfigSchema(t *testing.T) {
	r := newAPGroupKitResource()
	resp := &fwlist.ListResourceSchemaResponse{}
	r.ListResourceConfigSchema(context.Background(), fwlist.ListResourceSchemaRequest{}, resp)
	if _, ok := resp.Schema.Attributes["site"]; !ok {
		t.Error("ListResourceConfigSchema missing 'site' attribute")
	}
}

// macSet is the spelling a practitioner wrote, which is the thing several of
// the tests below are about preserving.
func macSet(t *testing.T, macs ...string) types.Set {
	t.Helper()
	set, diags := types.SetValueFrom(context.Background(), hwtypes.MACAddressType{}, macs)
	if diags.HasError() {
		t.Fatalf("building the MAC set: %v", diags)
	}
	return set
}

// Test_apGroupKit_writePath carries over what
// Test_apGroupResource_modelToAPIAPGroup asserted about the hand-written
// mapper. The mapper is gone; the behaviour is not, and it now lives in the
// descriptor's field list and its BeforeSend.
func Test_apGroupKit_writePath(t *testing.T) {
	ctx := context.Background()
	spec := apGroupKitSpec()

	send := func(t *testing.T, model *apGroupKitModel) *unifi.APGroup {
		t.Helper()
		sdk, diags := spec.ToSDK(ctx, model)
		if diags.HasError() {
			t.Fatalf("ToSDK: %v", diags)
		}
		if diags := spec.BeforeSend(ctx, model, model, apGroupKitModel{}, sdk, nil); diags.HasError() {
			t.Fatalf("BeforeSend: %v", diags)
		}
		return sdk
	}

	t.Run("basic group", func(t *testing.T) {
		got := send(t, &apGroupKitModel{
			Name:       types.StringValue("Test Group"),
			DeviceMacs: macSet(t, "00:11:22:33:44:55", "00:11:22:33:44:66"),
		})
		if got.Name != "Test Group" {
			t.Errorf("Name = %q, want %q", got.Name, "Test Group")
		}
		if len(got.DeviceMacs) != 2 {
			t.Errorf("DeviceMacs = %v, want 2 entries", got.DeviceMacs)
		}
	})

	// The controller stores MACs lowercased and colon-separated. Without this
	// the object it returns never matches what was sent and every apply reports
	// a change.
	t.Run("normalizes mac case and separators", func(t *testing.T) {
		got := send(t, &apGroupKitModel{
			Name:       types.StringValue("Mixed"),
			DeviceMacs: macSet(t, "AA-BB-CC-DD-EE-FF"),
		})
		if !reflect.DeepEqual(got.DeviceMacs, []string{"aa:bb:cc:dd:ee:ff"}) {
			t.Errorf("DeviceMacs = %v, want [aa:bb:cc:dd:ee:ff]", got.DeviceMacs)
		}
	})
}

// Test_apGroupKit_readPath carries over Test_apGroupResource_apGroupToModel.
func Test_apGroupKit_readPath(t *testing.T) {
	ctx := context.Background()
	spec := apGroupKitSpec()

	t.Run("basic API to model", func(t *testing.T) {
		var model apGroupKitModel
		api := &unifi.APGroup{ID: "ap1", Name: "Test", DeviceMacs: []string{"00:11:22:33:44:55"}}
		if diags := spec.ToModel(ctx, api, &model, "default"); diags.HasError() {
			t.Fatalf("ToModel: %v", diags)
		}
		if model.Name.ValueString() != "Test" {
			t.Errorf("Name = %q, want Test", model.Name.ValueString())
		}
		if n := len(model.DeviceMacs.Elements()); n != 1 {
			t.Errorf("DeviceMacs elements = %d, want 1", n)
		}
	})

	// An empty membership is a value, not an absence: the controller accepts a
	// group with no members, and device_macs is Optional AND Computed, so a
	// practitioner may have asked for exactly this. Nulling it would make state
	// disagree with a config that says `device_macs = []`.
	for _, name := range []string{"empty macs", "nil macs"} {
		t.Run(name+" produce an empty, non-null set", func(t *testing.T) {
			api := &unifi.APGroup{ID: "ap2", Name: "Empty", DeviceMacs: []string{}}
			if name == "nil macs" {
				api.DeviceMacs = nil
			}
			var model apGroupKitModel
			if diags := spec.ToModel(ctx, api, &model, "default"); diags.HasError() {
				t.Fatalf("ToModel: %v", diags)
			}
			if model.DeviceMacs.IsNull() {
				t.Fatal("DeviceMacs is null; an empty membership is a value the " +
					"practitioner may have configured")
			}
			if n := len(model.DeviceMacs.Elements()); n != 0 {
				t.Errorf("DeviceMacs elements = %d, want 0", n)
			}
		})
	}

	// A deliberate change from the hand-written mapper, which turned an
	// empty name into null: name is Required, and a Required attribute may
	// not be null in state, so Terraform would reject the result before
	// the value is ever read. The kit's own elide rule agrees: Required
	// means KeepZero.
	t.Run("empty name stays an empty string, not null", func(t *testing.T) {
		var model apGroupKitModel
		api := &unifi.APGroup{ID: "ap3", Name: "", DeviceMacs: []string{}}
		if diags := spec.ToModel(ctx, api, &model, "default"); diags.HasError() {
			t.Fatalf("ToModel: %v", diags)
		}
		if model.Name.IsNull() {
			t.Error("Name is null, but the attribute is Required")
		}
	})
}

// Test_apGroupKit_keepsThePractitionersMACSpelling covers the one behaviour the
// hand-written tests never reached.
//
// apGroupToModel called MACSetsEqual, but every case in its test started from a
// zero-valued model, and MACSetsEqual returns false for a null set -- so the
// preservation branch was never taken and the guard was effectively untested.
// It matters: a Set identifies its members by string value, so the element
// type's semantic equality never reaches the set. Overwriting "AA-BB-.." with
// the controller's "aa:bb:.." leaves a diff no apply can settle, because the
// config keeps producing the original spelling.
func Test_apGroupKit_keepsThePractitionersMACSpelling(t *testing.T) {
	ctx := context.Background()
	spec := apGroupKitSpec()

	model := apGroupKitModel{DeviceMacs: macSet(t, "AA-BB-CC-DD-EE-FF")}
	api := &unifi.APGroup{ID: "ap1", Name: "Test", DeviceMacs: []string{"aa:bb:cc:dd:ee:ff"}}

	if diags := spec.ToModel(ctx, api, &model, "default"); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}

	var got []string
	if diags := model.DeviceMacs.ElementsAs(ctx, &got, false); diags.HasError() {
		t.Fatalf("reading the set back: %v", diags)
	}
	if !reflect.DeepEqual(got, []string{"AA-BB-CC-DD-EE-FF"}) {
		t.Errorf("DeviceMacs = %v, want the configured spelling AA-BB-CC-DD-EE-FF; "+
			"rewriting it to the controller's form leaves a permanent diff", got)
	}

	// The control: a genuinely different address must still come through, or
	// this guard would be freezing state against real changes.
	changed := &unifi.APGroup{ID: "ap1", DeviceMacs: []string{"11:22:33:44:55:66"}}
	if diags := spec.ToModel(ctx, changed, &model, "default"); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	if diags := model.DeviceMacs.ElementsAs(ctx, &got, false); diags.HasError() {
		t.Fatalf("reading the set back: %v", diags)
	}
	if !reflect.DeepEqual(got, []string{"11:22:33:44:55:66"}) {
		t.Errorf("DeviceMacs = %v, want the controller's new address; the guard is "+
			"holding state against a real change", got)
	}
}

// Test_apGroupKit_neverWritesForWLANConf pins the whole-object-write fix, now
// asserted as a property of the descriptor rather than of a hand-kept list.
//
// The old fix was a mask file naming the two fields the resource owns. The kit
// derives the mask from Spec.Fields, so a field with no entry has no wire name
// to name. This is the assertion that the derivation really does exclude it --
// without it, "by construction" is a claim about code nobody checked.
func Test_apGroupKit_neverWritesForWLANConf(t *testing.T) {
	spec := apGroupKitSpec()

	for _, name := range spec.WireNames() {
		if name == "for_wlanconf" {
			t.Fatal("for_wlanconf is a declared field, so it goes on the wire and " +
				"a whole-object write would reset it on every apply")
		}
	}

	// The control: the derivation produces the names it should, so the check
	// above is not passing because WireNames() came back empty.
	want := map[string]bool{"name": true, "device_macs": true}
	got := map[string]bool{}
	for _, name := range spec.WireNames() {
		got[name] = true
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("WireNames() = %v, want %v", got, want)
	}
}
