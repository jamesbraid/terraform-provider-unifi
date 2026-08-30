package unifi

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ubiquiti-community/go-unifi/unifi"
)

// declaredPortOverride pairs a declared port override with the members the
// caller actually named for it. UpdateDevicePortOverrides cannot recover
// that from the Go struct alone -- every member is omitempty, so a field
// left at its zero value looks identical to one the caller never mentioned
// -- so the caller (the resource layer, working from what the Terraform
// config actually set) has to carry the field list forward explicitly.
type declaredPortOverride struct {
	Override unifi.DevicePortOverrides
	Fields   []string
}

// portOverrideGroup is everything one UpdateDevicePortOverrides call needs:
// a field mask and the ports that declare exactly that set of members.
type portOverrideGroup struct {
	Fields []string
	Ports  []unifi.DevicePortOverrides
}

// groupPortOverridesByFieldSet groups declared ports by the exact set of
// members they declare, in the order each distinct set first appears.
//
// UpdateDevicePortOverrides takes one member mask for the whole call, and
// applies it to every declared port. Measured against a live controller
// (task 1 of the port-overrides plan): a port that declares only "name"
// still carries "poe_mode" in the outgoing write when another port in the
// same call declares poe_mode -- at poe_mode's Go zero value, an empty
// string. On that measurement the controller's own validator rejected the
// empty string outright (poe_mode is enum-constrained) and refused the
// whole call; a member with a looser validator -- an ordinary string, a
// bool -- would have been accepted and silently overwritten. So ports with
// different declared member sets can never share a call. The grouping key
// is the *set* of names, not their order and not their union: an update
// touching N distinct member-sets costs N calls, not one.
func groupPortOverridesByFieldSet(declared []declaredPortOverride) []portOverrideGroup {
	order := make([]string, 0, len(declared))
	byKey := make(map[string]*portOverrideGroup, len(declared))

	for _, d := range declared {
		fields := slices.Clone(d.Fields)
		slices.Sort(fields)
		fields = slices.Compact(fields)
		key := strings.Join(fields, ",")

		g, ok := byKey[key]
		if !ok {
			g = &portOverrideGroup{Fields: fields}
			byKey[key] = g
			order = append(order, key)
		}
		g.Ports = append(g.Ports, d.Override)
	}

	out := make([]portOverrideGroup, 0, len(order))
	for _, key := range order {
		out = append(out, *byKey[key])
	}
	return out
}

// updateDevicePortOverridesGrouped writes declared port overrides to the
// controller, issuing one UpdateDevicePortOverrides call per distinct
// declared member set rather than one call carrying every declared field.
// See groupPortOverridesByFieldSet for why a single union-mask call is
// unsafe.
//
// Not yet wired into the device resource's update path -- that is the next
// task in the port-overrides plan, which changes the resource layer to
// track each port's declared fields instead of assuming the whole struct.
func updateDevicePortOverridesGrouped(
	ctx context.Context,
	client *unifi.ApiClient,
	site string,
	device *unifi.Device,
	declared []declaredPortOverride,
) (*unifi.Device, error) {
	groups := groupPortOverridesByFieldSet(declared)
	if len(groups) == 0 {
		return nil, fmt.Errorf(
			"no port overrides were declared.\n\n" +
				"This writes the ports it is given and leaves the rest alone, so an empty " +
				"list would be a no-op rather than a way to clear them")
	}

	updated := device
	for _, g := range groups {
		var err error
		updated, err = client.UpdateDevicePortOverrides(ctx, site, updated, g.Ports, g.Fields...)
		if err != nil {
			return nil, fmt.Errorf("port overrides %v: %w", g.Fields, err)
		}
	}
	return updated, nil
}
