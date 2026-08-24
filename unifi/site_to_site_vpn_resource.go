package unifi

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_site_to_site_vpn "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_site_to_site_vpn"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

type siteToSiteVPNKitResource struct {
	resourcekit.Resource[siteToSiteVPNKitModel, ui.Network]
}

var (
	_ resource.Resource                 = &siteToSiteVPNKitResource{}
	_ resource.ResourceWithImportState  = &siteToSiteVPNKitResource{}
	_ resource.ResourceWithIdentity     = &siteToSiteVPNKitResource{}
	_ resource.ResourceWithUpgradeState = &siteToSiteVPNKitResource{}
	_ list.ListResource                 = &siteToSiteVPNKitResource{}
	_ list.ListResourceWithConfigure    = &siteToSiteVPNKitResource{}
)

func newSiteToSiteVPNKitResource() *siteToSiteVPNKitResource {
	r := &siteToSiteVPNKitResource{}
	r.Spec = siteToSiteVPNKitSpec()
	r.SchemaSpec = siteToSiteVPNKitSchema()
	r.ListSurface = siteToSiteVPNKitList()
	return r
}

func NewSiteToSiteVPNResource() resource.Resource { return newSiteToSiteVPNKitResource() }

func NewSiteToSiteVPNListResource() list.ListResource { return newSiteToSiteVPNKitResource() }

func (r *siteToSiteVPNKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_site_to_site_vpn.SiteToSiteVpnResourceSchema(ctx)
	resp.Schema.Version = 1
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)
	// Grafted rather than generated: the code specification has no write_only member,
	// so a generated version would drop the property that's its entire reason for existing.
	resp.Schema.Attributes["pre_shared_key_wo"] = schema.StringAttribute{
		MarkdownDescription: "Write-only equivalent of `pre_shared_key` (Terraform 1.11+). " +
			"Used at apply time but never written to state, so it can be sourced from " +
			"an ephemeral resource (e.g. a Vault secret). Mutually exclusive with " +
			"`pre_shared_key`.",
		Optional:  true,
		Sensitive: true,
		WriteOnly: true,
		Validators: []validator.String{
			stringvalidator.ConflictsWith(path.MatchRoot("pre_shared_key")),
		},
	}
}

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *siteToSiteVPNKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_site_to_site_vpn"
}

func (r *siteToSiteVPNKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = siteToSiteVPNKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}

func (r *siteToSiteVPNKitResource) UpgradeState(
	ctx context.Context,
) map[int64]resource.StateUpgrader {
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	schemaType := schemaResp.Schema.Type().TerraformType(ctx)

	return map[int64]resource.StateUpgrader{
		0: {
			StateUpgrader: func(
				ctx context.Context,
				req resource.UpgradeStateRequest,
				resp *resource.UpgradeStateResponse,
			) {
				if req.RawState == nil {
					return
				}
				dv, err := util.UpgradeDurationRawState(
					schemaType,
					req.RawState.JSON,
					func(state map[string]any) {
						util.SetDurationField(state, "ike_lifetime", time.Second)
						util.SetDurationField(state, "esp_lifetime", time.Second)
					},
				)
				if err != nil {
					resp.Diagnostics.AddError(
						"Failed to upgrade site-to-site VPN state",
						err.Error(),
					)
					return
				}
				resp.DynamicValue = dv
			},
		},
	}
}
