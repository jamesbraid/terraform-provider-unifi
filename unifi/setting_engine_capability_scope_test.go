package unifi

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// setting_engine_capability_scope_test.go regression-tests two fail-closed
// scoping bugs: the settings engine must scope its fail-closed handling to
// the sections the user actually CONFIGURED, both on the read path
// (Read/readSections) and the write path (applySections). Before the fix:
//
//   - Bug 1: Read passed the full 13-section registry to
//     readSections(..., onlyConfigured=true), which fails closed on every
//     section absent from the snapshot, so a controller missing any section
//     (e.g. radius/usg on a gateway-less UDM) broke refresh even for
//     sections the user never configured.
//   - Bug 2: applySections overlaid + PUT every configured section without
//     a presence check, so a configured-but-unsupported section was only
//     caught by the controller AFTER other sections had already been
//     written (partial apply) instead of a clean pre-mutation abort.
//
// These 4 tests drive the fake seam directly (readSections/applySections),
// mirroring setting_engine_lifecycle_test.go's use of realSections() and the
// allSectionsNullModel()/dpiObject() fixtures.

// ---------------------------------------------------------------------------
// 1. Read (via readSections, filtered to isConfigured like the real Read
//    does) over a snapshot WITHOUT radius (and without any other unconfigured
//    section) succeeds: no spurious "radius not supported" for a section the
//    user never configured.
// ---------------------------------------------------------------------------

func TestCapabilityScope_ReadConfiguredOnly_SucceedsWhenUnconfiguredSectionAbsent(t *testing.T) {
	ctx := context.Background()
	client := newFakeSettingsClient()
	// Only dpi is present on the controller snapshot. radius and every other
	// section are absent from the snapshot -- but since only dpi is
	// configured in prior, only dpi should be checked/decoded.
	client.sections["dpi"] = rawSection("dpi", map[string]any{
		"enabled":               true,
		"fingerprintingEnabled": false,
	})

	prior := allSectionsNullModel()
	prior.Dpi = dpiObject(t, ctx, true, false)
	// Everything else (including radius) stays null == unconfigured.

	configured := make([]settingSection, 0, 1)
	for _, s := range realSections() {
		if s.isConfigured(prior) {
			configured = append(configured, s)
		}
	}
	if len(configured) != 1 || configured[0].key() != "dpi" {
		t.Fatalf("configured sections = %v, want exactly [dpi]", sectionKeys(configured))
	}

	var model settingResourceModel
	diags := readSections(ctx, configured, client, "default", prior, &model, true)
	if diags.HasError() {
		t.Fatalf("Read filtered to configured sections must succeed with radius absent-but-unconfigured, got: %v", diags)
	}
	if model.Dpi.IsNull() {
		t.Error("model.Dpi is null, want populated from the fake's seeded dpi section")
	}
}

// ---------------------------------------------------------------------------
// 2. Read where a CONFIGURED section (dpi) is ABSENT from the snapshot fails
//    closed with the exact "section dpi not supported" diagnostic.
// ---------------------------------------------------------------------------

func TestCapabilityScope_ReadConfiguredOnly_FailsClosedWhenConfiguredSectionAbsent(t *testing.T) {
	ctx := context.Background()
	client := newFakeSettingsClient() // empty: dpi is absent from the controller

	prior := allSectionsNullModel()
	prior.Dpi = dpiObject(t, ctx, true, false) // user configured dpi

	configured := make([]settingSection, 0, 1)
	for _, s := range realSections() {
		if s.isConfigured(prior) {
			configured = append(configured, s)
		}
	}
	if len(configured) != 1 || configured[0].key() != "dpi" {
		t.Fatalf("configured sections = %v, want exactly [dpi]", sectionKeys(configured))
	}

	var model settingResourceModel
	diags := readSections(ctx, configured, client, "default", prior, &model, true)
	if !diags.HasError() {
		t.Fatalf("expected a fail-closed diagnostic for configured-but-absent dpi, got none")
	}

	found := false
	for _, d := range diags.Errors() {
		if d.Summary() == "Settings section not supported" && strings.Contains(d.Detail(), `section "dpi"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf(`expected diagnostic Summary="Settings section not supported" mentioning section "dpi", got: %v`, diags)
	}
}

// ---------------------------------------------------------------------------
// 3. applySections with one supported-configured (dpi) + one absent-configured
//    (radius) section returns an error diagnostic AND records ZERO puts
//    (reconcile-before-mutate, now also gated on section presence).
// ---------------------------------------------------------------------------

func TestCapabilityScope_ApplySections_ZeroPutsWhenConfiguredSectionUnsupported(t *testing.T) {
	ctx := context.Background()
	client := newFakeSettingsClient()
	// dpi is supported on this controller; radius is NOT (absent from the
	// snapshot).
	client.sections["dpi"] = rawSection("dpi", map[string]any{
		"enabled":               false,
		"fingerprintingEnabled": false,
	})

	prior := allSectionsNullModel()
	plan := allSectionsNullModel()
	plan.Dpi = dpiObject(t, ctx, true, false)
	plan.Radius = radiusObject(t, ctx, basetypes.NewStringValue("s3cr3t"))

	_, diags := applySections(ctx, realSections(), client, "default", plan, prior)
	if !diags.HasError() {
		t.Fatalf("expected an error diagnostic for configured-but-unsupported radius, got none")
	}
	found := false
	for _, d := range diags.Errors() {
		if d.Summary() == "Settings section not supported" && strings.Contains(d.Detail(), `section "radius"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf(`expected diagnostic Summary="Settings section not supported" mentioning section "radius", got: %v`, diags)
	}
	if len(client.puts) != 0 {
		t.Fatalf("expected ZERO puts when a configured section is unsupported (reconcile-before-mutate), got %d: %+v", len(client.puts), client.puts)
	}
}

// ---------------------------------------------------------------------------
// 4. applySections with only supported-configured sections plus some
//    unconfigured-ABSENT sections succeeds and writes only the configured
//    sections; the unconfigured-absent sections never block the apply.
// ---------------------------------------------------------------------------

func TestCapabilityScope_ApplySections_UnconfiguredAbsentSectionsDoNotBlock(t *testing.T) {
	ctx := context.Background()
	client := newFakeSettingsClient()
	// Only dpi and country are present on this controller. Every other
	// section (radius, usg, mgmt, ips, ...) is absent -- but none of them
	// are configured in plan, so their absence must not block the apply.
	client.sections["dpi"] = rawSection("dpi", map[string]any{
		"enabled":               false,
		"fingerprintingEnabled": false,
	})
	client.sections["country"] = rawSection("country", map[string]any{"code": float64(0)})

	prior := allSectionsNullModel()
	plan := allSectionsNullModel()
	plan.Dpi = dpiObject(t, ctx, true, false)
	plan.Country = countryObject(t, ctx, 840)
	// radius, usg, mgmt, ips, etc. all remain null (unconfigured) and are
	// absent from the fake controller snapshot.

	out, diags := applySections(ctx, realSections(), client, "default", plan, prior)
	if diags.HasError() {
		t.Fatalf("unconfigured-absent sections must not block a valid apply, got: %v", diags)
	}

	wantPut := map[string]bool{"dpi": true, "country": true}
	if len(client.puts) != len(wantPut) {
		t.Fatalf("puts = %d, want %d: %+v", len(client.puts), len(wantPut), client.puts)
	}
	for _, p := range client.puts {
		if !wantPut[p.Key] {
			t.Errorf("unexpected PUT for key %q (not configured)", p.Key)
		}
	}

	if out.Dpi.IsNull() {
		t.Error("out.Dpi is null after a successful apply, want populated")
	}
	if out.Country.IsNull() {
		t.Error("out.Country is null after a successful apply, want populated")
	}
	// Unconfigured, unsupported sections must remain null/untouched.
	if !out.Radius.IsNull() {
		t.Error("out.Radius should remain null: never configured, never supported on this fake")
	}
}

// sectionKeys is a small test helper for readable failure messages.
func sectionKeys(sections []settingSection) []string {
	out := make([]string, len(sections))
	for i, s := range sections {
		out[i] = s.key()
	}
	return out
}
