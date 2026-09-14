package unifi

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ubiquiti-community/go-unifi/unifi"
)

// declaredRadio pairs a declared radio with the members the caller actually
// named for it. UpdateDeviceRadioTable cannot recover that from the Go struct
// alone -- every member is omitempty, so a member left at its zero value looks
// identical to one the caller never mentioned -- so the caller (the resource
// layer, working from what the Terraform config actually set) has to carry the
// field list forward explicitly. The same shape as declaredPortOverride.
type declaredRadio struct {
	Radio  unifi.DeviceRadioTable
	Fields []string
}

// radioGroup is everything one UpdateDeviceRadioTable call needs: a member mask
// and the radios that declare exactly that set of members.
type radioGroup struct {
	Fields []string
	Radios []unifi.DeviceRadioTable
}

// groupRadiosByFieldSet groups declared radios by the exact set of members they
// declare, in the order each distinct set first appears.
//
// UpdateDeviceRadioTable takes one member mask for the whole call and applies
// it to every declared radio. A radio that declares only "channel" would still
// carry "maxsta" in the outgoing write when another radio in the same call
// declared maxsta -- at maxsta's zero value -- and radio_table merges member by
// member, so the controller would apply that zero and clobber the first radio's
// stored maxsta. So radios with different declared member sets can never share
// a call. This is the same grouping groupPortOverridesByFieldSet performs, and
// for the same reason; the difference between the two writers is the merge
// (radio_table keeps what a mask omits, port_overrides replaces it), which does
// not change the need to group. The grouping key is the set of names, not their
// order and not their union: an update touching N distinct member-sets costs N
// calls, not one.
func groupRadiosByFieldSet(declared []declaredRadio) []radioGroup {
	order := make([]string, 0, len(declared))
	byKey := make(map[string]*radioGroup, len(declared))

	for _, d := range declared {
		fields := slices.Clone(d.Fields)
		slices.Sort(fields)
		fields = slices.Compact(fields)
		key := strings.Join(fields, ",")

		g, ok := byKey[key]
		if !ok {
			g = &radioGroup{Fields: fields}
			byKey[key] = g
			order = append(order, key)
		}
		g.Radios = append(g.Radios, d.Radio)
	}

	out := make([]radioGroup, 0, len(order))
	for _, key := range order {
		out = append(out, *byKey[key])
	}
	return out
}

// updateDeviceRadioTableGrouped writes declared radios to the controller,
// issuing one UpdateDeviceRadioTable call per distinct declared member set
// rather than one call carrying every declared field. See groupRadiosByFieldSet
// for why a single union-mask call is unsafe. Wired into the device resource's
// update path by deviceKitBeforeSend.
func updateDeviceRadioTableGrouped(
	ctx context.Context,
	client *unifi.ApiClient,
	site string,
	device *unifi.Device,
	declared []declaredRadio,
) (*unifi.Device, error) {
	groups := groupRadiosByFieldSet(declared)
	if len(groups) == 0 {
		return nil, fmt.Errorf(
			"no radios were declared.\n\n" +
				"This writes the radios it is given and leaves the rest alone, so an empty " +
				"list would be a no-op rather than a way to clear them")
	}

	updated := device
	for _, g := range groups {
		var err error
		updated, err = client.UpdateDeviceRadioTable(ctx, site, updated, g.Radios, g.Fields...)
		if err != nil {
			return nil, fmt.Errorf("radio table %v: %w", g.Fields, err)
		}
	}
	return updated, nil
}
