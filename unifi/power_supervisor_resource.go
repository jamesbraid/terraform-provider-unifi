package unifi

import (
	"context"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_power_supervisor "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_power_supervisor"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type powerSupervisorKitResource struct {
	resourcekit.Resource[powerSupervisorKitModel, ui.PowerSupervisor]

	// api is here for ImportState alone: resolving a MAC import handle needs
	// GetPowerSupervisorByMAC, which the kit's Backend does not carry.
	api *ui.ApiClient
}

var (
	_ resource.Resource                 = &powerSupervisorKitResource{}
	_ resource.ResourceWithImportState  = &powerSupervisorKitResource{}
	_ resource.ResourceWithIdentity     = &powerSupervisorKitResource{}
	_ resource.ResourceWithUpgradeState = &powerSupervisorKitResource{}
	_ list.ListResource                 = &powerSupervisorKitResource{}
	_ list.ListResourceWithConfigure    = &powerSupervisorKitResource{}
)

func newPowerSupervisorKitResource() *powerSupervisorKitResource {
	r := &powerSupervisorKitResource{}
	r.Spec = powerSupervisorKitSpec()
	r.SchemaSpec = powerSupervisorKitSchema()
	r.ListSurface = powerSupervisorKitList()
	return r
}

func (r *powerSupervisorKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_power_supervisor.PowerSupervisorResourceSchema(ctx)
	// v1: the three settings durations changed from Int64 seconds to
	// GoDuration strings. See the SchemaSpec's Upgraders.
	resp.Schema.Version = 1
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true})
}

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *powerSupervisorKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_power_supervisor"
}

func NewPowerSupervisorResource() resource.Resource { return newPowerSupervisorKitResource() }

func NewPowerSupervisorListResource() list.ListResource { return newPowerSupervisorKitResource() }

func (r *powerSupervisorKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = powerSupervisorKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
	r.api = client.ApiClient
}

// powerSupervisorMACPattern is a MAC import handle. It must be detected
// before splitting on ":" (else "9c:05:..." parses as site "9c").
var powerSupervisorMACPattern = regexp.MustCompile(`^([0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}$`)

// ImportState accepts what the kit accepts ("site:id" or "id") plus the two
// MAC forms the hand resource took ("site:mac" or a bare MAC): a MAC names
// the supervised device, so it is resolved to the controller id first and
// then handed to the kit's routing.
func (r *powerSupervisorKitResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	site := r.DefaultSite
	identifier := req.ID
	sitePrefix := ""
	if !powerSupervisorMACPattern.MatchString(identifier) {
		if parts := strings.SplitN(identifier, ":", 2); len(parts) == 2 {
			site = parts[0]
			identifier = parts[1]
			sitePrefix = parts[0] + ":"
		}
	}
	if powerSupervisorMACPattern.MatchString(identifier) {
		supervisor, err := r.api.GetPowerSupervisorByMAC(ctx, site, strings.ToLower(identifier))
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Importing Power Supervisor",
				"Could not find a power supervisor for device MAC "+identifier+": "+
					resourcekit.DiagErrorText(err),
			)
			return
		}
		req.ID = sitePrefix + supervisor.ID
	}
	r.Resource.ImportState(ctx, req, resp)
}
