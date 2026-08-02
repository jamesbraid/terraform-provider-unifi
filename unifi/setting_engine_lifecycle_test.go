package unifi

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// setting_engine_lifecycle_test.go is the end-to-end integration suite (plan
// §8): it drives listSnapshot/readSections/applySections through the REAL 13
// registered sections (orderedSections(settingSections)) against the
// in-memory fakeSettingsClient, proving the whole engine works together
// rather than just its mechanics (setting_engine_test.go, stub sections) or
// each section in isolation (setting_section_*_test.go). No production code
// changes; this file is test-only.

// ---------------------------------------------------------------------------
// Helpers: build configured section objects for the real settingResourceModel
// fields, mirroring each section's own _test.go fixtures.
// ---------------------------------------------------------------------------

// allSectionsNullModel returns a settingResourceModel with every registered
// section's object field explicitly null (nothing configured) — the base
// starting point for building a partially-configured model in these tests.
func allSectionsNullModel() settingResourceModel {
	return settingResourceModel{
		AutoSpeedtest: types.ObjectNull(autoSpeedtestAttrTypes),
		Country:       types.ObjectNull(countryAttrTypes),
		Dpi:           types.ObjectNull(dpiAttrTypes),
		Lcm:           types.ObjectNull(lcmAttrTypes),
		NetworkOpt:    types.ObjectNull(networkOptimizationAttrTypes),
		Ntp:           types.ObjectNull(ntpAttrTypes),
		Syslog:        types.ObjectNull(syslogAttrTypes),
		Doh:           types.ObjectNull(dohAttrTypes),
		Ips:           types.ObjectNull(ipsAttrTypes),
		Mgmt:          types.ObjectNull(mgmtAttrTypes),
		Radius:        types.ObjectNull(radiusAttrTypes),
		USG:           types.ObjectNull(usgAttrTypes),
		IgmpSnooping:  types.ObjectNull(igmpSnoopingAttrTypes),
	}
}

// dpiObject builds a configured dpi types.Object.
func dpiObject(t *testing.T, ctx context.Context, enabled, fingerprinting bool) types.Object {
	t.Helper()
	obj, diags := types.ObjectValueFrom(ctx, dpiAttrTypes, settingDpiModel{
		Enabled:               types.BoolValue(enabled),
		FingerprintingEnabled: types.BoolValue(fingerprinting),
	})
	if diags.HasError() {
		t.Fatalf("building dpi object: %v", diags)
	}
	return obj
}

// countryObject builds a configured country types.Object.
func countryObject(t *testing.T, ctx context.Context, code int64) types.Object {
	t.Helper()
	obj, diags := types.ObjectValueFrom(ctx, countryAttrTypes, settingCountryModel{
		Code: types.Int64Value(code),
	})
	if diags.HasError() {
		t.Fatalf("building country object: %v", diags)
	}
	return obj
}

// syslogObject builds a configured syslog types.Object with the enabled and
// contents leaves set; every other leaf null.
func syslogObject(t *testing.T, ctx context.Context, enabled bool, contents []string) types.Object {
	t.Helper()
	var contentsList types.List
	if contents == nil {
		contentsList = types.ListNull(types.StringType)
	} else {
		l, diags := types.ListValueFrom(ctx, types.StringType, contents)
		if diags.HasError() {
			t.Fatalf("building syslog contents list: %v", diags)
		}
		contentsList = l
	}
	obj, diags := types.ObjectValueFrom(ctx, syslogAttrTypes, settingSyslogModel{
		Enabled:                     types.BoolValue(enabled),
		Contents:                    contentsList,
		Debug:                       types.BoolNull(),
		IP:                          types.StringNull(),
		Port:                        types.Int64Null(),
		LogAllContents:              types.BoolNull(),
		NetconsoleEnabled:           types.BoolNull(),
		NetconsoleHost:              types.StringNull(),
		NetconsolePort:              types.Int64Null(),
		ThisController:              types.BoolNull(),
		ThisControllerEncryptedOnly: types.BoolNull(),
	})
	if diags.HasError() {
		t.Fatalf("building syslog object: %v", diags)
	}
	return obj
}

// ipsObject builds a configured ips types.Object with ips_mode set and every
// list leaf null (so overlay produces no suppression wrapper unless the
// snapshot already carries one).
func ipsObject(t *testing.T, ctx context.Context, ipsMode string) types.Object {
	t.Helper()
	obj, diags := types.ObjectValueFrom(ctx, ipsAttrTypes, settingIpsModel{
		IPSMode:                             types.StringValue(ipsMode),
		AdvancedFilteringPreference:         types.StringNull(),
		ContentFilteringBlockingPageEnabled: types.BoolNull(),
		EnabledCategories:                   types.ListNull(types.StringType),
		EnabledNetworks:                     types.ListNull(types.StringType),
		Honeypot:                            types.ListNull(types.ObjectType{AttrTypes: ipsHoneypotAttrTypes}),
		HoneypotEnabled:                     types.BoolNull(),
		MemoryOptimized:                     types.BoolNull(),
		RestrictTorrents:                    types.BoolNull(),
		SuppressionWhitelist:                types.ListNull(types.ObjectType{AttrTypes: ipsWhitelistAttrTypes}),
		SuppressionAlerts:                   types.ListNull(types.ObjectType{AttrTypes: ipsAlertAttrTypes}),
	})
	if diags.HasError() {
		t.Fatalf("building ips object: %v", diags)
	}
	return obj
}

// radiusObject builds a configured radius types.Object with accounting +
// secret set; auth_port/interim_update_interval left null.
func radiusObject(t *testing.T, ctx context.Context, secret types.String) types.Object {
	t.Helper()
	obj, diags := types.ObjectValueFrom(ctx, radiusAttrTypes, settingRadiusModel{
		AccountingEnabled:     types.BoolValue(true),
		AcctPort:              types.Int64Value(1813),
		AuthPort:              types.Int64Null(),
		InterimUpdateInterval: timetypes.NewGoDurationNull(),
		Secret:                secret,
	})
	if diags.HasError() {
		t.Fatalf("building radius object: %v", diags)
	}
	return obj
}

// rawSection is a small constructor for seeding fakeSettingsClient.sections.
func rawSection(key string, data map[string]any) settings.RawSetting {
	if data == nil {
		data = map[string]any{}
	}
	return settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: key},
		Data:        data,
	}
}

// realSections is the full REAL registry in deterministic order, used as the
// `sections` argument across this suite per the brief.
func realSections() []settingSection {
	return orderedSections(settingSections)
}

// ---------------------------------------------------------------------------
// 1. TestLifecycle_multipleSectionsOneOp
// ---------------------------------------------------------------------------

func TestLifecycle_multipleSectionsOneOp(t *testing.T) {
	ctx := context.Background()
	client := newFakeSettingsClient()
	// Model a controller that supports these sections: ListSettings returns
	// their default materialized rows before the provider updates them (the
	// presence preflight in applySections fails closed on a configured
	// section absent from the snapshot, so every section this test
	// configures must be seeded here).
	client.sections["dpi"] = rawSection("dpi", map[string]any{"enabled": false, "fingerprintingEnabled": false})
	client.sections["country"] = rawSection("country", map[string]any{"code": float64(0)})
	client.sections["rsyslogd"] = rawSection("rsyslogd", map[string]any{"enabled": false})
	client.sections["ips"] = rawSection("ips", map[string]any{"ips_mode": "disabled"})
	client.sections["radius"] = rawSection("radius", map[string]any{"accounting_enabled": false})

	plan := allSectionsNullModel()
	plan.Dpi = dpiObject(t, ctx, true, false)
	plan.Country = countryObject(t, ctx, 840)
	plan.Syslog = syslogObject(t, ctx, true, []string{"device", "client"})
	plan.Ips = ipsObject(t, ctx, "ips")
	plan.Radius = radiusObject(t, ctx, types.StringValue("s3cr3t"))

	prior := allSectionsNullModel()

	out, diags := applySections(ctx, realSections(), client, "default", plan, prior)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	wantPut := map[string]bool{"dpi": true, "country": true, "rsyslogd": true, "ips": true, "radius": true}
	if len(client.puts) != len(wantPut) {
		t.Fatalf("puts = %d, want %d: %+v", len(client.puts), len(wantPut), client.puts)
	}
	seen := map[string]bool{}
	for _, p := range client.puts {
		seen[p.Key] = true
		if !wantPut[p.Key] {
			t.Errorf("unexpected PUT for key %q (not configured)", p.Key)
		}
	}
	for k := range wantPut {
		if !seen[k] {
			t.Errorf("missing PUT for configured section key %q", k)
		}
	}

	if out.Dpi.IsNull() {
		t.Error("returned model: Dpi is null, want populated")
	}
	if out.Country.IsNull() {
		t.Error("returned model: Country is null, want populated")
	}
	if out.Syslog.IsNull() {
		t.Error("returned model: Syslog is null, want populated")
	}
	if out.Ips.IsNull() {
		t.Error("returned model: Ips is null, want populated")
	}
	if out.Radius.IsNull() {
		t.Error("returned model: Radius is null, want populated")
	}
	// Unconfigured sections must remain null/absent in the returned model
	// (never PUT and never spuriously hydrated).
	if !out.Mgmt.IsNull() {
		t.Error("returned model: Mgmt should remain null (not configured)")
	}
}

// ---------------------------------------------------------------------------
// 2. TestLifecycle_onlyConfiguredWritten
// ---------------------------------------------------------------------------

func TestLifecycle_onlyConfiguredWritten(t *testing.T) {
	ctx := context.Background()
	client := newFakeSettingsClient()
	// Model a controller that supports dpi: ListSettings returns its default
	// materialized row before the provider updates it (the presence
	// preflight in applySections fails closed on a configured section absent
	// from the snapshot).
	client.sections["dpi"] = rawSection("dpi", map[string]any{"enabled": false, "fingerprintingEnabled": false})

	plan := allSectionsNullModel()
	plan.Dpi = dpiObject(t, ctx, true, true)
	prior := allSectionsNullModel()

	_, diags := applySections(ctx, realSections(), client, "default", plan, prior)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	if len(client.puts) != 1 {
		t.Fatalf("puts = %d, want exactly 1: %+v", len(client.puts), client.puts)
	}
	if client.puts[0].Key != "dpi" {
		t.Errorf("puts[0].Key = %q, want %q", client.puts[0].Key, "dpi")
	}
}

// ---------------------------------------------------------------------------
// 3. TestLifecycle_failBeforeFirstWrite
// ---------------------------------------------------------------------------

func TestLifecycle_failBeforeFirstWrite(t *testing.T) {
	ctx := context.Background()
	client := newFakeSettingsClient()
	client.failList = errors.New("injected list failure")

	plan := allSectionsNullModel()
	plan.Dpi = dpiObject(t, ctx, true, false)
	plan.Country = countryObject(t, ctx, 840)
	prior := allSectionsNullModel()

	_, diags := applySections(ctx, realSections(), client, "default", plan, prior)
	if !diags.HasError() {
		t.Fatalf("expected error diagnostics from failed initial snapshot, got none")
	}
	if len(client.puts) != 0 {
		t.Fatalf("puts = %d, want 0 (reconcile-before-mutate: snapshot read fails before any write)", len(client.puts))
	}
}

// ---------------------------------------------------------------------------
// 4. TestLifecycle_failAfterPartialWrite_retryConverges
// ---------------------------------------------------------------------------

func TestLifecycle_failAfterPartialWrite_retryConverges(t *testing.T) {
	ctx := context.Background()
	client := newFakeSettingsClient()
	// Both sections already exist on the controller (pre-apply state), so
	// dpi's OLD value is still readable after its PUT fails and leaves the
	// fake's stored section untouched — mirroring
	// TestEngine_partialApplyReReads' fixture shape for the real sections.
	client.sections["country"] = rawSection("country", map[string]any{"code": float64(840)})
	client.sections["dpi"] = rawSection("dpi", map[string]any{"enabled": false, "fingerprintingEnabled": false})
	// PUT order is deterministic (orderedSections sorts by key()), and
	// "country" < "dpi" alphabetically, so country is written first, dpi
	// second — failUpdateOn["dpi"] fails the SECOND write.
	client.failUpdateOn["dpi"] = errors.New("injected dpi update failure")

	plan := allSectionsNullModel()
	plan.Country = countryObject(t, ctx, 826)
	plan.Dpi = dpiObject(t, ctx, true, false)
	prior := allSectionsNullModel()
	prior.Country = countryObject(t, ctx, 840)
	prior.Dpi = dpiObject(t, ctx, false, false)

	_, diags1 := applySections(ctx, realSections(), client, "default", plan, prior)
	if !diags1.HasError() {
		t.Fatalf("first apply: expected error diagnostics from failed dpi PUT, got none")
	}
	if len(client.puts) != 2 {
		t.Fatalf("first apply: puts = %d, want 2 (country succeeded, dpi attempted+failed): %+v", len(client.puts), client.puts)
	}
	if client.puts[0].Key != "country" || client.puts[1].Key != "dpi" {
		t.Fatalf("first apply: put order = [%q, %q], want [country, dpi]", client.puts[0].Key, client.puts[1].Key)
	}

	// Confirm the controller's canonical state after the partial apply:
	// country (successful PUT) reflects the new plan value; dpi (attempted
	// but failed) is unchanged on the controller — still the old value, not
	// silently advanced to the plan's value.
	var after settingResourceModel
	rdiags := readSections(ctx, realSections(), client, "default", prior, &after, false)
	if rdiags.HasError() {
		t.Fatalf("re-reading after partial apply: unexpected diagnostics: %v", rdiags)
	}
	var gotCountry settingCountryModel
	if d := after.Country.As(ctx, &gotCountry, basetypes.ObjectAsOptions{}); d.HasError() {
		t.Fatalf("extracting country: %v", d)
	}
	if gotCountry.Code.ValueInt64() != 826 {
		t.Errorf("after partial apply, Country.Code = %d, want 826 (successful PUT)", gotCountry.Code.ValueInt64())
	}
	var gotDpi settingDpiModel
	if d := after.Dpi.As(ctx, &gotDpi, basetypes.ObjectAsOptions{}); d.HasError() {
		t.Fatalf("extracting dpi: %v", d)
	}
	if gotDpi.Enabled.ValueBool() {
		t.Errorf("after partial apply, Dpi.Enabled = %v, want false (failed PUT: controller value unchanged)", gotDpi.Enabled)
	}

	// Retry: clear the injected failure and re-apply with the SAME plan,
	// using the re-read canonical state as prior (what a real provider Update
	// would do on a subsequent apply).
	delete(client.failUpdateOn, "dpi")
	out2, diags2 := applySections(ctx, realSections(), client, "default", plan, after)
	if diags2.HasError() {
		t.Fatalf("retry apply: unexpected diagnostics: %v", diags2)
	}
	if len(client.puts) != 4 {
		t.Fatalf("across both applies: puts = %d, want 4 (2 from first apply + 2 from retry): %+v", len(client.puts), client.puts)
	}
	if client.puts[2].Key != "country" || client.puts[3].Key != "dpi" {
		t.Fatalf("retry put order = [%q, %q], want [country, dpi]", client.puts[2].Key, client.puts[3].Key)
	}
	var gotDpi2 settingDpiModel
	if d := out2.Dpi.As(ctx, &gotDpi2, basetypes.ObjectAsOptions{}); d.HasError() {
		t.Fatalf("extracting dpi after retry: %v", d)
	}
	if !gotDpi2.Enabled.ValueBool() {
		t.Errorf("retry Dpi.Enabled = %v, want true (converged)", gotDpi2.Enabled)
	}
	var gotCountry2 settingCountryModel
	if d := out2.Country.As(ctx, &gotCountry2, basetypes.ObjectAsOptions{}); d.HasError() {
		t.Fatalf("extracting country after retry: %v", d)
	}
	if gotCountry2.Code.ValueInt64() != 826 {
		t.Errorf("retry Country.Code = %d, want 826 (still converged)", gotCountry2.Code.ValueInt64())
	}
}

// ---------------------------------------------------------------------------
// 5. TestLifecycle_universalPreservation
// ---------------------------------------------------------------------------

func TestLifecycle_universalPreservation(t *testing.T) {
	ctx := context.Background()
	client := newFakeSettingsClient()
	client.sections["dpi"] = rawSection("dpi", map[string]any{
		"enabled":               false,
		"fingerprintingEnabled": false,
		"x_unmanaged":           "keep",
	})

	plan := allSectionsNullModel()
	plan.Dpi = dpiObject(t, ctx, true, false)
	prior := allSectionsNullModel()

	_, diags := applySections(ctx, realSections(), client, "default", plan, prior)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	if len(client.puts) != 1 {
		t.Fatalf("puts = %d, want 1: %+v", len(client.puts), client.puts)
	}
	put := client.puts[0]
	if put.Key != "dpi" {
		t.Fatalf("puts[0].Key = %q, want dpi", put.Key)
	}
	if got := put.Data["x_unmanaged"]; got != "keep" {
		t.Errorf("PUT Data[x_unmanaged] = %v, want keep (unmodeled key must survive end-to-end RMW)", got)
	}
}

// ---------------------------------------------------------------------------
// 6. TestLifecycle_emptyVsAbsent
// ---------------------------------------------------------------------------

func TestLifecycle_emptyVsAbsent(t *testing.T) {
	ctx := context.Background()

	t.Run("explicit empty string decodes to StringValue empty", func(t *testing.T) {
		client := newFakeSettingsClient()
		client.sections["ntp"] = rawSection("ntp", map[string]any{"ntp_server_1": ""})

		prior := allSectionsNullModel()
		var model settingResourceModel
		diags := readSections(ctx, realSections(), client, "default", prior, &model, false)
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		var got settingNtpModel
		if d := model.Ntp.As(ctx, &got, basetypes.ObjectAsOptions{}); d.HasError() {
			t.Fatalf("extracting ntp: %v", d)
		}
		if got.NtpServer1.IsNull() {
			t.Fatalf("NtpServer1 = null, want StringValue(\"\") for an explicit empty string on the wire")
		}
		if got.NtpServer1.ValueString() != "" {
			t.Errorf("NtpServer1 = %q, want empty string", got.NtpServer1.ValueString())
		}
	})

	t.Run("absent key decodes to null", func(t *testing.T) {
		client := newFakeSettingsClient()
		client.sections["ntp"] = rawSection("ntp", map[string]any{})

		prior := allSectionsNullModel()
		var model settingResourceModel
		diags := readSections(ctx, realSections(), client, "default", prior, &model, false)
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		var got settingNtpModel
		if d := model.Ntp.As(ctx, &got, basetypes.ObjectAsOptions{}); d.HasError() {
			t.Fatalf("extracting ntp: %v", d)
		}
		if !got.NtpServer1.IsNull() {
			t.Errorf("NtpServer1 = %q, want null for an absent wire key", got.NtpServer1.ValueString())
		}
	})
}

// ---------------------------------------------------------------------------
// 7. TestLifecycle_explicitClearVsStopManaging
// ---------------------------------------------------------------------------

func TestLifecycle_explicitClearVsStopManaging(t *testing.T) {
	ctx := context.Background()

	t.Run("null config stops managing: no PUT, controller value untouched", func(t *testing.T) {
		client := newFakeSettingsClient()
		client.sections["rsyslogd"] = rawSection("rsyslogd", map[string]any{
			"enabled":  true,
			"contents": []any{"device", "client"},
		})
		before := client.sections["rsyslogd"]

		plan := allSectionsNullModel() // syslog left null: stop managing
		prior := allSectionsNullModel()

		_, diags := applySections(ctx, realSections(), client, "default", plan, prior)
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		for _, p := range client.puts {
			if p.Key == "rsyslogd" {
				t.Fatalf("unexpected PUT for rsyslogd when syslog is not configured (null): %+v", p)
			}
		}
		after := client.sections["rsyslogd"]
		if after.Data["enabled"] != before.Data["enabled"] {
			t.Errorf("controller section mutated despite null config: enabled = %v, want unchanged %v", after.Data["enabled"], before.Data["enabled"])
		}
	})

	t.Run("explicit-clear config emits the cleared value", func(t *testing.T) {
		client := newFakeSettingsClient()
		client.sections["rsyslogd"] = rawSection("rsyslogd", map[string]any{
			"enabled":  true,
			"contents": []any{"device", "client"},
		})

		plan := allSectionsNullModel()
		// Explicitly configure syslog with an empty contents list (a
		// configured-but-empty list, not an unconfigured null).
		plan.Syslog = syslogObject(t, ctx, true, []string{})
		prior := allSectionsNullModel()
		prior.Syslog = syslogObject(t, ctx, true, []string{"device", "client"})

		_, diags := applySections(ctx, realSections(), client, "default", plan, prior)
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		var put *settings.RawSetting
		for i := range client.puts {
			if client.puts[i].Key == "rsyslogd" {
				put = &client.puts[i]
			}
		}
		if put == nil {
			t.Fatalf("expected a PUT for rsyslogd (explicit-clear config), got none")
		}
		contents, ok := put.Data["contents"].([]any)
		if !ok {
			t.Fatalf("PUT Data[contents] = %v (%T), want an empty []any", put.Data["contents"], put.Data["contents"])
		}
		if len(contents) != 0 {
			t.Errorf("PUT Data[contents] = %v, want empty list (explicit clear)", contents)
		}
	})
}

// ---------------------------------------------------------------------------
// 8. TestLifecycle_malformedRemoteTolerated
// ---------------------------------------------------------------------------

// TestLifecycle_malformedRemoteTolerated supersedes the old
// TestLifecycle_malformedRemoteAborts (Task 1: remote type drift is now
// tolerated at read time, not a hard failure). A malformed remote value
// (string where dpi.enabled expects a bool) is remote type drift: readSections
// and applySections both now WARN and retain the field's prior typed value —
// they never abort with an error diagnostic, and refresh/apply keep
// succeeding through a single drifted field. A subsequent apply with a VALID
// configured value for that same leaf still overwrites the malformed prior
// remote (overlay always writes the plan's own valid value for a
// plan-managed leaf, independent of decode).
func TestLifecycle_malformedRemoteTolerated(t *testing.T) {
	ctx := context.Background()

	plan := allSectionsNullModel()
	plan.Dpi = dpiObject(t, ctx, true, false)
	prior := allSectionsNullModel()
	prior.Dpi = dpiObject(t, ctx, false, false) // prior typed value the field must retain on drift

	malformedClient := func() *fakeSettingsClient {
		c := newFakeSettingsClient()
		c.sections["dpi"] = rawSection("dpi", map[string]any{
			"enabled":               "not-a-bool", // malformed: string where a bool is expected
			"fingerprintingEnabled": false,
		})
		return c
	}

	t.Run("readSections warns and retains prior on a malformed remote field, refresh still succeeds", func(t *testing.T) {
		// onlyConfigured=false: matches a refresh/import pass over the full
		// registry (malformedClient() only seeds dpi, so every other
		// section is legitimately unsupported-and-skipped here, not
		// configured-but-missing).
		model := prior
		diags := readSections(ctx, realSections(), malformedClient(), "default", prior, &model, false)
		if diags.HasError() {
			t.Fatalf("malformed remote dpi.enabled must not be an error, got: %v", diags)
		}
		if !hasWarning(diags) {
			t.Fatalf("malformed remote dpi.enabled must produce a warning, got: %v", diags)
		}
		var got settingDpiModel
		if d := model.Dpi.As(ctx, &got, basetypes.ObjectAsOptions{}); d.HasError() {
			t.Fatalf("extracting dpi: %v", d)
		}
		if got.Enabled.ValueBool() {
			t.Errorf("Dpi.Enabled = %v, want false (prior's typed value retained through the drifted leaf)", got.Enabled)
		}
	})

	t.Run("applySections: valid configured value still overwrites a malformed prior remote", func(t *testing.T) {
		out, diags := applySections(ctx, realSections(), malformedClient(), "default", plan, prior)
		if diags.HasError() {
			t.Fatalf("unexpected error diagnostics: %v", diags)
		}
		var got settingDpiModel
		if d := out.Dpi.As(ctx, &got, basetypes.ObjectAsOptions{}); d.HasError() {
			t.Fatalf("extracting dpi: %v", d)
		}
		if !got.Enabled.ValueBool() {
			t.Errorf("Dpi.Enabled = %v, want true (plan's valid value clobbered the malformed remote value)", got.Enabled)
		}
	})

	t.Run("applySections: post-apply re-read surfaces a type-drift warning (not silently dropped) when the malformed section is left unconfigured", func(t *testing.T) {
		planWithoutDpi := allSectionsNullModel()
		planWithoutDpi.Country = countryObject(t, ctx, 840) // configure something else this apply

		// This subtest's client also models a controller that supports
		// country (materialized row), since the presence preflight in
		// applySections fails closed on a configured section absent from
		// the snapshot; malformedClient() alone only seeds dpi.
		client := malformedClient()
		client.sections["country"] = rawSection("country", map[string]any{"code": float64(0)})

		out, diags := applySections(ctx, realSections(), client, "default", planWithoutDpi, prior)
		if diags.HasError() {
			t.Fatalf("unexpected ERROR diagnostics (expected a warning, not an error): %v", diags)
		}
		if !hasWarning(diags) {
			t.Fatalf("expected a type-drift warning surfaced from the post-apply re-read (setting_engine.go's applySections else branch), got: %v", diags)
		}
		var got settingDpiModel
		if d := out.Dpi.As(ctx, &got, basetypes.ObjectAsOptions{}); d.HasError() {
			t.Fatalf("extracting dpi: %v", d)
		}
		if got.Enabled.ValueBool() {
			t.Errorf("Dpi.Enabled = %v, want false (prior's typed value retained: dpi was never configured this apply, and its remote leaf is drifted)", got.Enabled)
		}

		var driftWarning diag.Diagnostic
		for _, dg := range diags {
			if dg.Severity() == diag.SeverityWarning && dg.Summary() == "Settings value type drift" {
				driftWarning = dg
				break
			}
		}
		if driftWarning == nil {
			t.Fatalf("expected a %q warning diagnostic, got: %v", "Settings value type drift", diags)
		}
		detail := driftWarning.Detail()
		if !strings.Contains(detail, `field "enabled"`) || !strings.Contains(detail, "expected bool") {
			t.Errorf("warning detail = %q, want it to name the offending dpi.enabled decode drift", detail)
		}
	})
}

// ---------------------------------------------------------------------------
// 9. TestLifecycle_writeOnlySecretNeverCleared
// ---------------------------------------------------------------------------

func TestLifecycle_writeOnlySecretNeverCleared(t *testing.T) {
	ctx := context.Background()

	t.Run("refresh: model secret comes from prior, not the mask", func(t *testing.T) {
		client := newFakeSettingsClient()
		client.sections["radius"] = rawSection("radius", map[string]any{
			"accounting_enabled": true,
			"acct_port":          float64(1813),
			"x_secret":           "******",
		})

		prior := allSectionsNullModel()
		prior.Radius = radiusObject(t, ctx, types.StringValue("real-secret"))

		var model settingResourceModel
		diags := readSections(ctx, realSections(), client, "default", prior, &model, false)
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		var got settingRadiusModel
		if d := model.Radius.As(ctx, &got, basetypes.ObjectAsOptions{}); d.HasError() {
			t.Fatalf("extracting radius: %v", d)
		}
		if got.Secret.ValueString() != "real-secret" {
			t.Errorf("Secret = %q, want %q (prior) — masked x_secret must not leak", got.Secret.ValueString(), "real-secret")
		}
	})

	t.Run("failed mutation: best-effort state still carries prior/plan secret, never cleared", func(t *testing.T) {
		client := newFakeSettingsClient()
		client.sections["radius"] = rawSection("radius", map[string]any{
			"accounting_enabled": false,
			"x_secret":           "******",
		})
		client.failUpdateOn["radius"] = errors.New("injected radius update failure")

		prior := allSectionsNullModel()
		prior.Radius = radiusObject(t, ctx, types.StringValue("prior-secret"))
		plan := allSectionsNullModel()
		plan.Radius = radiusObject(t, ctx, types.StringValue("rotated-secret"))

		out, diags := applySections(ctx, realSections(), client, "default", plan, prior)
		if !diags.HasError() {
			t.Fatalf("expected error diagnostics from failed radius PUT, got none")
		}
		var got settingRadiusModel
		if d := out.Radius.As(ctx, &got, basetypes.ObjectAsOptions{}); d.HasError() {
			t.Fatalf("extracting radius from best-effort state: %v", d)
		}
		if got.Secret.IsNull() || got.Secret.ValueString() == "" {
			t.Fatalf("Secret = %v, want a non-empty carried-forward value (never cleared/nulled)", got.Secret)
		}
		if got.Secret.ValueString() != "rotated-secret" {
			t.Errorf("Secret = %q, want %q (plan's rotated secret carried best-effort since radius was attempted this apply)", got.Secret.ValueString(), "rotated-secret")
		}
	})

	t.Run("null-secret config apply deletes x_secret, mask never re-sent", func(t *testing.T) {
		client := newFakeSettingsClient()
		client.sections["radius"] = rawSection("radius", map[string]any{
			"accounting_enabled": true,
			"x_secret":           "******",
		})

		prior := allSectionsNullModel()
		prior.Radius = radiusObject(t, ctx, types.StringValue("prior-secret"))
		plan := allSectionsNullModel()
		plan.Radius = radiusObject(t, ctx, types.StringNull()) // null secret: not being changed

		_, diags := applySections(ctx, realSections(), client, "default", plan, prior)
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		var put *settings.RawSetting
		for i := range client.puts {
			if client.puts[i].Key == "radius" {
				put = &client.puts[i]
			}
		}
		if put == nil {
			t.Fatalf("expected a PUT for radius, got none")
		}
		if _, ok := put.Data["x_secret"]; ok {
			t.Errorf("PUT Data[x_secret] = %v, want key deleted (mask must never be re-sent)", put.Data["x_secret"])
		}
	})
}

// ---------------------------------------------------------------------------
// 10. TestLifecycle_importHydratesAll_thenCleanRePlan
// ---------------------------------------------------------------------------

func TestLifecycle_importHydratesAll_thenCleanRePlan(t *testing.T) {
	ctx := context.Background()
	client := newFakeSettingsClient()
	client.sections["dpi"] = rawSection("dpi", map[string]any{
		"enabled":               true,
		"fingerprintingEnabled": false,
	})
	client.sections["country"] = rawSection("country", map[string]any{"code": float64(840)})
	client.sections["rsyslogd"] = rawSection("rsyslogd", map[string]any{
		"enabled":  true,
		"contents": []any{"device"},
	})

	// Import shape: prior has every section attr null.
	importPrior := allSectionsNullModel()

	var hydrated settingResourceModel
	diags := readSections(ctx, realSections(), client, "default", importPrior, &hydrated, false)
	if diags.HasError() {
		t.Fatalf("import hydration: unexpected diagnostics: %v", diags)
	}

	if hydrated.Dpi.IsNull() {
		t.Error("hydrated.Dpi is null, want populated from the fake's seeded dpi section")
	}
	if hydrated.Country.IsNull() {
		t.Error("hydrated.Country is null, want populated from the fake's seeded country section")
	}
	if hydrated.Syslog.IsNull() {
		t.Error("hydrated.Syslog is null, want populated from the fake's seeded rsyslogd section")
	}
	// A section with NO fake entry at all must remain null even on a
	// best-effort (onlyConfigured=false) hydration pass, since a key absent
	// from the snapshot is skipped rather than decoded.
	if !hydrated.Radius.IsNull() {
		t.Error("hydrated.Radius should remain null: no fake entry seeded for radius")
	}

	// Second pass: re-read using the hydrated model as prior, over an
	// UNCHANGED fake, with onlyConfigured=true (as a real plan/refresh would
	// do once every section has a known prior value). No drift expected: the
	// re-read must reproduce the same values, so a subsequent plan is empty.
	//
	// readSections' onlyConfigured=true contract (setting_engine.go) requires
	// the caller to have already filtered sections down to the configured
	// set: it fails closed (hard error) on any absent section in that set,
	// by design, since the caller is asserting the user configured it. A
	// real Terraform Read only re-reads sections present in prior state, so
	// this pass filters to exactly the 3 sections this test hydrated (dpi,
	// country, rsyslogd/syslog) — not the full 13-section registry, which
	// would spuriously fail closed on the 10 sections this fake never seeded
	// (radius, mgmt, ips, etc., all absent from the snapshot).
	configuredSections := []settingSection{}
	for _, s := range realSections() {
		if s.key() == "dpi" || s.key() == "country" || s.key() == "rsyslogd" {
			configuredSections = append(configuredSections, s)
		}
	}
	if len(configuredSections) != 3 {
		t.Fatalf("configuredSections filter matched %d sections, want 3", len(configuredSections))
	}

	var reread settingResourceModel
	diags2 := readSections(ctx, configuredSections, client, "default", hydrated, &reread, true)
	if diags2.HasError() {
		t.Fatalf("clean re-plan: unexpected diagnostics: %v", diags2)
	}

	var wantDpi, gotDpi settingDpiModel
	if d := hydrated.Dpi.As(ctx, &wantDpi, basetypes.ObjectAsOptions{}); d.HasError() {
		t.Fatalf("extracting hydrated dpi: %v", d)
	}
	if d := reread.Dpi.As(ctx, &gotDpi, basetypes.ObjectAsOptions{}); d.HasError() {
		t.Fatalf("extracting reread dpi: %v", d)
	}
	if gotDpi.Enabled.ValueBool() != wantDpi.Enabled.ValueBool() ||
		gotDpi.FingerprintingEnabled.ValueBool() != wantDpi.FingerprintingEnabled.ValueBool() {
		t.Errorf("re-read dpi drifted from hydrated: got %+v, want %+v", gotDpi, wantDpi)
	}

	var wantCountry, gotCountry settingCountryModel
	if d := hydrated.Country.As(ctx, &wantCountry, basetypes.ObjectAsOptions{}); d.HasError() {
		t.Fatalf("extracting hydrated country: %v", d)
	}
	if d := reread.Country.As(ctx, &gotCountry, basetypes.ObjectAsOptions{}); d.HasError() {
		t.Fatalf("extracting reread country: %v", d)
	}
	if gotCountry.Code.ValueInt64() != wantCountry.Code.ValueInt64() {
		t.Errorf("re-read country drifted from hydrated: got %d, want %d", gotCountry.Code.ValueInt64(), wantCountry.Code.ValueInt64())
	}
}
