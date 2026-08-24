// Package planmodifiers holds plan modifiers the provider's schemas reference
// by name.
//
// It must stay a leaf package: package unifi imports the generated resource
// packages that reference these modifiers by name, so a modifier declared in
// package unifi would be unreachable from them. Its types must stay exported
// for the same reason — generated code cannot name an unexported one.
package planmodifiers

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// KeepEquivalentMACs keeps the stored set of MAC addresses when the
// configuration names the same addresses in a different format.
//
// Terraform only applies semantic equality on create, read and update, never
// while planning, so an equivalent MAC written differently would otherwise
// plan a spurious change.
type KeepEquivalentMACs struct{}

func (KeepEquivalentMACs) Description(_ context.Context) string {
	return "Keeps the stored MAC addresses when the configuration writes the same ones differently."
}

func (m KeepEquivalentMACs) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (KeepEquivalentMACs) PlanModifySet(
	ctx context.Context,
	req planmodifier.SetRequest,
	resp *planmodifier.SetResponse,
) {
	if req.StateValue.IsNull() || req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	var configMACs []string
	if diags := req.ConfigValue.ElementsAs(ctx, &configMACs, false); diags.HasError() {
		return
	}
	if MACSetsEqual(ctx, req.StateValue, configMACs) {
		resp.PlanValue = req.StateValue
	}
}

// canonicalMAC reduces a MAC to a comparable form, ignoring the separator and
// case differences that distinguish "AA-BB-CC-DD-EE-FF" from "aa:bb:cc:dd:ee:ff".
func canonicalMAC(mac string) string {
	return strings.ToLower(strings.NewReplacer("-", "", ":", "", ".", "").Replace(mac))
}

// MACSetsEqual reports whether a set already in state holds the same addresses
// the controller returned, disregarding how each one is written.
//
// It is exported for the read path in package unifi, which needs the same
// comparison before overwriting state.
func MACSetsEqual(ctx context.Context, current types.Set, apiMACs []string) bool {
	if current.IsNull() || current.IsUnknown() {
		return false
	}

	var stateMACs []string
	if diags := current.ElementsAs(ctx, &stateMACs, false); diags.HasError() {
		return false
	}
	if len(stateMACs) != len(apiMACs) {
		return false
	}

	seen := make(map[string]int, len(stateMACs))
	for _, mac := range stateMACs {
		seen[canonicalMAC(mac)]++
	}
	for _, mac := range apiMACs {
		key := canonicalMAC(mac)
		if seen[key] == 0 {
			return false
		}
		seen[key]--
	}
	return true
}
