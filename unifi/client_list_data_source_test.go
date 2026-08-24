package unifi

import (
	"testing"

	fwdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	gounifi "github.com/ubiquiti-community/go-unifi/unifi"
)

func TestAccClientListDataSource_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccClientListDataSourceConfig_basic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(
						"data.unifi_client_list.test",
						"clients.#",
					),
					resource.TestCheckResourceAttr(
						"data.unifi_client_list.test",
						"site",
						"default",
					),
				),
			},
		},
	})
}

func TestAccClientListDataSource_filtered(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccClientListDataSourceConfig_wired(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(
						"data.unifi_client_list.wired",
						"clients.#",
					),
					resource.TestCheckResourceAttr(
						"data.unifi_client_list.wired",
						"site",
						"default",
					),
				),
			},
			{
				Config: testAccClientListDataSourceConfig_blocked(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(
						"data.unifi_client_list.blocked",
						"clients.#",
					),
				),
			},
		},
	})
}

func testAccClientListDataSourceConfig_basic() string {
	return `
data "unifi_client_list" "test" {
}
`
}

func testAccClientListDataSourceConfig_wired() string {
	return `
data "unifi_client_list" "wired" {
  wired = true
}
`
}

func testAccClientListDataSourceConfig_blocked() string {
	return `
data "unifi_client_list" "blocked" {
  blocked = false
}
`
}

func TestNewClientListDataSource(t *testing.T) {
	d := NewClientListDataSource()
	if d == nil {
		t.Fatal("NewClientListDataSource() returned nil")
	}
	if _, ok := d.(fwdatasource.DataSourceWithConfigure); !ok {
		t.Error("expected DataSourceWithConfigure interface")
	}
}

func Test_clientListEntryAttrTypes(t *testing.T) {
	got := clientListEntryAttrTypes()
	for _, key := range []string{"id", "mac", "name", "ip", "blocked", "is_wired", "status", "uptime", "first_seen", "last_seen"} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing key %q", key)
		}
	}
	if got["id"] != types.StringType {
		t.Errorf("id type = %T, want StringType", got["id"])
	}
	if got["blocked"] != types.BoolType {
		t.Errorf("blocked type = %T, want BoolType", got["blocked"])
	}
	if got["uptime"] != types.Int64Type {
		t.Errorf("uptime type = %T, want Int64Type", got["uptime"])
	}
}

func Test_clientListEntryValues(t *testing.T) {
	c := &gounifi.Client{
		ID:  "abc123",
		MAC: "aa:bb:cc:dd:ee:ff",
	}
	got := clientListEntryValues(c, nil)
	if got["id"] != types.StringValue("abc123") {
		t.Errorf("id = %v, want %q", got["id"], "abc123")
	}
	if got["mac"] != types.StringValue("aa:bb:cc:dd:ee:ff") {
		t.Errorf("mac = %v, want %q", got["mac"], "aa:bb:cc:dd:ee:ff")
	}
}

func Test_networkIDValue(t *testing.T) {
	t.Run("VirtualNetworkOverrideID wins", func(t *testing.T) {
		c := &gounifi.Client{NetworkID: "net1", VirtualNetworkOverrideID: "vnet1"}
		got := networkIDValue(c)
		if got.ValueString() != "vnet1" {
			t.Errorf("got %q, want %q", got.ValueString(), "vnet1")
		}
	})
	t.Run("falls back to NetworkID", func(t *testing.T) {
		c := &gounifi.Client{NetworkID: "net1"}
		got := networkIDValue(c)
		if got.ValueString() != "net1" {
			t.Errorf("got %q, want %q", got.ValueString(), "net1")
		}
	})
}

func Test_stringSliceToList(t *testing.T) {
	t.Run("nil slice returns null list", func(t *testing.T) {
		got := stringSliceToList(nil)
		if !got.IsNull() {
			t.Error("expected null list for nil slice")
		}
	})
	t.Run("empty slice returns null list", func(t *testing.T) {
		got := stringSliceToList([]string{})
		if !got.IsNull() {
			t.Error("expected null list for empty slice")
		}
	})
	t.Run("populated slice returns list value", func(t *testing.T) {
		got := stringSliceToList([]string{"a", "b"})
		if got.IsNull() || got.IsUnknown() {
			t.Error("expected non-null list")
		}
		if len(got.Elements()) != 2 {
			t.Errorf("len = %d, want 2", len(got.Elements()))
		}
	})
}

func Test_int64PointerValueOrNull(t *testing.T) {
	t.Run("nil returns null", func(t *testing.T) {
		got := int64PointerValueOrNull(nil)
		if !got.IsNull() {
			t.Error("expected null Int64 for nil pointer")
		}
	})
	t.Run("non-nil returns value", func(t *testing.T) {
		v := int64(42)
		got := int64PointerValueOrNull(&v)
		if got.ValueInt64() != 42 {
			t.Errorf("got %d, want 42", got.ValueInt64())
		}
	})
}
