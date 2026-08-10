// Package planmodifiers holds plan modifiers the provider's schemas reference
// by name.
//
// It exists to be a leaf. A generated schema is compiled into
// internal/generated/resource_<surface>, and package unifi imports those
// packages to serve them — so a plan modifier declared in package unifi is
// unreachable from the generated code that needs it, and moving the import the
// other way is a cycle. Anything a policy names in a plan_modifiers entry has
// to live somewhere that imports neither, which is here. unifi/validators is
// the same arrangement for validators, and is why power_supervisor migrated
// without this problem.
//
// Keeping the type exported is part of the same requirement: generated code is
// a different package and cannot name an unexported one.
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
// hwtypes.MACAddressType compares elements semantically, but a Set identifies
// its members by their string value, so that never reaches the set. Terraform
// also never consults semantic equality while building a plan — the framework
// applies it on create, read and update only. Rewriting an applied
// aa:bb:cc:dd:ee:ff as AA-BB-CC-DD-EE-FF would otherwise plan a change with
// nothing behind it. A real membership change still plans.
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
// It is exported because the read path needs it too: a resource refreshing from
// the controller has to make the same comparison before overwriting state, and
// that code lives in package unifi.
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
