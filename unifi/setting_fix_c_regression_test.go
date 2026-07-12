package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// setting_fix_c_regression_test.go regression-tests codex Findings 4 & 5
// (fix-C-brief.md): two lifecycle-correctness bugs surfaced by interactions
// between earlier fixes and import-hydration / omitted-list preservation.
//
//   - Finding 4: Create/Update must derive the "configured sections" set
//     passed to applySections from req.Config (the authoritative
//     user-configured signal), NOT from plan. After an import,
//     UseStateForUnknown fills the plan with hydrated Computed sections the
//     user never configured in config; passing plan's isConfigured set to
//     applySections over-manages (PUTs) those hydrated sections and can fail
//     closed on one that has disappeared since import.
//
//   - Finding 5: mgmt's blankSSHKeyControllerMetadata must run only when
//     ssh_keys is actually being replaced (model.SSHKeys non-null/known) —
//     not unconditionally — so omitted ssh_keys preserve the snapshot's
//     controller-assigned date/fingerprint metadata (legacy parity).

// ---------------------------------------------------------------------------
// Finding 4: configuredSections(config) scopes applySections to only the
// sections present in CONFIG, even when plan carries many more (hydrated)
// sections.
// ---------------------------------------------------------------------------

// TestConfiguredSections_FiltersToConfigOnly proves the configuredSections
// helper returns exactly the sections non-null/known in the given model,
// independent of what else might be populated elsewhere (plan).
func TestConfiguredSections_FiltersToConfigOnly(t *testing.T) {
	ctx := context.Background()

	config := allSectionsNullModel()
	config.Mgmt = mgmtObjectForFixC(t, ctx, true)

	got := configuredSections(config)
	if len(got) != 1 || got[0].key() != "mgmt" {
		t.Fatalf("configuredSections(config) = %v, want exactly [mgmt]", sectionKeys(got))
	}
}

// TestApplySections_UsesConfigFilteredSet_NotPlan is the TDD regression test
// for Finding 4: it simulates the post-import shape directly — a PLAN with
// many sections hydrated (non-null, as UseStateForUnknown would produce) but
// a CONFIG where only mgmt is set. Passing configuredSections(config) (NOT
// plan) to applySections must PUT ONLY mgmt; the hydrated dpi/country/radius
// in plan must NOT be checked or written, even though radius is entirely
// ABSENT from the controller snapshot (which would fail closed if it were
// included).
func TestApplySections_UsesConfigFilteredSet_NotPlan(t *testing.T) {
	ctx := context.Background()
	client := newFakeSettingsClient()
	// Only mgmt is present/supported on this controller. dpi/country ARE
	// supported (seeded) to prove they're skipped for being unconfigured
	// (not merely absent); radius is absent entirely, which would fail
	// closed if the engine mistakenly tried to manage it.
	client.sections["mgmt"] = rawSection("mgmt", map[string]any{})
	client.sections["dpi"] = rawSection("dpi", map[string]any{"enabled": false, "fingerprintingEnabled": false})
	client.sections["country"] = rawSection("country", map[string]any{"code": float64(0)})

	prior := allSectionsNullModel()

	// PLAN: mirrors post-import UseStateForUnknown — mgmt (the section the
	// user is actually changing) PLUS dpi/country/radius hydrated as
	// Computed-from-state, none of which the user configured.
	plan := allSectionsNullModel()
	plan.Mgmt = mgmtObjectForFixC(t, ctx, true)
	plan.Dpi = dpiObject(t, ctx, true, false)
	plan.Country = countryObject(t, ctx, 840)
	plan.Radius = radiusObject(t, ctx, types.StringValue("s3cr3t"))

	// CONFIG: only mgmt — what the user actually wrote in HCL.
	config := allSectionsNullModel()
	config.Mgmt = mgmtObjectForFixC(t, ctx, true)

	configured := configuredSections(config)
	if len(configured) != 1 || configured[0].key() != "mgmt" {
		t.Fatalf("configuredSections(config) = %v, want exactly [mgmt]", sectionKeys(configured))
	}

	out, diags := applySections(ctx, configured, client, "default", plan, prior)
	if diags.HasError() {
		t.Fatalf("applySections with config-filtered [mgmt] must succeed (radius absence must not block), got: %v", diags)
	}

	if len(client.puts) != 1 {
		t.Fatalf("puts = %d, want exactly 1 (mgmt only): %+v", len(client.puts), client.puts)
	}
	if client.puts[0].Key != "mgmt" {
		t.Fatalf("puts[0].Key = %q, want %q", client.puts[0].Key, "mgmt")
	}

	// The decisive assertion is the PUT count/keys above: dpi/country/radius
	// were hydrated in plan (as UseStateForUnknown would do post-import) but
	// absent from config, so applySections must never have checked their
	// presence or overlaid/PUT them — radius in particular is entirely
	// absent from the fake controller, which would fail closed had it been
	// included in the config-filtered set. The single mgmt PUT above already
	// proves that. out itself starts from plan
	// (applySections' `out := plan`) and only the sections actually passed
	// (mgmt) get freshly overwritten by the post-apply read — so out.Dpi/
	// out.Radius simply passing through plan's hydrated values here is
	// expected and does not indicate they were managed.
	if out.Mgmt.IsNull() {
		t.Error("out.Mgmt is null after a successful apply, want populated")
	}
}

// mgmtObjectForFixC builds a minimal configured mgmt types.Object (SSHKeys
// left null) for Finding 4's plan/config fixtures.
func mgmtObjectForFixC(t *testing.T, ctx context.Context, autoUpgrade bool) types.Object {
	t.Helper()
	m := settingMgmtModel{
		AutoUpgrade: types.BoolValue(autoUpgrade),
		SSHKeys:     types.ListNull(types.ObjectType{AttrTypes: mgmtSSHKeyAttrTypes}),
	}
	obj, diags := types.ObjectValueFrom(ctx, mgmtAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building mgmt object: %v", diags)
	}
	return obj
}

// ---------------------------------------------------------------------------
// Finding 5: blankSSHKeyControllerMetadata must not run when ssh_keys is
// omitted (null/unknown) in the model — the snapshot's existing keyed
// metadata (date/fingerprint) must be preserved verbatim.
// ---------------------------------------------------------------------------

// TestMgmtSection_OmittedSshKeysPreservesControllerMetadata is the TDD
// regression test for Finding 5: the base snapshot carries a keyed
// x_ssh_keys entry with real controller date/fingerprint. The model sets
// some OTHER mgmt field but leaves SSHKeys null (omitted, matching a plan
// where the user never wrote an ssh_keys block). overlayObjectList correctly
// leaves base's x_ssh_keys untouched when the model's list is null/unknown
// — but blankSSHKeyControllerMetadata used to run unconditionally afterward
// and zero the metadata anyway. It must now be preserved.
func TestMgmtSection_OmittedSshKeysPreservesControllerMetadata(t *testing.T) {
	ctx := context.Background()
	sec := mgmtSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "mgmt"},
		Data: map[string]any{
			"x_ssh_keys": []any{
				map[string]any{
					"name":        "existing",
					"type":        "ssh-ed25519",
					"key":         "KA",
					"comment":     "existing key",
					"date":        "D",
					"fingerprint": "F",
				},
			},
		},
	}})

	m := settingMgmtModel{
		AutoUpgrade: types.BoolValue(true), // some other mgmt field is set
		SSHKeys:     types.ListNull(types.ObjectType{AttrTypes: mgmtSSHKeyAttrTypes}),
	}
	obj, objDiags := types.ObjectValueFrom(ctx, mgmtAttrTypes, m)
	if objDiags.HasError() {
		t.Fatalf("building mgmt object: %v", objDiags)
	}

	model := settingResourceModel{Mgmt: obj}
	prior := settingResourceModel{}

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}

	rawKeys, ok := rs.Data["x_ssh_keys"].([]any)
	if !ok || len(rawKeys) != 1 {
		t.Fatalf("rs.Data[%q] = %v, want 1-element []any", "x_ssh_keys", rs.Data["x_ssh_keys"])
	}
	elem, ok := rawKeys[0].(map[string]any)
	if !ok {
		t.Fatalf("x_ssh_keys[0] = %v, want map[string]any", rawKeys[0])
	}

	if elem["key"] != "KA" {
		t.Fatalf("x_ssh_keys[0][key] = %v, want %q (base preserved verbatim when ssh_keys omitted)", elem["key"], "KA")
	}
	if elem["date"] != "D" {
		t.Errorf("x_ssh_keys[0][date] = %v, want %q (PRESERVED: ssh_keys omitted, not being replaced)", elem["date"], "D")
	}
	if elem["fingerprint"] != "F" {
		t.Errorf("x_ssh_keys[0][fingerprint] = %v, want %q (PRESERVED: ssh_keys omitted, not being replaced)", elem["fingerprint"], "F")
	}
}
