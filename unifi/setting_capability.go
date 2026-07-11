package unifi

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// capabilityState classifies whether a given settings section is usable on
// the current controller. The settings engine consults this per section
// before attempting to read or write it.
type capabilityState int

const (
	// capSupported sections are present in the controller's settings
	// snapshot and may be read and written normally.
	capSupported capabilityState = iota

	// capUnsupported sections are absent from the controller's settings
	// snapshot: this controller (by version, platform, or licensing) does
	// not expose the section at all.
	capUnsupported

	// capUnmaterialized sections exist on this controller but have not
	// yet been created/populated server-side (e.g. an optional
	// sub-feature not yet enabled). Distinct from capUnsupported: the
	// section could come into existence without provider changes.
	capUnmaterialized

	// capUnauthorized sections exist on this controller but the
	// credentials used by the provider lack permission to read or write
	// them.
	capUnauthorized

	// capUnknown covers any case where capability could not be
	// determined (e.g. an inconclusive or errored probe).
	capUnknown
)

// sectionCapability classifies key's capability against snap.
//
// PR-A behavior is deliberately coarse: a section present in the snapshot is
// capSupported, and anything absent is capUnsupported. Distinguishing
// capUnmaterialized and capUnauthorized from a plain absence — and any real
// detection of capUnknown — requires per-section probing that is out of
// scope here and deferred to a later PR.
func sectionCapability(snap rawSettings, key string) capabilityState {
	if snap.has(key) {
		return capSupported
	}
	return capUnsupported
}

// configuredError returns the diagnostics to raise when a user has
// configured a section whose capability is c. It fails closed: any state
// other than capSupported or capUnmaterialized produces a clear error
// naming the section, since a section the user explicitly configured but
// cannot actually be managed on this controller must never be silently
// accepted. capUnmaterialized returns no error because the section may
// still come into existence without any provider-side change.
//
// Callers must only invoke this for sections the user actually configured;
// an unsupported section the user did not configure must never error here —
// that filtering is the engine's responsibility, not this function's.
func (c capabilityState) configuredError(section string) diag.Diagnostics {
	var diags diag.Diagnostics

	switch c {
	case capSupported, capUnmaterialized:
		return diags
	case capUnsupported:
		diags.AddError(
			"Settings section not supported",
			fmt.Sprintf("section %q is not supported on this controller and cannot be managed.", section),
		)
	case capUnauthorized:
		diags.AddError(
			"Settings section not authorized",
			fmt.Sprintf("section %q is not authorized for the credentials used by this provider and cannot be managed.", section),
		)
	case capUnknown:
		diags.AddError(
			"Settings section capability unknown",
			fmt.Sprintf("section %q's support on this controller could not be determined, so it cannot be managed.", section),
		)
	default:
		diags.AddError(
			"Settings section capability unknown",
			fmt.Sprintf("section %q has an unrecognized capability state and cannot be managed.", section),
		)
	}

	return diags
}
