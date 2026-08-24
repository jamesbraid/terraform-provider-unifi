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

// resolveGroupID looks up a network-members group by name and returns its ID.
// Deliberately uncached: a cache here previously left a renamed group stale until restart.
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

	diags := req.Config.Get(ctx, &config)
	if diags.HasError() {
		stream.Results = list.ListResultsStreamDiagnostics(diags)
		return
	}

	site := config.Site.ValueString()
	if site == "" {
		site = r.DefaultSite
	}

	// One fetch for the whole stream: AfterReceive needs the site's group
	// vocabularies to turn ids into names, rather than one call per client.
	groups, groupDiags := r.Spec.Prefetch(ctx, site)
	if groupDiags.HasError() {
		stream.Results = list.ListResultsStreamDiagnostics(groupDiags)
		return
	}

	// apiFilters go straight to ListClientFiltered; postFilters are evaluated in
	// memory after the API responds, each mapping a field to an OR-set of accepted values.
	apiFilters := make(map[string]string)
	postFilters := make(map[string]map[string]struct{})

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

	stream.Results = func(push func(list.ListResult) bool) {
		for _, client := range clients {
			info := infoByUserID[client.ID]

			if groupIDFilter != "" {
				found := slices.Contains(client.NetworkMembersGroupIDs, groupIDFilter)
				if !found {
					continue
				}
			}

			if len(nameFilter) > 0 {
				if _, ok := nameFilter[client.Name]; !ok {
					continue
				}
			}

			if len(displayNameFilter) > 0 {
				if _, ok := displayNameFilter[client.DisplayName]; !ok {
					continue
				}
			}

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

			if len(networkNameFilter) > 0 {
				netName := ""
				if info != nil {
					netName = info.NetworkName
				}
				if _, ok := networkNameFilter[netName]; !ok {
					continue
				}
			}

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

			// Sets both id and mac: a query check treats id's absence as a
			// mismatch, not a don't-care, so this identity must carry both, matching every other operation on this surface.
			result.Diagnostics.Append(
				result.Identity.SetAttribute(ctx, path.Root("id"), types.StringValue(client.ID))...)
			result.Diagnostics.Append(
				result.Identity.SetAttribute(
					ctx,
					path.Root("mac"),
					hwtypes.NewMACAddressValue(client.MAC),
				)...)

			// Uses the same ToModel/AfterReceive path the kit's refresh uses,
			// rather than a separate mapper, so a list row and a refreshed resource are guaranteed to agree.
			var model clientModel
			result.Diagnostics.Append(r.Spec.ToModel(ctx, &client, &model, site)...)
			result.Diagnostics.Append(
				r.Spec.AfterReceive(ctx, &client, &model, clientModel{}, groups)...)
			if !result.Diagnostics.HasError() {
				model.Timeouts = timeoutsNullValue()
				result.Diagnostics.Append(result.Resource.Set(ctx, model)...)
			}

			if !push(result) {
				return
			}
		}
	}
}
