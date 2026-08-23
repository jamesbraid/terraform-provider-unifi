package unifi

import (
	"context"
	"fmt"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

const (
	defaultSkipForgetOnDestroy = false
	defaultAllowExisting       = true
)

// qosRateModel describes the nested qos_rate attribute.
type qosRateModel struct {
	ID      types.String `tfsdk:"id"`
	Name    types.String `tfsdk:"name"`
	MaxUp   types.Int64  `tfsdk:"max_up"`
	MaxDown types.Int64  `tfsdk:"max_down"`
}

func (m qosRateModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"id":       types.StringType,
		"name":     types.StringType,
		"max_up":   types.Int64Type,
		"max_down": types.Int64Type,
	}
}

// clientListConfigModel describes the list configuration model.
type clientListConfigModel struct {
	Site   types.String `tfsdk:"site"`
	Group  types.String `tfsdk:"group"`
	Filter types.List   `tfsdk:"filter"`
}

// clientListFilterModel represents a single name/values filter entry.
type clientListFilterModel struct {
	Name  types.String `tfsdk:"name"`
	Value types.String `tfsdk:"value"`
}

// resolveGroupID looks up a network members group by name and returns its ID.
// Results are cached per site to avoid repeated API calls.
// resolveGroupID turns a network-members group name into its id, for the group
// filter on the list surface.
//
// THE PROCESS-LIFETIME CACHE IS GONE. It was a mutex-guarded map on the
// resource struct, populated on first use and never invalidated, so a group
// renamed on the controller stayed wrong until the provider restarted. One list
// per List call is the same call the hand-written version made on a cold cache,
// and it cannot go stale.
func (r *clientKitResource) resolveGroupID(
	ctx context.Context,
	site, groupName string,
) (string, error) {
	groups, err := r.api.ListNetworkMembersGroups(ctx, site)
	if err != nil {
		return "", fmt.Errorf("listing network members groups: %w", err)
	}
	for _, g := range groups {
		if g.Name == groupName {
			return g.ID, nil
		}
	}
	return "", fmt.Errorf("no network members group named %q on site %s", groupName, site)
}

func (r *clientKitResource) List(
	ctx context.Context,
	req list.ListRequest,
	stream *list.ListResultsStream,
) {
	var config clientListConfigModel

	// Read list config data into the model.
	diags := req.Config.Get(ctx, &config)
	if diags.HasError() {
		stream.Results = list.ListResultsStreamDiagnostics(diags)
		return
	}

	site := config.Site.ValueString()
	if site == "" {
		site = r.DefaultSite
	}

	// ONE FETCH FOR THE WHOLE STREAM. AfterReceive needs the site's group
	// vocabularies to turn ids into names, and the hand-written read issued a
	// GetClientGroup per client to do it.
	groups, groupDiags := r.Spec.Prefetch(ctx, site)
	if groupDiags.HasError() {
		stream.Results = list.ListResultsStreamDiagnostics(groupDiags)
		return
	}

	// apiFilters are passed directly to ListClientFiltered.
	// postFilters require in-memory evaluation after the API responds.
	// Each postFilter entry maps a field name to an OR-set of accepted values.
	apiFilters := make(map[string]string)
	postFilters := make(map[string]map[string]struct{})

	// Resolve the group attribute to an ID for post-filtering.
	var groupIDFilter string
	if !config.Group.IsNull() && !config.Group.IsUnknown() {
		groupID, err := r.resolveGroupID(ctx, site, config.Group.ValueString())
		if err != nil {
			var d diag.Diagnostics
			d.AddError("Error Resolving Group", err.Error())
			stream.Results = list.ListResultsStreamDiagnostics(d)
			return
		}
		groupIDFilter = groupID
	}

	// Process generic filter blocks.
	// API-passthrough names: oui, blocked, is_wired (first value used).
	// Post-filter names: network_id, network_name, name, display_name, fixed_ip (OR across values).

	filters := []clientListFilterModel{}
	config.Filter.ElementsAs(ctx, &filters, false)

	for _, f := range filters {
		name := f.Name.ValueString()
		value := f.Value.ValueString()

		switch name {
		case "network_id", "network_name", "name", "display_name", "fixed_ip":
			set := make(map[string]struct{}, 1)
			set[value] = struct{}{}
			postFilters[name] = set
		default:
			// Pass first value to the API; the API does not support OR within a field.
			apiFilters[name] = value
		}
	}

	// Fetch clients — use filtered endpoint only when API filters are present.
	var clients []unifi.Client
	var err error
	if len(apiFilters) > 0 {
		clients, err = r.api.ListClientFiltered(ctx, site, apiFilters)
	} else {
		clients, err = r.api.ListClient(ctx, site)
	}
	if err != nil {
		var d diag.Diagnostics
		d.AddError("Error Listing Clients", "Could not list clients: "+err.Error())
		stream.Results = list.ListResultsStreamDiagnostics(d)
		return
	}

	// Fetch active client info for display-name enrichment and network_name post-filtering.
	// Failures are non-fatal — enrichment is skipped if unavailable.
	infoByUserID := make(map[string]*unifi.ClientInfo)
	if activeClients, infoErr := r.api.ListClientInfo(ctx, site); infoErr == nil {
		for i := range activeClients {
			ci := &activeClients[i]
			if ci.UserId != "" {
				infoByUserID[ci.UserId] = ci
			}
		}
	}

	networkIDFilter := postFilters["network_id"]
	networkNameFilter := postFilters["network_name"]
	nameFilter := postFilters["name"]
	displayNameFilter := postFilters["display_name"]
	fixedIPFilter := postFilters["fixed_ip"]

	// Define the function that will push results into the stream.
	stream.Results = func(push func(list.ListResult) bool) {
		for _, client := range clients {
			info := infoByUserID[client.ID]

			// Post-filter by group ID: check if the resolved group ID is in the client's group list.
			if groupIDFilter != "" {
				found := slices.Contains(client.NetworkMembersGroupIDs, groupIDFilter)
				if !found {
					continue
				}
			}

			// Post-filter by name.
			if len(nameFilter) > 0 {
				if _, ok := nameFilter[client.Name]; !ok {
					continue
				}
			}

			// Post-filter by display_name.
			if len(displayNameFilter) > 0 {
				if _, ok := displayNameFilter[client.DisplayName]; !ok {
					continue
				}
			}

			// Post-filter by fixed_ip.
			if len(fixedIPFilter) > 0 {
				if _, ok := fixedIPFilter[client.FixedIP]; !ok {
					continue
				}
			}

			// Post-filter by network_id (OR across values): match VirtualNetworkOverrideID or NetworkID.
			if len(networkIDFilter) > 0 {
				clientNetworkID := client.VirtualNetworkOverrideID
				if clientNetworkID == "" {
					clientNetworkID = client.NetworkID
				}
				if _, ok := networkIDFilter[clientNetworkID]; !ok {
					continue
				}
			}

			// Post-filter by network_name (OR across values): uses active ClientInfo data.
			if len(networkNameFilter) > 0 {
				netName := ""
				if info != nil {
					netName = info.NetworkName
				}
				if _, ok := networkNameFilter[netName]; !ok {
					continue
				}
			}

			// Initialize a new result object for each client.
			result := req.NewListResult(ctx)

			// Set display name: prefer user-assigned name, then ClientInfo hostname,
			// then the stored hostname, falling back to MAC address.
			switch {
			case client.Name != "":
				result.DisplayName = client.Name
			case info != nil && info.Hostname != "":
				result.DisplayName = info.Hostname
			case client.Hostname != "":
				result.DisplayName = client.Hostname
			default:
				result.DisplayName = client.MAC
			}

			// Set resource identity: both id and mac, matching every other
			// operation on this surface. Only mac was set here before mac
			// joined the identity schema (see IdentitySchema in
			// client_kit_resource.go); a query check comparing the whole
			// identity object treats id's absence as a mismatch, not a
			// don't-care, so a listed client's identity has to carry both or
			// neither.
			result.Diagnostics.Append(
				result.Identity.SetAttribute(ctx, path.Root("id"), types.StringValue(client.ID))...)
			result.Diagnostics.Append(
				result.Identity.SetAttribute(
					ctx,
					path.Root("mac"),
					hwtypes.NewMACAddressValue(client.MAC),
				)...)

			// THE SAME READ PATH THE KIT USES, rather than a mapper of its
			// own. ToModel fills the Fields and AfterReceive derives qos_rate
			// and groups from the vocabularies fetched once above -- so a list
			// row and a refreshed resource are built by the same code, which is
			// what the hand-written clientToModel could not promise.
			var model clientModel
			result.Diagnostics.Append(r.Spec.ToModel(ctx, &client, &model, site)...)
			result.Diagnostics.Append(
				r.Spec.AfterReceive(ctx, &client, &model, clientModel{}, groups)...)
			if !result.Diagnostics.HasError() {
				model.Timeouts = timeoutsNullValue()
				result.Diagnostics.Append(result.Resource.Set(ctx, model)...)
			}

			// Send the result to the stream.
			if !push(result) {
				return
			}
		}
	}
}
