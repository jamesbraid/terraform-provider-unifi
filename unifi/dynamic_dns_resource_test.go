package unifi

import (
	"context"
	"reflect"
	"testing"

	fwlist "github.com/hashicorp/terraform-plugin-framework/list"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

func TestAccDynamicDNS_dyndns(t *testing.T) {
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccDynamicDNSConfig,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("unifi_dynamic_dns.test", "service", "dyndns"),
					resource.TestCheckResourceAttr(
						"unifi_dynamic_dns.test",
						"host_name",
						"test.example.com",
					),
					resource.TestCheckResourceAttr(
						"unifi_dynamic_dns.test",
						"server",
						"dyndns.example.com",
					),
				),
			},
			{
				ResourceName:    "unifi_dynamic_dns.test",
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
			},
		},
	})
}

const testAccDynamicDNSConfig = `
resource "unifi_dynamic_dns" "test" {
	service = "dyndns"

	host_name = "test.example.com"

	server   = "dyndns.example.com"
	login    = "testuser"
	password = "password"
}
`

func TestNewDynamicDNSResource(t *testing.T) {
	r := NewDynamicDNSResource()
	if r == nil {
		t.Fatal("returned nil")
	}
	if _, ok := r.(fwresource.ResourceWithConfigure); !ok {
		t.Error("expected ResourceWithConfigure")
	}
	if _, ok := r.(fwresource.ResourceWithImportState); !ok {
		t.Error("expected ResourceWithImportState")
	}
	if _, ok := r.(fwresource.ResourceWithIdentity); !ok {
		t.Error("expected ResourceWithIdentity")
	}
}

func TestNewDynamicDNSListResource(t *testing.T) {
	r := NewDynamicDNSListResource()
	if r == nil {
		t.Fatal("returned nil")
	}
	if _, ok := r.(fwlist.ListResourceWithConfigure); !ok {
		t.Error("expected ListResourceWithConfigure")
	}
}

func Test_dynamicDNSResource_IdentitySchema(t *testing.T) {
	r := newDynamicDNSKitResource()
	resp := &fwresource.IdentitySchemaResponse{}
	r.IdentitySchema(context.Background(), fwresource.IdentitySchemaRequest{}, resp)
	if _, ok := resp.IdentitySchema.Attributes["id"]; !ok {
		t.Error("expected identity schema to have 'id' attribute")
	}
	if _, ok := resp.IdentitySchema.Attributes["site"]; !ok {
		t.Error("expected identity schema to have 'site' attribute")
	}
}

// TestDynamicDNSDescriptorRoundTripsEveryField pins what the hand-written
// mapper did in both directions: every set attribute reaches the SDK struct,
// and a full SDK object reads back attribute for attribute.
func TestDynamicDNSDescriptorRoundTripsEveryField(t *testing.T) {
	ctx := context.Background()
	spec := dynamicDNSKitSpec()

	model := dynamicDNSKitModel{
		ID:        types.StringValue("abc123"),
		HostName:  types.StringValue("test.example.com"),
		Interface: types.StringValue("wan"),
		Login:     types.StringValue("user"),
		Password:  types.StringValue("pass"),
		Server:    types.StringValue("dyndns.example.com"),
		Service:   types.StringValue("dyndns"),
	}

	var sdk unifi.DynamicDNS
	for _, field := range spec.Fields {
		if d := field.ToSDK(ctx, &model, &sdk); d.HasError() {
			t.Fatalf("ToSDK(%s): %v", field.WireName(), d)
		}
	}
	want := unifi.DynamicDNS{
		HostName:  "test.example.com",
		Interface: "wan",
		Login:     "user",
		Password:  "pass",
		Server:    "dyndns.example.com",
		Service:   "dyndns",
	}
	if !reflect.DeepEqual(sdk, want) {
		t.Errorf("ToSDK produced %+v, want %+v", sdk, want)
	}

	var back dynamicDNSKitModel
	for _, field := range spec.Fields {
		if d := field.ToModel(ctx, &sdk, &back); d.HasError() {
			t.Fatalf("ToModel(%s): %v", field.WireName(), d)
		}
	}
	for name, pair := range map[string][2]any{
		"host_name": {back.HostName, model.HostName},
		"interface": {back.Interface, model.Interface},
		"login":     {back.Login, model.Login},
		"password":  {back.Password, model.Password},
		"server":    {back.Server, model.Server},
		"service":   {back.Service, model.Service},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s round trip: %v want %v", name, pair[0], pair[1])
		}
	}
}

// TestDynamicDNSOptionalFieldsStayAbsent pins the hand mapper's null
// handling on both paths: a null optional attribute is not written to the
// SDK struct, and an empty SDK value reads back null rather than "".
func TestDynamicDNSOptionalFieldsStayAbsent(t *testing.T) {
	ctx := context.Background()
	spec := dynamicDNSKitSpec()

	model := dynamicDNSKitModel{
		HostName:  types.StringValue("test.example.com"),
		Interface: types.StringValue("wan"),
		Service:   types.StringValue("dyndns"),
		Login:     types.StringNull(),
		Password:  types.StringNull(),
		Server:    types.StringNull(),
	}
	var sdk unifi.DynamicDNS
	for _, field := range spec.Fields {
		if d := field.ToSDK(ctx, &model, &sdk); d.HasError() {
			t.Fatalf("ToSDK(%s): %v", field.WireName(), d)
		}
	}
	if sdk.Login != "" || sdk.Password != "" || sdk.Server != "" {
		t.Errorf("null optional fields reached the SDK struct: %+v", sdk)
	}

	var back dynamicDNSKitModel
	for _, field := range spec.Fields {
		if d := field.ToModel(ctx, &sdk, &back); d.HasError() {
			t.Fatalf("ToModel(%s): %v", field.WireName(), d)
		}
	}
	if !back.Login.IsNull() || !back.Password.IsNull() || !back.Server.IsNull() {
		t.Errorf("empty optional fields did not read back null: %+v", back)
	}
}

// TestDynamicDNSDescriptorCoversEveryManagedField stops the round trips
// above passing because a field is absent from the descriptor entirely.
func TestDynamicDNSDescriptorCoversEveryManagedField(t *testing.T) {
	got := map[string]bool{}
	for _, f := range dynamicDNSKitSpec().Fields {
		got[f.WireName()] = true
	}
	for _, want := range []string{
		"host_name", "interface", "login", "server", "service", "x_password",
	} {
		if !got[want] {
			t.Errorf("the descriptor does not carry managed field %q", want)
		}
	}
	if len(got) != 6 {
		t.Errorf("descriptor carries %d fields, want 6: %v", len(got), got)
	}
}

func Test_dynamicDNSResource_ListResourceConfigSchema(t *testing.T) {
	r := newDynamicDNSKitResource()
	resp := &fwlist.ListResourceSchemaResponse{}
	r.ListResourceConfigSchema(context.Background(), fwlist.ListResourceSchemaRequest{}, resp)
	if len(resp.Schema.Attributes) == 0 {
		t.Error("expected non-empty list resource schema")
	}
}

func TestAccDynamicDNSList_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_14_0),
		},
		Steps: []resource.TestStep{
			{
				Config: testAccDynamicDNSConfig,
			},
			{
				Query: true,
				Config: `
					provider "unifi" {}
					list "unifi_dynamic_dns" "test" {
						provider = unifi
						config {
							filter {
								name  = "host_name"
								value = "test.example.com"
							}
						}
					}
				`,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("unifi_dynamic_dns.test", 1),
				},
			},
		},
	})
}
