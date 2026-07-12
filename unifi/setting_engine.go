package unifi

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// setting_engine.go ties the whole settings machine together: one
// ListSettings snapshot per read, presence-gated per-section decode
// (readSections); reconcile-before-mutate, deterministic-order PUT, and
// best-effort state recovery when the post-apply read-back itself fails
// (applySections); and the bestEffortState/carrySecretObject helpers that
// implement that recovery. No caller outside this file drives a
// settingsClient directly.

// listSnapshot performs the one ListSettings call for a settings-engine pass
// and wraps the result in a keyed rawSettings snapshot.
func listSnapshot(ctx context.Context, client settingsClient, site string) (rawSettings, error) {
	raw, err := client.ListSettings(ctx, site)
	if err != nil {
		return rawSettings{}, err
	}
	return newRawSettings(raw), nil
}

// readSections performs exactly one ListSettings call, then for each section
// in sections runs a fail-closed presence check before decoding.
//
// The settingSection interface has no "is this configured in plan"
// predicate, so readSections relies on its caller to have already filtered
// sections down to the relevant set (e.g. applySections passes only the
// sections it just PUT or the plan's configured set; an import/refresh
// caller passes the full registry). onlyConfigured selects which presence
// policy applies to every section in that set:
//
//   - true: the caller asserts the user configured each section here, so a
//     section missing from the snapshot is a hard error, fail closed.
//   - false: the caller is doing a best-effort pass over a broader set (e.g.
//     applySections' post-apply re-read, or import), so a section this
//     controller doesn't expose is silently skipped rather than erroring.
func readSections(ctx context.Context, sections []settingSection, client settingsClient, site string, prior settingResourceModel, model *settingResourceModel, onlyConfigured bool) diag.Diagnostics {
	var diags diag.Diagnostics

	snap, err := listSnapshot(ctx, client, site)
	if err != nil {
		diags.AddError("read settings failed", err.Error())
		return diags
	}

	for _, s := range sections {
		if !snap.has(s.key()) {
			if onlyConfigured {
				diags.AddError(
					"Settings section not supported",
					fmt.Sprintf("section %q is not present on this controller", s.key()),
				)
			}
			// Fail closed for this section only when configured: reported,
			// but does not block decoding of the remaining sections. When
			// not configured, a section this controller doesn't expose is
			// silently left untouched rather than erroring, since the
			// caller did not assert the user configured it.
			continue
		}
		diags.Append(s.decode(ctx, snap, prior, model)...)
	}

	return diags
}

// applySections snapshots current controller state, reconciles every
// configured section's write overlay against that ONE snapshot before any
// PUT is issued (reconcile-before-mutate: any overlay error aborts with no
// writes at all), PUTs each section that needs writing in deterministic
// (orderedSections) order, and re-reads canonical state afterward. If the
// re-read itself fails, state is assembled best-effort from what is known
// to have been successfully PUT.
func applySections(ctx context.Context, sections []settingSection, client settingsClient, site string, plan, prior settingResourceModel) (settingResourceModel, diag.Diagnostics) {
	var d diag.Diagnostics
	ordered := orderedSections(sections)
	snap, err := listSnapshot(ctx, client, site)
	if err != nil {
		d.AddError("read settings failed", err.Error())
		return prior, d
	}
	type pending struct {
		s  settingSection
		rs settings.RawSetting
	}
	var todo []pending
	for _, s := range ordered {
		// Both callers (setting_resource.go) already pass a config-filtered
		// sections slice (configuredSections(config)), so this plan-based
		// isConfigured check is now a redundant second gate for a genuinely
		// configured section: plan only diverges from config additively
		// (Computed/UseStateForUnknown attributes), so isConfigured(plan)
		// is always true whenever isConfigured(config) already was. Kept
		// anyway — it's the defensive check that keeps applySections
		// correct if it's ever called with an unfiltered sections set (e.g.
		// from a test), so it stays even though it's a no-op in the two
		// current call paths.
		if !s.isConfigured(plan) {
			continue // not user-configured — never checked, never written
		}
		if !snap.has(s.key()) {
			d.AddError(
				"Settings section not supported",
				fmt.Sprintf("section %q is not present on this controller", s.key()),
			)
			continue // record the fail-closed diagnostic; the aggregate HasError() gate below aborts before any PUT
		}
		rs, configured, sd := s.overlay(ctx, plan, prior, snap)
		d.Append(sd...)
		if configured {
			todo = append(todo, pending{s, rs})
		}
	}
	if d.HasError() {
		return prior, d // reconcile-before-mutate: nothing written (now also covers a configured-but-unsupported section)
	}
	put := map[string]bool{}
	var putErr error
	for _, p := range todo {
		if err := client.UpdateRawSetting(ctx, site, p.rs); err != nil {
			putErr = fmt.Errorf("section %q: %w", p.s.key(), err)
			break
		}
		put[p.s.key()] = true
	}
	out := plan
	rd := readSections(ctx, sections, client, site, plan, &out, false)
	if rd.HasError() {
		var bd diag.Diagnostics
		out, bd = bestEffortState(prior, plan, put, sections) // read-back failed after apply: recover state best-effort
		d.Append(bd...)
		// rd holds ERROR diagnostics from the failed read-back. Do NOT
		// d.Append(rd...) here: that would make d.HasError() true and flip
		// this from a warning into a hard failure, when the operation must
		// stay successful-with-warning, with best-effort state persisted.
		// Instead fold rd's per-section messages into the warning's detail
		// text so practitioners see which section/field failed to decode,
		// without rd's severity propagating.
		d.AddWarning("settings read-back failed after apply",
			"state written best-effort from applied values; run `terraform refresh`.\n"+
				"read-back errors: "+joinDiagMessages(rd))
	} else {
		d.Append(rd...) // surface type-drift warnings from the post-apply read
	}
	if putErr != nil {
		d.AddError("settings apply failed", putErr.Error())
	}
	return out, d
}

// joinDiagMessages renders diags as a semicolon-separated "Summary: Detail"
// list, for folding a Diagnostics value's specifics into another
// diagnostic's detail text (e.g. the apply path's read-back-failure warning)
// without appending diags itself and propagating its severity.
func joinDiagMessages(diags diag.Diagnostics) string {
	msgs := make([]string, 0, len(diags))
	for _, dg := range diags {
		msg := dg.Summary()
		if detail := dg.Detail(); detail != "" {
			msg += ": " + detail
		}
		msgs = append(msgs, msg)
	}
	return strings.Join(msgs, "; ")
}

// bestEffortState assembles a settingResourceModel for the case where the
// canonical post-apply read-back itself failed, so state cannot be read
// from the controller. It starts from prior (the only
// state known to be true before this apply) and, for each section that was
// successfully PUT this apply (put[key] == true), asks that section to
// carry its own best-effort value onto the result via carryBestEffort. dst
// (out) is seeded from prior before the loop, so a secret section's
// carryBestEffort can read its own prior secret straight off dst. A section
// that was NOT PUT this apply is left entirely as prior — it was never
// touched, so prior is already correct for it.
func bestEffortState(prior, plan settingResourceModel, put map[string]bool, sections []settingSection) (settingResourceModel, diag.Diagnostics) {
	var diags diag.Diagnostics
	out := prior
	for _, s := range sections {
		if !put[s.key()] {
			continue
		}
		diags.Append(s.carryBestEffort(&out, plan)...)
	}
	return out, diags
}

// carrySecretObject rebuilds plan's section object but keeps prior's secret
// leaf when plan's is null/unknown (write-only secrets are never in the
// controller read-back). Every non-secret leaf comes from plan.
//
// Traps this function must honor:
//  1. IsUnknown() is treated exactly like IsNull() for the secret leaf:
//     retain prior's value for both, matching overlay's own delete-on-either
//     behavior for write-only secrets.
//  2. A configured EMPTY STRING secret (types.StringValue("")) is a
//     rotate-to-empty that WAS sent this apply: it is kept from plan, never
//     replaced by prior.
//  3. If plan itself is null or unknown, prior is returned unchanged — a
//     known object is never manufactured from a null/unknown section.
//  4. Diagnostics are threaded out of the helper even though, structurally,
//     rebuilding a same-schema object cannot fail in practice.
func carrySecretObject(plan, prior types.Object, secretLeaf string) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics

	if plan.IsNull() || plan.IsUnknown() {
		return prior, diags
	}

	attrTypes := plan.AttributeTypes(context.Background())
	planAttrs := plan.Attributes()

	var priorAttrs map[string]attr.Value
	if !prior.IsNull() && !prior.IsUnknown() {
		priorAttrs = prior.Attributes()
	}

	out := make(map[string]attr.Value, len(planAttrs))
	for name, planVal := range planAttrs {
		if name == secretLeaf {
			if sv, ok := planVal.(types.String); ok && (sv.IsNull() || sv.IsUnknown()) {
				if priorAttrs != nil {
					if pv, ok := priorAttrs[name]; ok {
						out[name] = pv
						continue
					}
				}
				out[name] = planVal
				continue
			}
		}
		out[name] = planVal
	}

	obj, objDiags := types.ObjectValue(attrTypes, out)
	diags.Append(objDiags...)
	return obj, diags
}
