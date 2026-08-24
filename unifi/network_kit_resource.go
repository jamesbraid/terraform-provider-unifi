package unifi

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	listresource_network "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_network"
	resource_network "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_network"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

type networkKitResource struct {
	resourcekit.Resource[netModel, ui.Network]
}

var (
	_ resource.Resource                     = &networkKitResource{}
	_ resource.ResourceWithImportState      = &networkKitResource{}
	_ resource.ResourceWithIdentity         = &networkKitResource{}
	_ resource.ResourceWithUpgradeState     = &networkKitResource{}
	_ resource.ResourceWithConfigValidators = &networkKitResource{}
	_ resource.ResourceWithModifyPlan       = &networkKitResource{}
	// The assertion is the guard, not decoration: the framework calls
	// ValidateConfig only if the type satisfies this interface, so a mistyped
	// signature would mean the warning is simply never raised, with nothing
	// failing to say so. This makes that a compile error.
	_ resource.ResourceWithValidateConfig = &networkKitResource{}
	_ list.ListResource                   = &networkKitResource{}
	_ list.ListResourceWithConfigure      = &networkKitResource{}
)

func newNetworkKitResource() *networkKitResource {
	r := &networkKitResource{}
	r.Spec = networkKitSpec()
	r.SchemaSpec = networkKitSchema()
	r.ListSurface = networkKitList()
	return r
}

func NewNetworkResource() resource.Resource { return newNetworkKitResource() }

func NewNetworkListResource() list.ListResource { return newNetworkKitResource() }

func networkKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_network.NetworkResourceSchema,
		Version:  1,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
		// v0 -> v1: dhcp_server.leasetime, ipv6_ra_preferred_lifetime and
		// ipv6_ra_valid_lifetime changed from integer seconds to GoDuration
		// strings.
		Upgraders: func(
			ctx context.Context, built schema.Schema,
		) map[int64]resource.StateUpgrader {
			schemaType := built.Type().TerraformType(ctx)
			return map[int64]resource.StateUpgrader{
				0: {StateUpgrader: func(
					_ context.Context,
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
							util.SetDurationField(
								state, "ipv6_ra_preferred_lifetime", time.Second)
							util.SetDurationField(
								state, "ipv6_ra_valid_lifetime", time.Second)
							if dhcp, ok := state["dhcp_server"].(map[string]any); ok {
								util.SetDurationField(dhcp, "leasetime", time.Second)
							}
						},
					)
					if err != nil {
						resp.Diagnostics.AddError(
							"Failed to upgrade network state", err.Error())
						return
					}
					resp.DynamicValue = dv
				}},
			}
		},
	}
}

func networkKitList() resourcekit.ListSpec[ui.Network] {
	return resourcekit.ListSpec[ui.Network]{
		ConfigSchema: listresource_network.NetworkListResourceSchema,
		DisplayName: func(s *ui.Network) string {
			if s.Name != nil && *s.Name != "" {
				return *s.Name
			}
			return s.ID
		},
		Filters: map[string]func(*ui.Network) string{
			"name": func(s *ui.Network) string {
				if s.Name == nil {
					return ""
				}
				return *s.Name
			},
			"purpose": func(s *ui.Network) string { return s.Purpose },
		},
	}
}

func (r *networkKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_network.NetworkResourceSchema(ctx)
	// The specification cannot carry a schema version, so it is re-set here as
	// site_to_site_vpn does.
	resp.Schema.Version = 1
	// Grafted rather than generated, as everywhere else: timeouts.Attributes is
	// a call, not a literal, so the code specification cannot carry it.
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)
}

func (r *networkKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_network"
}

func (r *networkKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = networkKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}
