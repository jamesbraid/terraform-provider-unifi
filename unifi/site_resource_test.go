package unifi

import (
	"context"
	"testing"

	fwlist "github.com/hashicorp/terraform-plugin-framework/list"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

func TestAccSiteFramework_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSiteFrameworkConfig_basic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("unifi_site.test", "description", "tfacc-test"),
					resource.TestCheckResourceAttrSet("unifi_site.test", "name"),
				),
				ResourceName:  "unifi_site.test",
				ImportState:   true,
				ImportStateId: "default",
			},
		},
	})
}

func testAccSiteFrameworkConfig_basic() string {
	return `
resource "unifi_site" "test" {
	name        = "default"
	description = "tfacc-test"
}
`
}

// TestSiteToModelNilDoesNotPanic checks that siteToModel returns an error
// diagnostic for a nil site instead of dereferencing it.
func TestSiteToModelNilDoesNotPanic(t *testing.T) {
	r := &siteFrameworkResource{}
	var model siteFrameworkResourceModel
	diags := r.siteToModel(context.Background(), nil, &model)
	if !diags.HasError() {
		t.Fatal("expected an error diagnostic for a nil site, got none")
	}
}

func TestNewSiteFrameworkResource(t *testing.T) {
	r := NewSiteFrameworkResource()
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

func TestNewSiteListResource(t *testing.T) {
	r := NewSiteListResource()
	if r == nil {
		t.Fatal("returned nil")
	}
	if _, ok := r.(fwlist.ListResourceWithConfigure); !ok {
		t.Error("expected ListResourceWithConfigure")
	}
}

func Test_siteFrameworkResource_IdentitySchema(t *testing.T) {
	type args struct {
		in0  context.Context
		in1  fwresource.IdentitySchemaRequest
		resp *fwresource.IdentitySchemaResponse
	}
	tests := []struct {
		name string
		r    *siteFrameworkResource
		args args
	}{
		{
			name: "has_id",
			r:    &siteFrameworkResource{},
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
			if _, ok := tt.args.resp.IdentitySchema.Attributes["id"]; !ok {
				t.Error("expected identity schema to have 'id' attribute")
			}
		})
	}
}

func Test_siteFrameworkResource_applyPlanToState(t *testing.T) {
	type args struct {
		in0   context.Context
		plan  *siteFrameworkResourceModel
		state *siteFrameworkResourceModel
	}
	tests := []struct {
		name string
		r    *siteFrameworkResource
		args args
	}{
		{
			name: "copies_description",
			r:    &siteFrameworkResource{},
			args: args{
				in0: context.Background(),
				plan: &siteFrameworkResourceModel{
					Description: types.StringValue("new-desc"),
				},
				state: &siteFrameworkResourceModel{
					Description: types.StringValue("old-desc"),
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.applyPlanToState(tt.args.in0, tt.args.plan, tt.args.state)
			if tt.args.state.Description.ValueString() != "new-desc" {
				t.Error("expected Description to be copied from plan")
			}
		})
	}
}

func Test_siteFrameworkResource_siteToModel(t *testing.T) {
	type args struct {
		in0   context.Context
		site  *unifi.Site
		model *siteFrameworkResourceModel
	}
	tests := []struct {
		name      string
		r         *siteFrameworkResource
		args      args
		wantError bool
	}{
		{
			name: "nil_site_returns_error",
			r:    &siteFrameworkResource{},
			args: args{
				in0:   context.Background(),
				site:  nil,
				model: &siteFrameworkResourceModel{},
			},
			wantError: true,
		},
		{
			name: "empty_id_and_name_returns_error",
			r:    &siteFrameworkResource{},
			args: args{
				in0:   context.Background(),
				site:  &unifi.Site{},
				model: &siteFrameworkResourceModel{},
			},
			wantError: true,
		},
		{
			name: "valid_site",
			r:    &siteFrameworkResource{},
			args: args{
				in0: context.Background(),
				site: &unifi.Site{
					ID:          "abc123",
					Name:        "default",
					Description: "Default site",
				},
				model: &siteFrameworkResourceModel{},
			},
			wantError: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.r.siteToModel(tt.args.in0, tt.args.site, tt.args.model)
			if got.HasError() != tt.wantError {
				t.Errorf("hasError = %v, want %v, diags: %v", got.HasError(), tt.wantError, got)
			}
			if !tt.wantError && tt.args.site != nil {
				if tt.args.model.ID.ValueString() != tt.args.site.ID {
					t.Errorf("ID = %q, want %q", tt.args.model.ID.ValueString(), tt.args.site.ID)
				}
			}
		})
	}
}

func Test_siteFrameworkResource_ListResourceConfigSchema(t *testing.T) {
	type args struct {
		in0  context.Context
		in1  fwlist.ListResourceSchemaRequest
		resp *fwlist.ListResourceSchemaResponse
	}
	tests := []struct {
		name string
		r    *siteFrameworkResource
		args args
	}{
		{
			name: "returns_schema",
			r:    &siteFrameworkResource{},
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
			if len(tt.args.resp.Schema.Attributes) == 0 && len(tt.args.resp.Schema.Blocks) == 0 {
				t.Error("expected non-empty list resource schema")
			}
		})
	}
}

func TestAccSiteList_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_14_0),
		},
		Steps: []resource.TestStep{
			{
				Query: true,
				Config: `
					provider "unifi" {}
					list "unifi_site" "test" {
						provider = unifi
						config {}
					}
				`,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("unifi_site.test", 1),
				},
			},
		},
	})
}
