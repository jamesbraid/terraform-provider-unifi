package unifi

import (
	"context"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/iptypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_traffic_route"
	resource_traffic_route "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_traffic_route"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

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
	return map[string]attr.Type{"id": types.StringType}
}

// sourceClientModel describes a nested source.clients entry.
type sourceClientModel struct {
	MAC types.String `tfsdk:"mac"`
}

func (m sourceClientModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{"mac": types.StringType}
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

type trafficRouteKitModel struct {
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

// trafficRouteKitSpec is the whole of what varies.
//
// matching_target defaults to INTERNET and target_devices to a single
// ALL_CLIENTS entry in New, so a route naming neither destination nor source
// still says "all traffic, all clients." Seeding alone isn't enough to reach
// the controller though: ScatteredObjectField writes nothing when its model
// value is null, so both names also need AlwaysWire to travel.
//
// network_id is AlwaysWire for a different reason: it's Optional+Computed, so
// a plan that omits it holds unknown and SetInPlan has nothing to send.
// BeforeSend fills it from Prefetch's inventory when left empty.
func trafficRouteKitSpec() resourcekit.Spec[trafficRouteKitModel, ui.TrafficRoute] {
	return resourcekit.Spec[trafficRouteKitModel, ui.TrafficRoute]{
		TypeName: "traffic_route",
		Subject:  "Traffic Route",
		New: func() *ui.TrafficRoute {
			return &ui.TrafficRoute{
				MatchingTarget: "INTERNET",
				TargetDevices:  []ui.TrafficRouteTargetDevices{{Type: trafficRouteAllClients}},
			}
		},
		ID:       func(m *trafficRouteKitModel) *types.String { return &m.ID },
		Site:     func(m *trafficRouteKitModel) *types.String { return &m.Site },
		Timeouts: func(m *trafficRouteKitModel) *timeouts.Value { return &m.Timeouts },
		// Prefetch is bound in Configure, where the client exists.
		BeforeSend: trafficRouteBeforeSend,
		AlwaysWire: []string{"matching_target", "target_devices", "network_id"},
		Fields: []resourcekit.Field[trafficRouteKitModel, ui.TrafficRoute]{
			resourcekit.StringField[trafficRouteKitModel, ui.TrafficRoute]{
				Wire:  "description",
				Model: func(m *trafficRouteKitModel) *types.String { return &m.Description },
				SDK:   func(s *ui.TrafficRoute) *string { return &s.Description },
				Elide: resourcekit.NullZero,
			},
			resourcekit.BoolField[trafficRouteKitModel, ui.TrafficRoute]{
				Wire:  "enabled",
				Model: func(m *trafficRouteKitModel) *types.Bool { return &m.Enabled },
				SDK:   func(s *ui.TrafficRoute) *bool { return &s.Enabled },
			},
			resourcekit.BoolField[trafficRouteKitModel, ui.TrafficRoute]{
				Wire:  "kill_switch_enabled",
				Model: func(m *trafficRouteKitModel) *types.Bool { return &m.KillSwitchEnabled },
				SDK:   func(s *ui.TrafficRoute) *bool { return &s.KillSwitchEnabled },
			},
			resourcekit.StringField[trafficRouteKitModel, ui.TrafficRoute]{
				Wire:  "network_id",
				Model: func(m *trafficRouteKitModel) *types.String { return &m.NetworkID },
				SDK:   func(s *ui.TrafficRoute) *string { return &s.NetworkID },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringLikeField[trafficRouteKitModel, ui.TrafficRoute, iptypes.IPAddress]{
				Wire:  "next_hop",
				Model: func(m *trafficRouteKitModel) *iptypes.IPAddress { return &m.NextHop },
				SDK:   func(s *ui.TrafficRoute) *string { return &s.NextHop },
				New: func(v basetypes.StringValue) iptypes.IPAddress {
					return iptypes.IPAddress{StringValue: v}
				},
				Elide: resourcekit.NullZero,
			},
			// destination spans five wires and the fifth is a discriminator:
			// domains, regions, ip_addresses and ip_ranges hold the values, and
			// matching_target says which one the controller reads -- omitting
			// it on a change would leave the route matching nothing. If a
			// practitioner fills in two members, ip beats region beats domain
			// (branch order below); that precedence is preserved intentionally.
			resourcekit.ScatteredObjectField[trafficRouteKitModel, ui.TrafficRoute]{
				Wires: []string{
					"domains",
					"regions",
					"ip_addresses",
					"ip_ranges",
					"matching_target",
				},
				Model:     func(m *trafficRouteKitModel) *types.Object { return &m.Destination },
				AttrTypes: destinationModel{}.AttributeTypes(),
				Elide:     resourcekit.NullZero,
				Encode:    trafficRouteDestinationToAPI,
				Decode:    trafficRouteDestinationFromAPI,
			},
			// source scatters across exactly one wire: ObjectListField needs a
			// list on the model (target_devices is one object), and
			// ObjectField needs an SDK struct per model object (there is no
			// TrafficRouteSource). This kind is about a model object with no
			// SDK object to correspond to; how many wires it lands on is
			// incidental.
			resourcekit.ScatteredObjectField[trafficRouteKitModel, ui.TrafficRoute]{
				Wires:     []string{"target_devices"},
				Model:     func(m *trafficRouteKitModel) *types.Object { return &m.Source },
				AttrTypes: sourceModel{}.AttributeTypes(),
				Elide:     resourcekit.NullZero,
				Encode:    trafficRouteSourceToAPI,
				Decode:    trafficRouteSourceDecode,
			},
		},
		// The identity accessors are seeded here rather than left to Configure,
		// which replaces this whole Backend with the client-bound one. They are
		// struct accessors and need no client, so ToModel works on a spec that
		// has never been configured -- which is how every unit test reaches it.
		Backend: resourcekit.Backend[ui.TrafficRoute]{
			GetID: func(s *ui.TrafficRoute) string { return s.ID },
			SetID: func(s *ui.TrafficRoute, id string) { s.ID = id },
		},
	}
}

// trafficRouteAllClients is the target_devices entry meaning "every client",
// which is what an absent source means.
const trafficRouteAllClients = "ALL_CLIENTS"

// trafficRouteSourceToAPI is the source object's Encode half: two model lists
// partitioned into the one observed array.
func trafficRouteSourceToAPI(
	ctx context.Context,
	object types.Object,
	sdk *ui.TrafficRoute,
) diag.Diagnostics {
	var diags diag.Diagnostics
	devices, ok := trafficRouteTargetDevicesToAPI(ctx, &diags, object)
	if !ok {
		return diags
	}
	sdk.TargetDevices = devices
	return diags
}

// trafficRouteSourceDecode is its counterpart, splitting the array back out.
func trafficRouteSourceDecode(
	ctx context.Context,
	sdk *ui.TrafficRoute,
	_ types.Object,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	return trafficRouteSourceFromAPI(ctx, &diags, sdk), diags
}

// trafficRouteDestinationToAPI writes the object's three members onto the four
// arrays and sets the discriminator.
func trafficRouteDestinationToAPI(
	ctx context.Context,
	object types.Object,
	route *ui.TrafficRoute,
) diag.Diagnostics {
	var diags diag.Diagnostics

	var dest destinationModel
	diags.Append(object.As(ctx, &dest, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}

	if !dest.Domain.IsNull() && !dest.Domain.IsUnknown() {
		route.MatchingTarget = "DOMAIN"
		var domains []string
		diags.Append(dest.Domain.ElementsAs(ctx, &domains, false)...)
		if diags.HasError() {
			return diags
		}
		route.Domains = make([]ui.TrafficRouteDomains, len(domains))
		for i, d := range domains {
			route.Domains[i] = ui.TrafficRouteDomains{Domain: d}
		}
	}

	if !dest.Region.IsNull() && !dest.Region.IsUnknown() {
		route.MatchingTarget = "REGION"
		var regions []string
		diags.Append(dest.Region.ElementsAs(ctx, &regions, false)...)
		route.Regions = regions
	}

	if !dest.IP.IsNull() && !dest.IP.IsUnknown() {
		route.MatchingTarget = "IP"
		var ips []destinationIPModel
		diags.Append(dest.IP.ElementsAs(ctx, &ips, false)...)
		if diags.HasError() {
			return diags
		}
		addresses, ranges, ok := trafficRouteDestinationIPToAPI(ctx, &diags, ips)
		if !ok {
			return diags
		}
		route.IPAddresses = append(route.IPAddresses, addresses...)
		route.IPRanges = append(route.IPRanges, ranges...)
	}

	return diags
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
) ([]ui.TrafficRouteIPAddresses, []ui.TrafficRouteIPRanges, bool) {
	var addresses []ui.TrafficRouteIPAddresses
	var ranges []ui.TrafficRouteIPRanges

	for _, ip := range ips {
		address := ip.Address.ValueString()

		if strings.Contains(address, "-") {
			parts := strings.SplitN(address, "-", 2)
			entry := ui.TrafficRouteIPRanges{
				Start:   strings.TrimSpace(parts[0]),
				Stop:    strings.TrimSpace(parts[1]),
				Version: ui.TrafficRouteIPVersionV4,
			}
			if ipAddr, err := netip.ParseAddr(entry.Start); err == nil && ipAddr.Is6() {
				entry.Version = ui.TrafficRouteIPVersionV6
			}
			ranges = append(ranges, entry)
			continue
		}

		entry := ui.TrafficRouteIPAddresses{
			Address: address,
			Version: ui.TrafficRouteIPVersionV4,
		}
		if ipAddr, err := netip.ParseAddr(address); err == nil && ipAddr.Is6() {
			entry.Version = ui.TrafficRouteIPVersionV6
		}

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
					entry.PortRanges = append(entry.PortRanges, ui.TrafficRoutePortRanges{
						Start: &start,
						Stop:  &stop,
					})
					continue
				}
				port, err := strconv.ParseInt(strings.TrimSpace(ps), 10, 64)
				if err != nil {
					diags.AddError("Invalid Port", fmt.Sprintf("could not parse port %q", ps))
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
) ([]ui.TrafficRouteTargetDevices, bool) {
	allClients := []ui.TrafficRouteTargetDevices{{Type: trafficRouteAllClients}}
	if source.IsNull() || source.IsUnknown() {
		return allClients, true
	}

	var src sourceModel
	diags.Append(source.As(ctx, &src, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return nil, false
	}

	var devices []ui.TrafficRouteTargetDevices

	if !src.Networks.IsNull() && !src.Networks.IsUnknown() {
		var networks []sourceNetworkModel
		diags.Append(src.Networks.ElementsAs(ctx, &networks, false)...)
		for _, n := range networks {
			devices = append(devices, ui.TrafficRouteTargetDevices{
				NetworkID: n.ID.ValueString(),
				Type:      "NETWORK",
			})
		}
	}

	if !src.Clients.IsNull() && !src.Clients.IsUnknown() {
		var clients []sourceClientModel
		diags.Append(src.Clients.ElementsAs(ctx, &clients, false)...)
		for _, c := range clients {
			devices = append(devices, ui.TrafficRouteTargetDevices{
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

// trafficRouteDestinationFromAPI rebuilds the object from the four arrays.
// An all-empty read is a null object, which is what NullZero records.
func trafficRouteDestinationFromAPI(
	ctx context.Context,
	route *ui.TrafficRoute,
	_ types.Object,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics

	if len(route.Domains) == 0 && len(route.Regions) == 0 &&
		len(route.IPAddresses) == 0 && len(route.IPRanges) == 0 {
		return types.ObjectNull(destinationModel{}.AttributeTypes()), diags
	}

	domains := types.ListNull(types.StringType)
	if len(route.Domains) > 0 {
		elements := make([]attr.Value, len(route.Domains))
		for i, dom := range route.Domains {
			elements[i] = types.StringValue(dom.Domain)
		}
		list, d := types.ListValue(types.StringType, elements)
		diags.Append(d...)
		domains = list
	}

	regions := types.ListNull(types.StringType)
	if len(route.Regions) > 0 {
		elements := make([]attr.Value, len(route.Regions))
		for i, reg := range route.Regions {
			elements[i] = types.StringValue(reg)
		}
		list, d := types.ListValue(types.StringType, elements)
		diags.Append(d...)
		regions = list
	}

	object, d := types.ObjectValueFrom(ctx, destinationModel{}.AttributeTypes(), destinationModel{
		Domain: domains,
		IP:     trafficRouteDestinationIPFromAPI(ctx, &diags, route),
		Region: regions,
	})
	diags.Append(d...)
	return object, diags
}

// trafficRouteDestinationIPFromAPI merges the two observed arrays back into the
// one released list. An ip_ranges record reads as "start-stop" and carries no
// ports; an ip_addresses record reads as its address, with Ports and PortRanges
// rendered into the one ports list. Addresses come before ranges, which is the
// order the write does not preserve and the read therefore imposes.
func trafficRouteDestinationIPFromAPI(
	ctx context.Context,
	diags *diag.Diagnostics,
	route *ui.TrafficRoute,
) types.List {
	elementType := types.ObjectType{AttrTypes: destinationIPModel{}.AttributeTypes()}
	if len(route.IPAddresses) == 0 && len(route.IPRanges) == 0 {
		return types.ListNull(elementType)
	}

	var ipElements []attr.Value

	for _, addr := range route.IPAddresses {
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
	route *ui.TrafficRoute,
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
		case trafficRouteAllClients:
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

// trafficRoutePrefetchWANID finds the site's primary WAN network once per
// operation, deferred rather than eager: Prefetch runs on every create, read
// and update, so an eager lookup would cost a request per apply even when
// network_id is set. The closure only calls on first use.
func trafficRoutePrefetchWANID(
	client *ui.ApiClient,
) func(context.Context, string) (any, diag.Diagnostics) {
	return func(_ context.Context, site string) (any, diag.Diagnostics) {
		return func(ctx context.Context) (string, error) {
			networks, err := client.ListNetwork(ctx, site)
			if err != nil {
				return "", fmt.Errorf("unable to list networks: %w", err)
			}
			for _, n := range networks {
				if n.Purpose == ui.PurposeWAN && n.WANNetworkGroup != nil &&
					*n.WANNetworkGroup == "WAN" {
					return n.ID, nil
				}
			}
			return "", fmt.Errorf("no default WAN network found")
		}, nil
	}
}

// trafficRouteBeforeSend fills network_id from the primary WAN when the
// encoded object left it empty. Reads sdk, not the model: network_id is
// Optional+Computed, so an omitted plan holds the prior value, not null --
// checking the model would re-derive a network_id the practitioner had
// pinned.
func trafficRouteBeforeSend(
	ctx context.Context,
	_, _ *trafficRouteKitModel,
	_ trafficRouteKitModel,
	sdk *ui.TrafficRoute,
	prefetched any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	if sdk.NetworkID != "" {
		return diags
	}
	lookup, ok := prefetched.(func(context.Context) (string, error))
	if !ok {
		diags.AddError(
			"Error Finding Default WAN Network",
			"the traffic_route resource was configured without a network lookup",
		)
		return diags
	}
	id, err := lookup(ctx)
	if err != nil {
		diags.AddError("Error Finding Default WAN Network", err.Error())
		return diags
	}
	sdk.NetworkID = id
	return diags
}

func trafficRouteKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_traffic_route.TrafficRouteResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

func trafficRouteKitList() resourcekit.ListSpec[ui.TrafficRoute] {
	return resourcekit.ListSpec[ui.TrafficRoute]{
		ConfigSchema: listresource_traffic_route.TrafficRouteListResourceSchema,
		DisplayName: func(s *ui.TrafficRoute) string {
			if s.Description != "" {
				return s.Description
			}
			return s.ID
		},
		Filters: map[string]func(*ui.TrafficRoute) string{
			"description":     func(s *ui.TrafficRoute) string { return s.Description },
			"enabled":         func(s *ui.TrafficRoute) string { return fmt.Sprintf("%t", s.Enabled) },
			"matching_target": func(s *ui.TrafficRoute) string { return s.MatchingTarget },
			"network_id":      func(s *ui.TrafficRoute) string { return s.NetworkID },
		},
	}
}

func trafficRouteKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.TrafficRoute] {
	return resourcekit.Backend[ui.TrafficRoute]{
		Create: func(ctx context.Context, site string, in *ui.TrafficRoute) (*ui.TrafficRoute, error) {
			return client.CreateTrafficRoute(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.TrafficRoute, error) {
			return client.GetTrafficRoute(ctx, site, id)
		},
		UpdateFields: func(ctx context.Context, site string, in *ui.TrafficRoute, fields ...string) (*ui.TrafficRoute, error) {
			return client.UpdateTrafficRouteFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.DeleteTrafficRoute(ctx, site, id)
		},
		List: func(ctx context.Context, site string) ([]ui.TrafficRoute, error) {
			return client.ListTrafficRoute(ctx, site)
		},
		GetID: func(s *ui.TrafficRoute) string { return s.ID },
		SetID: func(s *ui.TrafficRoute, id string) { s.ID = id },
	}
}
