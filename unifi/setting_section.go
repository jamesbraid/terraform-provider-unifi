package unifi

import (
	"context"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// settingSection is the contract every migrated settings section (Tasks
// 9-22) implements. The settings engine drives sections exclusively through
// this interface: it never reaches into a section's internals.
type settingSection interface {
	// key is the controller's settings-API key for this section (e.g.
	// "mgmt"), matching a settings.RawSetting.Key / rawSettings entry.
	key() string

	// attrName is the top-level Terraform attribute name for this section
	// on the setting resource (e.g. "mgmt").
	attrName() string

	// schemaAttribute returns this section's Terraform schema attribute,
	// nested under attrName() on the setting resource.
	schemaAttribute() schema.Attribute

	// ownership maps each leaf attribute path in schemaAttribute() to its
	// ownershipClass: attr path -> class; every schema leaf present.
	ownership() map[string]ownershipClass

	// decode populates model from snap (and, where a field's ownership
	// falls back to prior state, from prior).
	decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics

	// overlay computes this section's write payload from model (falling
	// back to prior/snap per ownership), returning the RawSetting to PUT
	// and whether a write is needed at all.
	overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics)

	// capability classifies whether this section is usable against snap.
	capability(snap rawSettings) capabilityState

	// carryBestEffort copies this section's own field from plan (or, for a
	// leaf classed ownerWriteOnlySecret, a per-leaf plan/prior choice via
	// bestEffortObject) onto dst, for C2.4 second-failure recovery after a
	// partial apply whose canonical re-read also failed. Implementations
	// touch only their own field on dst.
	carryBestEffort(dst *settingResourceModel, plan, prior settingResourceModel) diag.Diagnostics
}

// settingSections is the registry of all migrated settings sections. Tasks
// 9-22 populate it via registerSection during package init; PR-A registers
// none.
var settingSections []settingSection

// registerSection appends s to the settingSections registry. Intended to be
// called from each section's package-level init().
func registerSection(s settingSection) {
	settingSections = append(settingSections, s)
}

// orderedSections returns a copy of in sorted by key(), giving the settings
// engine a deterministic PUT order regardless of registration or caller
// order. in is never modified.
func orderedSections(in []settingSection) []settingSection {
	out := make([]settingSection, len(in))
	copy(out, in)
	sort.Slice(out, func(i, j int) bool {
		return out[i].key() < out[j].key()
	})
	return out
}
