package unifi

import (
	"context"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/iptypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_traffic_route"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_traffic_route"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// Ensure provider defined types fully satisfy framework interfaces.
var (
	_ resource.Resource                = &trafficRouteResource{}
	_ resource.ResourceWithImportState = &trafficRouteResource{}
	_ resource.ResourceWithIdentity    = &trafficRouteResource{}
)

// Ensure provider defined types fully satisfy list interfaces.
var (
	_ list.ListResource              = &trafficRouteResource{}
	_ list.ListResourceWithConfigure = &trafficRouteResource{}
)

func NewTrafficRouteResource() resource.Resource {
	return &trafficRouteResource{}
}

func NewTrafficRouteListResource() list.ListResource {
	return &trafficRouteResource{}
}

// trafficRouteResource defines the resource implementation.
type trafficRouteResource struct {
	client *Client
}

// destinationIPModel describes a nested destination.ip entry.
type destinationIPModel struct {
	Address types.String `tfsdk:"address"`
	Ports   types.List   `tfsdk:"ports"`
}

func (m destinationIPModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"address": types.StringType,
		"ports":   types.ListType{ElemType: types.StringType},
	}
}

// sourceNetworkModel describes a nested source.networks entry.
type sourceNetworkModel struct {
	ID types.String `tfsdk:"id"`
}

func (m sourceNetworkModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"id": types.StringType,
	}
}

// sourceClientModel describes a nested source.clients entry.
type sourceClientModel struct {
	MAC types.String `tfsdk:"mac"`
}

func (m sourceClientModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"mac": types.StringType,
	}
}

// sourceModel describes the nested source attribute.
type sourceModel struct {
	Networks types.List `tfsdk:"networks"`
	Clients  types.List `tfsdk:"clients"`
}

func (m sourceModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"networks": types.ListType{
			ElemType: types.ObjectType{AttrTypes: sourceNetworkModel{}.AttributeTypes()},
		},
		"clients": types.ListType{
			ElemType: types.ObjectType{AttrTypes: sourceClientModel{}.AttributeTypes()},
		},
	}
}

// destinationModel describes the nested destination attribute.
type destinationModel struct {
	Domain types.List `tfsdk:"domain"`
	IP     types.List `tfsdk:"ip"`
	Region types.List `tfsdk:"region"`
}

func (m destinationModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"domain": types.ListType{ElemType: types.StringType},
		"ip": types.ListType{
			ElemType: types.ObjectType{AttrTypes: destinationIPModel{}.AttributeTypes()},
		},
		"region": types.ListType{ElemType: types.StringType},
	}
}

// trafficRouteResourceModel describes the resource data model.
type trafficRouteResourceModel struct {
	ID                types.String      `tfsdk:"id"`
	Site              types.String      `tfsdk:"site"`
	Description       types.String      `tfsdk:"description"`
	Destination       types.Object      `tfsdk:"destination"`
	Enabled           types.Bool        `tfsdk:"enabled"`
	KillSwitchEnabled types.Bool        `tfsdk:"kill_switch_enabled"`
	NetworkID         types.String      `tfsdk:"network_id"`
	NextHop           iptypes.IPAddress `tfsdk:"next_hop"`
	Source            types.Object      `tfsdk:"source"`
	Timeouts          timeouts.Value    `tfsdk:"timeouts"`
}

type trafficRouteIdentityModel struct {
	ID types.String `tfsdk:"id"`
}

// trafficRouteListConfigModel describes the list configuration model.
type trafficRouteListConfigModel struct {
	Site   types.String `tfsdk:"site"`
	Filter types.List   `tfsdk:"filter"`
}

// trafficRouteListFilterModel represents a single name/value filter entry.
type trafficRouteListFilterModel struct {
	Name  types.String `tfsdk:"name"`
	Value types.String `tfsdk:"value"`
}

func (r *trafficRouteResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_traffic_route"
}

// IdentitySchema implements [resource.ResourceWithIdentity].
func (r *trafficRouteResource) IdentitySchema(
	_ context.Context,
	_ resource.IdentitySchemaRequest,
	resp *resource.IdentitySchemaResponse,
) {
	resp.IdentitySchema = identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"id": identityschema.StringAttribute{
				RequiredForImport: true,
			},
		},
	}
}

func (r *trafficRouteResource) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_traffic_route.TrafficRouteResourceSchema(ctx)
	// Grafted rather than generated, as everywhere else: timeouts.Attributes
	// is a call, not a literal, so the code specification cannot carry it.
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)
}

func (r *trafficRouteResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf(
				"Expected *Client, got: %T. Please report this issue to the provider developers.",
				req.ProviderData,
			),
		)
		return
	}

	r.client = client
}

func (r *trafficRouteResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var plan trafficRouteResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, timeoutDiags := plan.Timeouts.Create(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()

	site := plan.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	body, diags := r.modelToAPI(ctx, &plan, site)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateTrafficRoute(ctx, site, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Traffic Route", err.Error())
		return
	}

	resp.Diagnostics.Append(r.apiToModel(ctx, created, &plan, site)...)
	if resp.Diagnostics.HasError() {
		return
	}

	idModel := trafficRouteIdentityModel{ID: plan.ID}
	resp.Diagnostics.Append(resp.Identity.Set(ctx, &idModel)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *trafficRouteResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var state trafficRouteResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	readTimeout, timeoutDiags := state.Timeouts.Read(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	site := state.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	id := state.ID.ValueString()
	if id == "" {
		// Try identity
		var idModel trafficRouteIdentityModel
		if d := req.Identity.Get(ctx, &idModel); !d.HasError() {
			id = idModel.ID.ValueString()
		}
	}

	if id == "" {
		resp.Diagnostics.AddError("Invalid State", "Traffic route must have an ID")
		return
	}

	route, err := r.client.GetTrafficRoute(ctx, site, id)
	if err != nil {
		if _, ok := err.(*unifi.NotFoundError); ok {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Traffic Route", err.Error())
		return
	}

	resp.Diagnostics.Append(r.apiToModel(ctx, route, &state, site)...)
	if resp.Diagnostics.HasError() {
		return
	}

	idModel := trafficRouteIdentityModel{ID: state.ID}
	resp.Diagnostics.Append(resp.Identity.Set(ctx, &idModel)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *trafficRouteResource) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var state trafficRouteResourceModel
	var plan trafficRouteResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateTimeout, timeoutDiags := plan.Timeouts.Update(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()

	site := state.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	body, diags := r.modelToAPI(ctx, &plan, site)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body.ID = state.ID.ValueString()

	updated, err := r.client.UpdateTrafficRoute(ctx, site, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Traffic Route", err.Error())
		return
	}

	resp.Diagnostics.Append(r.apiToModel(ctx, updated, &plan, site)...)
	if resp.Diagnostics.HasError() {
		return
	}

	idModel := trafficRouteIdentityModel{ID: plan.ID}
	resp.Diagnostics.Append(resp.Identity.Set(ctx, &idModel)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *trafficRouteResource) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var state trafficRouteResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	deleteTimeout, timeoutDiags := state.Timeouts.Delete(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, deleteTimeout)
	defer cancel()

	site := state.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	id := state.ID.ValueString()

	err := r.client.DeleteTrafficRoute(ctx, site, id)
	if err != nil {
		if _, ok := err.(*unifi.NotFoundError); ok {
			return
		}
		resp.Diagnostics.AddError("Error Deleting Traffic Route", err.Error())
	}
}

func (r *trafficRouteResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	idParts := strings.Split(req.ID, ":")
	if len(idParts) == 2 {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("site"), idParts[0])...)
		req.ID = idParts[1]
	}

	idModel := trafficRouteIdentityModel{ID: types.StringValue(req.ID)}
	resp.Diagnostics.Append(resp.Identity.Set(ctx, &idModel)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resource.ImportStatePassthroughWithIdentity(
		ctx,
		path.Root("id"),
		path.Root("id"),
		req,
		resp,
	)
}

// modelToAPI converts the Terraform model to the UniFi API struct.
func (r *trafficRouteResource) modelToAPI(
	ctx context.Context,
	model *trafficRouteResourceModel,
	site string,
) (*unifi.TrafficRoute, diag.Diagnostics) {
	var diags diag.Diagnostics

	networkID := model.NetworkID.ValueString()
	if networkID == "" {
		var err error
		networkID, err = r.defaultWANNetworkID(ctx, site)
		if err != nil {
			diags.AddError("Error Finding Default WAN Network", err.Error())
			return nil, diags
		}
	}

	route := &unifi.TrafficRoute{
		Description:       model.Description.ValueString(),
		Enabled:           model.Enabled.ValueBool(),
		KillSwitchEnabled: model.KillSwitchEnabled.ValueBool(),
		MatchingTarget:    "INTERNET",
		NetworkID:         networkID,
		NextHop:           model.NextHop.ValueString(),
	}

	// Destination
	hasDest := !model.Destination.IsNull() && !model.Destination.IsUnknown()
	var dest destinationModel
	if hasDest {
		diags.Append(model.Destination.As(ctx, &dest, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return nil, diags
		}
	}

	// Domains
	if hasDest && !dest.Domain.IsNull() && !dest.Domain.IsUnknown() {
		route.MatchingTarget = "DOMAIN"
		var domains []string
		diags.Append(dest.Domain.ElementsAs(ctx, &domains, false)...)
		if diags.HasError() {
			return nil, diags
		}

		route.Domains = make([]unifi.TrafficRouteDomains, len(domains))
		for i, d := range domains {
			route.Domains[i] = unifi.TrafficRouteDomains{
				Domain: d,
			}
		}
	}

	// Regions
	if hasDest && !dest.Region.IsNull() && !dest.Region.IsUnknown() {
		route.MatchingTarget = "REGION"
		var regions []string
		diags.Append(dest.Region.ElementsAs(ctx, &regions, false)...)
		route.Regions = regions
	}

	// IP (addresses and ranges combined)
	if hasDest && !dest.IP.IsNull() && !dest.IP.IsUnknown() {
		route.MatchingTarget = "IP"
		var ips []destinationIPModel
		diags.Append(dest.IP.ElementsAs(ctx, &ips, false)...)
		if diags.HasError() {
			return nil, diags
		}

		addresses, ranges, ok := trafficRouteDestinationIPToAPI(ctx, &diags, ips)
		if !ok {
			return nil, diags
		}
		route.IPAddresses = append(route.IPAddresses, addresses...)
		route.IPRanges = append(route.IPRanges, ranges...)
	}

	// Initialize empty slices for nil arrays.
	if route.Domains == nil {
		route.Domains = []unifi.TrafficRouteDomains{}
	}
	if route.Regions == nil {
		route.Regions = []string{}
	}
	if route.IPAddresses == nil {
		route.IPAddresses = []unifi.TrafficRouteIPAddresses{}
	}
	if route.IPRanges == nil {
		route.IPRanges = []unifi.TrafficRouteIPRanges{}
	}

	// Source → TargetDevices
	devices, ok := trafficRouteTargetDevicesToAPI(ctx, &diags, model.Source)
	if !ok {
		return nil, diags
	}
	route.TargetDevices = devices

	return route, diags
}

// trafficRouteDestinationIPToAPI splits destination.ip into the two observed
// arrays: an entry containing a hyphen becomes an ip_ranges record and anything
// else an ip_addresses one, and only an ip_addresses entry carries ports.
//
// ok is false when a port could not be parsed, which is the one condition that
// aborted the whole conversion before this was a function of its own. The
// partial result is returned with it and the caller discards it.
func trafficRouteDestinationIPToAPI(
	ctx context.Context,
	diags *diag.Diagnostics,
	ips []destinationIPModel,
) ([]unifi.TrafficRouteIPAddresses, []unifi.TrafficRouteIPRanges, bool) {
	var addresses []unifi.TrafficRouteIPAddresses
	var ranges []unifi.TrafficRouteIPRanges

	for _, ip := range ips {
		address := ip.Address.ValueString()

		// Detect IP range (contains "-" but is not CIDR)
		if strings.Contains(address, "-") {
			parts := strings.SplitN(address, "-", 2)
			entry := unifi.TrafficRouteIPRanges{
				Start:   strings.TrimSpace(parts[0]),
				Stop:    strings.TrimSpace(parts[1]),
				Version: unifi.TrafficRouteIPVersionV4,
			}
			if ipAddr, err := netip.ParseAddr(entry.Start); err == nil && ipAddr.Is6() {
				entry.Version = unifi.TrafficRouteIPVersionV6
			}
			ranges = append(ranges, entry)
			continue
		}

		entry := unifi.TrafficRouteIPAddresses{
			Address: address,
			Version: unifi.TrafficRouteIPVersionV4,
		}
		if ipAddr, err := netip.ParseAddr(address); err == nil && ipAddr.Is6() {
			entry.Version = unifi.TrafficRouteIPVersionV6
		}

		// Parse ports
		if !ip.Ports.IsNull() && !ip.Ports.IsUnknown() {
			var portStrs []string
			diags.Append(ip.Ports.ElementsAs(ctx, &portStrs, false)...)
			for _, ps := range portStrs {
				if strings.Contains(ps, "-") {
					rangeParts := strings.SplitN(ps, "-", 2)
					start, err1 := strconv.ParseInt(strings.TrimSpace(rangeParts[0]), 10, 64)
					stop, err2 := strconv.ParseInt(strings.TrimSpace(rangeParts[1]), 10, 64)
					if err1 != nil || err2 != nil {
						diags.AddError(
							"Invalid Port Range",
							fmt.Sprintf("could not parse port range %q", ps),
						)
						return nil, nil, false
					}
					entry.PortRanges = append(
						entry.PortRanges,
						unifi.TrafficRoutePortRanges{
							Start: &start,
							Stop:  &stop,
						},
					)
					continue
				}
				port, err := strconv.ParseInt(strings.TrimSpace(ps), 10, 64)
				if err != nil {
					diags.AddError(
						"Invalid Port",
						fmt.Sprintf("could not parse port %q", ps),
					)
					return nil, nil, false
				}
				entry.Ports = append(entry.Ports, port)
			}
		}

		addresses = append(addresses, entry)
	}

	return addresses, ranges, true
}

// trafficRouteTargetDevicesToAPI partitions source.clients and source.networks
// into the one observed array, each entry carrying its own type discriminator.
// A source naming nothing and an absent source both mean every client.
//
// ok is false when the source object could not be read, which is the one
// condition that aborted the whole conversion. A member that could not be read
// only records its diagnostic, as it did before.
func trafficRouteTargetDevicesToAPI(
	ctx context.Context,
	diags *diag.Diagnostics,
	source types.Object,
) ([]unifi.TrafficRouteTargetDevices, bool) {
	allClients := []unifi.TrafficRouteTargetDevices{{Type: "ALL_CLIENTS"}}
	if source.IsNull() || source.IsUnknown() {
		return allClients, true
	}

	var src sourceModel
	diags.Append(source.As(ctx, &src, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return nil, false
	}

	var devices []unifi.TrafficRouteTargetDevices

	// Networks
	if !src.Networks.IsNull() && !src.Networks.IsUnknown() {
		var networks []sourceNetworkModel
		diags.Append(src.Networks.ElementsAs(ctx, &networks, false)...)
		for _, n := range networks {
			devices = append(devices, unifi.TrafficRouteTargetDevices{
				NetworkID: n.ID.ValueString(),
				Type:      "NETWORK",
			})
		}
	}

	// Clients
	if !src.Clients.IsNull() && !src.Clients.IsUnknown() {
		var clients []sourceClientModel
		diags.Append(src.Clients.ElementsAs(ctx, &clients, false)...)
		for _, c := range clients {
			devices = append(devices, unifi.TrafficRouteTargetDevices{
				ClientMAC: c.MAC.ValueString(),
				Type:      "CLIENT",
			})
		}
	}

	if len(devices) == 0 {
		return allClients, true
	}
	return devices, true
}

// apiToModel converts the UniFi API struct to the Terraform model.
func (r *trafficRouteResource) apiToModel(
	ctx context.Context,
	route *unifi.TrafficRoute,
	model *trafficRouteResourceModel,
	site string,
) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(route.ID)
	model.Site = util.StringValueOrNull(site)
	model.Description = util.StringValueOrNull(route.Description)
	model.Enabled = types.BoolValue(route.Enabled)
	model.KillSwitchEnabled = types.BoolValue(route.KillSwitchEnabled)
	model.NetworkID = util.StringValueOrNull(route.NetworkID)
	model.NextHop = util.IPValueOrNull(route.NextHop)

	// Domains
	var domainsList types.List
	if len(route.Domains) > 0 {
		elements := make([]attr.Value, len(route.Domains))
		for i, dom := range route.Domains {
			elements[i] = types.StringValue(dom.Domain)
		}
		var d diag.Diagnostics
		domainsList, d = types.ListValue(types.StringType, elements)
		diags.Append(d...)
	} else {
		domainsList = types.ListNull(types.StringType)
	}

	// Regions
	var regionsList types.List
	if len(route.Regions) > 0 {
		elements := make([]attr.Value, len(route.Regions))
		for i, reg := range route.Regions {
			elements[i] = types.StringValue(reg)
		}
		var d diag.Diagnostics
		regionsList, d = types.ListValue(types.StringType, elements)
		diags.Append(d...)
	} else {
		regionsList = types.ListNull(types.StringType)
	}

	// IP (merge IPAddresses and IPRanges into unified list)
	ipList := trafficRouteDestinationIPFromAPI(ctx, &diags, route)

	// Build destination object.
	if len(route.Domains) > 0 || len(route.Regions) > 0 || len(route.IPAddresses) > 0 ||
		len(route.IPRanges) > 0 {
		dest := destinationModel{
			Domain: domainsList,
			IP:     ipList,
			Region: regionsList,
		}
		var d diag.Diagnostics
		model.Destination, d = types.ObjectValueFrom(ctx, destinationModel{}.AttributeTypes(), dest)
		diags.Append(d...)
	} else {
		model.Destination = types.ObjectNull(destinationModel{}.AttributeTypes())
	}

	// TargetDevices → Source
	model.Source = trafficRouteSourceFromAPI(ctx, &diags, route)

	return diags
}

// trafficRouteDestinationIPFromAPI merges the two observed arrays back into the
// one released list. An ip_ranges record reads as "start-stop" and carries no
// ports; an ip_addresses record reads as its address, with Ports and PortRanges
// rendered into the one ports list. Addresses come before ranges, which is the
// order the write does not preserve and the read therefore imposes.
func trafficRouteDestinationIPFromAPI(
	ctx context.Context,
	diags *diag.Diagnostics,
	route *unifi.TrafficRoute,
) types.List {
	elementType := types.ObjectType{AttrTypes: destinationIPModel{}.AttributeTypes()}
	if len(route.IPAddresses) == 0 && len(route.IPRanges) == 0 {
		return types.ListNull(elementType)
	}

	var ipElements []attr.Value

	for _, addr := range route.IPAddresses {
		// Build ports list from Ports + PortRanges
		var portStrings []attr.Value
		for _, p := range addr.Ports {
			portStrings = append(portStrings, types.StringValue(strconv.FormatInt(p, 10)))
		}
		for _, pr := range addr.PortRanges {
			if pr.Start != nil && pr.Stop != nil {
				portStrings = append(portStrings, types.StringValue(
					strconv.FormatInt(*pr.Start, 10)+"-"+strconv.FormatInt(*pr.Stop, 10),
				))
			}
		}

		ports := types.ListNull(types.StringType)
		if len(portStrings) > 0 {
			list, d := types.ListValue(types.StringType, portStrings)
			diags.Append(d...)
			ports = list
		}

		obj, d := types.ObjectValueFrom(ctx, destinationIPModel{}.AttributeTypes(), destinationIPModel{
			Address: types.StringValue(addr.Address),
			Ports:   ports,
		})
		diags.Append(d...)
		ipElements = append(ipElements, obj)
	}

	for _, ipRange := range route.IPRanges {
		obj, d := types.ObjectValueFrom(ctx, destinationIPModel{}.AttributeTypes(), destinationIPModel{
			Address: types.StringValue(ipRange.Start + "-" + ipRange.Stop),
			Ports:   types.ListNull(types.StringType),
		})
		diags.Append(d...)
		ipElements = append(ipElements, obj)
	}

	list, d := types.ListValue(elementType, ipElements)
	diags.Append(d...)
	return list
}

// trafficRouteSourceFromAPI partitions the one observed array on its own type
// discriminator: NETWORK entries become source.networks and CLIENT entries
// source.clients. ALL_CLIENTS is the default and is represented by omitting
// source entirely, so an array of nothing else reads as a null object.
func trafficRouteSourceFromAPI(
	ctx context.Context,
	diags *diag.Diagnostics,
	route *unifi.TrafficRoute,
) types.Object {
	networkType := types.ObjectType{AttrTypes: sourceNetworkModel{}.AttributeTypes()}
	clientType := types.ObjectType{AttrTypes: sourceClientModel{}.AttributeTypes()}

	var networkElements []attr.Value
	var clientElements []attr.Value

	for _, td := range route.TargetDevices {
		switch td.Type {
		case "NETWORK":
			obj, d := types.ObjectValueFrom(
				ctx,
				sourceNetworkModel{}.AttributeTypes(),
				sourceNetworkModel{ID: types.StringValue(td.NetworkID)},
			)
			diags.Append(d...)
			networkElements = append(networkElements, obj)
		case "CLIENT":
			obj, d := types.ObjectValueFrom(
				ctx,
				sourceClientModel{}.AttributeTypes(),
				sourceClientModel{MAC: types.StringValue(td.ClientMAC)},
			)
			diags.Append(d...)
			clientElements = append(clientElements, obj)
		case "ALL_CLIENTS":
			// ALL_CLIENTS is the default; represented by omitting source
		}
	}

	if len(networkElements) == 0 && len(clientElements) == 0 {
		return types.ObjectNull(sourceModel{}.AttributeTypes())
	}

	networksList := types.ListNull(networkType)
	if len(networkElements) > 0 {
		list, d := types.ListValue(networkType, networkElements)
		diags.Append(d...)
		networksList = list
	}

	clientsList := types.ListNull(clientType)
	if len(clientElements) > 0 {
		list, d := types.ListValue(clientType, clientElements)
		diags.Append(d...)
		clientsList = list
	}

	object, d := types.ObjectValueFrom(ctx, sourceModel{}.AttributeTypes(), sourceModel{
		Networks: networksList,
		Clients:  clientsList,
	})
	diags.Append(d...)
	return object
}

func (r *trafficRouteResource) defaultWANNetworkID(
	ctx context.Context,
	site string,
) (string, error) {
	networks, err := r.client.ListNetwork(ctx, site)
	if err != nil {
		return "", fmt.Errorf("unable to list networks: %w", err)
	}
	for _, n := range networks {
		if n.Purpose == unifi.PurposeWAN && n.WANNetworkGroup != nil &&
			*n.WANNetworkGroup == "WAN" {
			return n.ID, nil
		}
	}
	return "", fmt.Errorf("no default WAN network found")
}

// ListResourceConfigSchema implements [list.ListResource].
func (r *trafficRouteResource) ListResourceConfigSchema(
	ctx context.Context,
	_ list.ListResourceSchemaRequest,
	resp *list.ListResourceSchemaResponse,
) {
	resp.Schema = listresource_traffic_route.TrafficRouteListResourceSchema(ctx)
}

// List implements [list.ListResource].
func (r *trafficRouteResource) List(
	ctx context.Context,
	req list.ListRequest,
	stream *list.ListResultsStream,
) {
	var config trafficRouteListConfigModel

	diags := req.Config.Get(ctx, &config)
	if diags.HasError() {
		stream.Results = list.ListResultsStreamDiagnostics(diags)
		return
	}

	site := config.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	// Process filter blocks.
	var filters []trafficRouteListFilterModel
	if !config.Filter.IsNull() && !config.Filter.IsUnknown() {
		config.Filter.ElementsAs(ctx, &filters, false)
	}

	postFilters := make(map[string]string)
	for _, f := range filters {
		postFilters[f.Name.ValueString()] = f.Value.ValueString()
	}

	routes, err := r.client.ListTrafficRoute(ctx, site)
	if err != nil {
		var d diag.Diagnostics
		d.AddError("Error Listing Traffic Routes", "Could not list traffic routes: "+err.Error())
		stream.Results = list.ListResultsStreamDiagnostics(d)
		return
	}

	stream.Results = func(push func(list.ListResult) bool) {
		for _, route := range routes {
			// Apply enabled filter.
			if val, ok := postFilters["enabled"]; ok {
				enabled := fmt.Sprintf("%t", route.Enabled)
				if enabled != val {
					continue
				}
			}

			// Apply matching_target filter.
			if val, ok := postFilters["matching_target"]; ok {
				if route.MatchingTarget != val {
					continue
				}
			}

			// Apply network_id filter.
			if val, ok := postFilters["network_id"]; ok {
				if route.NetworkID != val {
					continue
				}
			}

			// Apply description filter.
			if val, ok := postFilters["description"]; ok {
				if route.Description != val {
					continue
				}
			}

			result := req.NewListResult(ctx)

			// Display name: prefer description, fall back to ID.
			if route.Description != "" {
				result.DisplayName = route.Description
			} else {
				result.DisplayName = route.ID
			}

			// Set identity.
			result.Diagnostics.Append(
				result.Identity.SetAttribute(
					ctx,
					path.Root("id"),
					types.StringValue(route.ID),
				)...,
			)

			// Convert to model.
			var model trafficRouteResourceModel
			result.Diagnostics.Append(r.apiToModel(ctx, &route, &model, site)...)
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
