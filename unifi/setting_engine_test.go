package unifi

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// ---------------------------------------------------------------------------
// Test scaffolding: stub sections grounded in the real settingResourceModel.
//
// autoSpeedtestStubSection and ntpStubSection are non-secret stubs bound to
// real settingResourceModel object fields (AutoSpeedtest, Ntp), each
// round-tripping a single string leaf already present in that section's real
// schema/attribute-type map (cron_expr, ntp_server_1). mgmtSecretStubSection
// is the secret-matrix stub, bound to the real Mgmt field, with one normal
// sibling leaf (ssh_username) and one write-only secret leaf (ssh_password)
// — mirroring the real mgmt section shape described in the task brief.
//
// modelWith/setSection/sectionVal/testSections (below) are the brief's
// placeholders, grounded against these stub sections and the real model.
// ---------------------------------------------------------------------------

// simpleStubSection is a non-secret stub section operating on a single
// string leaf of a real settingResourceModel types.Object field, identified
// by fieldGet/fieldSet closures so each instance can bind to a different
// real field (AutoSpeedtest/cron_expr, Ntp/ntp_server_1) without duplicating
// the interface implementation.
type simpleStubSection struct {
	k         string
	attrTypes map[string]attr.Type
	leaf      string

	get func(m settingResourceModel) types.Object
	set func(m *settingResourceModel, v types.Object)

	// overlayErr, when non-nil, makes overlay return an error diagnostic
	// instead of computing a RawSetting (used by
	// TestEngine_noWriteBeforeReconcileError).
	overlayErr bool
}

func (s simpleStubSection) key() string      { return s.k }
func (s simpleStubSection) attrName() string { return s.k }
func (s simpleStubSection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{Attributes: map[string]schema.Attribute{}}
}

func (s simpleStubSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	data := snap.dataCopy(s.k)
	priorObj := s.get(prior)
	leafVal, d := decodeString(data, s.leaf, types.StringNull())
	diags.Append(d...)

	attrs := map[string]attr.Value{}
	for k := range s.attrTypes {
		if k == s.leaf {
			attrs[k] = leafVal
			continue
		}
		// carry any other leaf from prior verbatim (there are none in
		// practice for these single-leaf stubs, but keep this generic).
		if !priorObj.IsNull() && !priorObj.IsUnknown() {
			if pv, ok := priorObj.Attributes()[k]; ok {
				attrs[k] = pv
				continue
			}
		}
		attrs[k] = types.StringNull()
	}
	obj, objDiags := types.ObjectValue(s.attrTypes, attrs)
	diags.Append(objDiags...)
	s.set(model, obj)
	return diags
}

func (s simpleStubSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics
	if s.overlayErr {
		diags.AddError("stub overlay error", "injected for TestEngine_noWriteBeforeReconcileError")
		return settings.RawSetting{}, true, diags
	}
	out := snap.dataCopy(s.k)
	obj := s.get(model)
	if !obj.IsNull() && !obj.IsUnknown() {
		if lv, ok := obj.Attributes()[s.leaf].(types.String); ok {
			overlayString(out, s.leaf, lv)
		}
	}
	out["key"] = s.k
	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: s.k},
		Data:        out,
	}
	return rs, true, diags
}

func (s simpleStubSection) carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics {
	s.set(dst, s.get(plan))
	return nil
}

func (s simpleStubSection) isConfigured(m settingResourceModel) bool {
	obj := s.get(m)
	return !obj.IsNull() && !obj.IsUnknown()
}

func autoSpeedtestStub() simpleStubSection {
	return simpleStubSection{
		k:         "auto_speedtest",
		attrTypes: autoSpeedtestAttrTypes,
		leaf:      "cron_expr",
		get:       func(m settingResourceModel) types.Object { return m.AutoSpeedtest },
		set:       func(m *settingResourceModel, v types.Object) { m.AutoSpeedtest = v },
	}
}

func ntpStub() simpleStubSection {
	return simpleStubSection{
		k:         "ntp",
		attrTypes: ntpAttrTypes,
		leaf:      "ntp_server_1",
		get:       func(m settingResourceModel) types.Object { return m.Ntp },
		set:       func(m *settingResourceModel, v types.Object) { m.Ntp = v },
	}
}

// mgmtSecretAttrTypes is a deliberately small mgmt-shaped attribute-type map
// (one normal leaf, one write-only-secret leaf) used only by
// mgmtSecretStubSection — enough to exercise carrySecretObject's secret-leaf
// matrix without dragging in the full real mgmt schema (ssh_keys list etc).
var mgmtSecretAttrTypes = map[string]attr.Type{
	"ssh_username": types.StringType,
	"ssh_password": types.StringType,
}

// mgmtSecretStubSection is the secret-matrix stub bound to the real Mgmt
// field, whose carryBestEffort delegates to carrySecretObject per the
// brief's contract for secret sections.
type mgmtSecretStubSection struct {
	k string
}

func (s mgmtSecretStubSection) key() string      { return s.k }
func (s mgmtSecretStubSection) attrName() string { return s.k }
func (s mgmtSecretStubSection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{Attributes: map[string]schema.Attribute{}}
}

func (s mgmtSecretStubSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	data := snap.dataCopy(s.k)
	userVal, d := decodeString(data, "ssh_username", types.StringNull())
	diags.Append(d...)
	// ssh_password is write-only: never read from API, preserve prior.
	priorObj := prior.Mgmt
	pwVal := types.StringNull()
	if !priorObj.IsNull() && !priorObj.IsUnknown() {
		if pv, ok := priorObj.Attributes()["ssh_password"].(types.String); ok {
			pwVal = pv
		}
	}
	obj, objDiags := types.ObjectValue(mgmtSecretAttrTypes, map[string]attr.Value{
		"ssh_username": userVal,
		"ssh_password": pwVal,
	})
	diags.Append(objDiags...)
	model.Mgmt = obj
	return diags
}

func (s mgmtSecretStubSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics
	out := snap.dataCopy(s.k)
	obj := model.Mgmt
	if !obj.IsNull() && !obj.IsUnknown() {
		if uv, ok := obj.Attributes()["ssh_username"].(types.String); ok {
			overlayString(out, "ssh_username", uv)
		}
		if pv, ok := obj.Attributes()["ssh_password"].(types.String); ok {
			if !pv.IsNull() && !pv.IsUnknown() {
				out["ssh_password"] = pv.ValueString()
			} else {
				delete(out, "ssh_password")
			}
		}
	}
	out["key"] = s.k
	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: s.k},
		Data:        out,
	}
	return rs, true, diags
}

func (s mgmtSecretStubSection) carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics {
	obj, diags := carrySecretObject(plan.Mgmt, dst.Mgmt, "ssh_password")
	dst.Mgmt = obj
	return diags
}

func (s mgmtSecretStubSection) isConfigured(m settingResourceModel) bool {
	return !m.Mgmt.IsNull() && !m.Mgmt.IsUnknown()
}

// testSections is the sections slice used across the engine tests that need
// a plain two-section (non-secret) fixture (bestEffortState exclusion test).
var testSections = []settingSection{autoSpeedtestStub(), ntpStub()}

// modelWith builds a settingResourceModel with the named stub section's
// single leaf set to val and every other registered stub section left as a
// null object. key must be one of testSections' keys ("auto_speedtest",
// "ntp").
func modelWith(key, val string) settingResourceModel {
	m := settingResourceModel{
		AutoSpeedtest: types.ObjectNull(autoSpeedtestAttrTypes),
		Ntp:           types.ObjectNull(ntpAttrTypes),
		Mgmt:          types.ObjectNull(mgmtSecretAttrTypes),
	}
	return setSection(m, key, val)
}

// setSection sets key's single leaf to val on m, returning the updated
// model. key must name one of testSections' stub sections.
func setSection(m settingResourceModel, key, val string) settingResourceModel {
	switch key {
	case "auto_speedtest":
		obj, diags := types.ObjectValue(autoSpeedtestAttrTypes, map[string]attr.Value{
			"enabled":   types.BoolNull(),
			"cron_expr": types.StringValue(val),
		})
		if diags.HasError() {
			panic(diags.Errors())
		}
		m.AutoSpeedtest = obj
	case "ntp":
		obj, diags := types.ObjectValue(ntpAttrTypes, map[string]attr.Value{
			"ntp_server_1":       types.StringValue(val),
			"ntp_server_2":       types.StringNull(),
			"ntp_server_3":       types.StringNull(),
			"ntp_server_4":       types.StringNull(),
			"setting_preference": types.StringNull(),
		})
		if diags.HasError() {
			panic(diags.Errors())
		}
		m.Ntp = obj
	default:
		panic("setSection: unknown key " + key)
	}
	return m
}

// sectionVal reads key's single leaf back out of m. key must name one of
// testSections' stub sections.
func sectionVal(m settingResourceModel, key string) string {
	switch key {
	case "auto_speedtest":
		if m.AutoSpeedtest.IsNull() || m.AutoSpeedtest.IsUnknown() {
			return ""
		}
		sv, _ := m.AutoSpeedtest.Attributes()["cron_expr"].(types.String)
		return sv.ValueString()
	case "ntp":
		if m.Ntp.IsNull() || m.Ntp.IsUnknown() {
			return ""
		}
		sv, _ := m.Ntp.Attributes()["ntp_server_1"].(types.String)
		return sv.ValueString()
	default:
		panic("sectionVal: unknown key " + key)
	}
}

// ---------------------------------------------------------------------------
// Step 1 failing tests.
// ---------------------------------------------------------------------------

func TestEngine_noWriteBeforeReconcileError(t *testing.T) {
	ctx := context.Background()
	client := newFakeSettingsClient()
	client.sections["auto_speedtest"] = settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: "auto_speedtest"},
		Data:        map[string]any{"key": "auto_speedtest", "cron_expr": "old"},
	}
	client.sections["ntp"] = settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: "ntp"},
		Data:        map[string]any{"key": "ntp", "ntp_server_1": "old"},
	}

	badNtp := ntpStub()
	badNtp.overlayErr = true
	sections := []settingSection{autoSpeedtestStub(), badNtp}

	// ntp must be configured (not just present on the controller) so its
	// injected overlay error is actually reached: applySections only
	// presence-checks/overlays sections the plan configures.
	prior := modelWith("auto_speedtest", "old")
	prior = setSection(prior, "ntp", "old")
	plan := modelWith("auto_speedtest", "new")
	plan = setSection(plan, "ntp", "new")

	_, diags := applySections(ctx, sections, client, "default", plan, prior)
	if !diags.HasError() {
		t.Fatalf("expected error diagnostics from failing overlay, got none")
	}
	if len(client.puts) != 0 {
		t.Fatalf("expected no PUTs when any overlay errors (reconcile-before-mutate), got %d: %+v", len(client.puts), client.puts)
	}
}

func TestEngine_oneSnapshotOnRead(t *testing.T) {
	ctx := context.Background()
	client := newFakeSettingsClient()
	client.sections["auto_speedtest"] = settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: "auto_speedtest"},
		Data:        map[string]any{"key": "auto_speedtest", "cron_expr": "old"},
	}

	countingClient := &countingSettingsClient{inner: client}
	sections := []settingSection{autoSpeedtestStub()}

	prior := modelWith("auto_speedtest", "old")
	var model settingResourceModel
	diags := readSections(ctx, sections, countingClient, "default", prior, &model, false)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if countingClient.listCalls != 1 {
		t.Fatalf("readSections triggered %d ListSettings calls, want exactly 1", countingClient.listCalls)
	}
}

// countingSettingsClient wraps a settingsClient and counts ListSettings
// calls, so TestEngine_oneSnapshotOnRead can assert readSections performs
// exactly one snapshot regardless of how many sections it decodes.
type countingSettingsClient struct {
	inner     settingsClient
	listCalls int
}

func (c *countingSettingsClient) ListSettings(ctx context.Context, site string) ([]settings.RawSetting, error) {
	c.listCalls++
	return c.inner.ListSettings(ctx, site)
}

func (c *countingSettingsClient) UpdateRawSetting(ctx context.Context, site string, s settings.RawSetting) error {
	return c.inner.UpdateRawSetting(ctx, site, s)
}

// TestEngine_readSectionsCapabilityErrorDoesNotBlockLaterSections is a
// regression test: a presence error (onlyConfigured=true) for one section
// must be reported without preventing decode() of a later, perfectly fine
// section in the same sections slice. An earlier buggy implementation
// checked the accumulated diags.HasError() (which is sticky once any prior
// section errors) instead of that section's own presence check, silently
// starving every section after the first failure.
func TestEngine_readSectionsCapabilityErrorDoesNotBlockLaterSections(t *testing.T) {
	ctx := context.Background()
	client := newFakeSettingsClient()
	// auto_speedtest is deliberately absent from the controller snapshot;
	// ntp is present and must still decode.
	client.sections["ntp"] = settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: "ntp"},
		Data:        map[string]any{"key": "ntp", "ntp_server_1": "from-api"},
	}

	sections := []settingSection{autoSpeedtestStub(), ntpStub()}
	prior := modelWith("auto_speedtest", "old")
	prior = setSection(prior, "ntp", "old")
	var model settingResourceModel

	diags := readSections(ctx, sections, client, "default", prior, &model, true)
	if !diags.HasError() {
		t.Fatalf("expected a presence error for unsupported auto_speedtest, got none")
	}
	if sectionVal(model, "ntp") != "from-api" {
		t.Fatalf("ntp (a later, supported section) was not decoded after an earlier section's presence error: got %q, want from-api", sectionVal(model, "ntp"))
	}
}

func TestEngine_partialApplyReReads(t *testing.T) {
	ctx := context.Background()
	client := newFakeSettingsClient()
	client.sections["auto_speedtest"] = settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: "auto_speedtest"},
		Data:        map[string]any{"key": "auto_speedtest", "cron_expr": "old-a"},
	}
	client.sections["ntp"] = settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: "ntp"},
		Data:        map[string]any{"key": "ntp", "ntp_server_1": "old-b"},
	}
	client.failUpdateOn["ntp"] = errors.New("injected ntp update failure")

	sections := []settingSection{autoSpeedtestStub(), ntpStub()}

	prior := modelWith("auto_speedtest", "old-a")
	prior = setSection(prior, "ntp", "old-b")
	plan := modelWith("auto_speedtest", "new-a")
	plan = setSection(plan, "ntp", "new-b")

	_, diags := applySections(ctx, sections, client, "default", plan, prior)
	if !diags.HasError() {
		t.Fatalf("expected error diagnostics from failed PUT on section b, got none")
	}

	var after settingResourceModel
	rdiags := readSections(ctx, sections, client, "default", prior, &after, false)
	if rdiags.HasError() {
		t.Fatalf("unexpected diagnostics re-reading: %v", rdiags)
	}
	if sectionVal(after, "auto_speedtest") != "new-a" {
		t.Fatalf("section a (successful PUT) = %q, want new-a", sectionVal(after, "auto_speedtest"))
	}
	if sectionVal(after, "ntp") != "old-b" {
		t.Fatalf("section b (failed PUT) = %q, want old-b (unchanged on controller)", sectionVal(after, "ntp"))
	}
}

func TestEngine_preservesUnmodeledKeys(t *testing.T) {
	ctx := context.Background()
	client := newFakeSettingsClient()
	client.sections["auto_speedtest"] = settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: "auto_speedtest"},
		Data: map[string]any{
			"key":          "auto_speedtest",
			"cron_expr":    "old",
			"x_unmanaged":  "keep-me",
			"another_flag": true,
		},
	}

	sections := []settingSection{autoSpeedtestStub()}
	prior := modelWith("auto_speedtest", "old")
	plan := modelWith("auto_speedtest", "new")

	_, diags := applySections(ctx, sections, client, "default", plan, prior)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	if len(client.puts) != 1 {
		t.Fatalf("expected 1 PUT, got %d", len(client.puts))
	}
	put := client.puts[0]
	if put.Data["x_unmanaged"] != "keep-me" {
		t.Errorf("PUT Data[x_unmanaged] = %v, want keep-me (unmodeled key must survive overlay)", put.Data["x_unmanaged"])
	}
	if put.Data["another_flag"] != true {
		t.Errorf("PUT Data[another_flag] = %v, want true (unmodeled key must survive overlay)", put.Data["another_flag"])
	}
	if put.Data["cron_expr"] != "new" {
		t.Errorf("PUT Data[cron_expr] = %v, want new", put.Data["cron_expr"])
	}
}

func TestBestEffortState_excludesUnattempted(t *testing.T) {
	prior := modelWith("auto_speedtest", "old")
	prior = setSection(prior, "ntp", "old")
	plan := modelWith("auto_speedtest", "new")
	plan = setSection(plan, "ntp", "new")

	got, gotDiags := bestEffortState(prior, plan, map[string]bool{"auto_speedtest": true}, testSections)
	if gotDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", gotDiags)
	}
	if sectionVal(got, "auto_speedtest") != "new" || sectionVal(got, "ntp") != "old" {
		t.Fatalf("best-effort must use plan for PUT sections and prior for the rest: auto_speedtest=%q ntp=%q",
			sectionVal(got, "auto_speedtest"), sectionVal(got, "ntp"))
	}
}

func TestBestEffortState_secretRotationRetained(t *testing.T) {
	mgmtStub := mgmtSecretStubSection{k: "mgmt"}
	sections := []settingSection{autoSpeedtestStub(), mgmtStub}

	priorMgmt, diags := types.ObjectValue(mgmtSecretAttrTypes, map[string]attr.Value{
		"ssh_username": types.StringValue("prior-user"),
		"ssh_password": types.StringValue("prior-secret"),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}

	t.Run("rotated secret retained from plan", func(t *testing.T) {
		rotatedMgmt, diags := types.ObjectValue(mgmtSecretAttrTypes, map[string]attr.Value{
			"ssh_username": types.StringValue("new-user"),
			"ssh_password": types.StringValue("rotated-secret"),
		})
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics building fixture: %v", diags)
		}
		prior := modelWith("auto_speedtest", "old")
		prior.Mgmt = priorMgmt
		plan := modelWith("auto_speedtest", "new")
		plan.Mgmt = rotatedMgmt

		put := map[string]bool{"auto_speedtest": true, "mgmt": true}
		got, gotDiags := bestEffortState(prior, plan, put, sections)
		if gotDiags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", gotDiags)
		}
		pw, ok := got.Mgmt.Attributes()["ssh_password"].(types.String)
		if !ok || pw.ValueString() != "rotated-secret" {
			t.Fatalf("expected rotated secret retained from plan, got %v", got.Mgmt.Attributes()["ssh_password"])
		}
	})

	t.Run("null secret falls back to prior", func(t *testing.T) {
		unsetMgmt, diags := types.ObjectValue(mgmtSecretAttrTypes, map[string]attr.Value{
			"ssh_username": types.StringValue("new-user"),
			"ssh_password": types.StringNull(),
		})
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics building fixture: %v", diags)
		}
		prior := modelWith("auto_speedtest", "old")
		prior.Mgmt = priorMgmt
		plan := modelWith("auto_speedtest", "new")
		plan.Mgmt = unsetMgmt

		put := map[string]bool{"auto_speedtest": true, "mgmt": true}
		got, gotDiags := bestEffortState(prior, plan, put, sections)
		if gotDiags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", gotDiags)
		}
		pw, ok := got.Mgmt.Attributes()["ssh_password"].(types.String)
		if !ok || pw.ValueString() != "prior-secret" {
			t.Fatalf("expected unset secret to fall back to prior, got %v", got.Mgmt.Attributes()["ssh_password"])
		}
	})

	t.Run("non-PUT section keeps prior entirely", func(t *testing.T) {
		rotatedMgmt, diags := types.ObjectValue(mgmtSecretAttrTypes, map[string]attr.Value{
			"ssh_username": types.StringValue("new-user"),
			"ssh_password": types.StringValue("rotated-secret"),
		})
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics building fixture: %v", diags)
		}
		prior := modelWith("auto_speedtest", "old")
		prior.Mgmt = priorMgmt
		plan := modelWith("auto_speedtest", "new")
		plan.Mgmt = rotatedMgmt

		// mgmt was NOT put this apply.
		put := map[string]bool{"auto_speedtest": true}
		got, gotDiags := bestEffortState(prior, plan, put, sections)
		if gotDiags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", gotDiags)
		}
		pw, ok := got.Mgmt.Attributes()["ssh_password"].(types.String)
		if !ok || pw.ValueString() != "prior-secret" {
			t.Fatalf("expected non-PUT section to keep prior entirely, got %v", got.Mgmt.Attributes()["ssh_password"])
		}
		user, ok := got.Mgmt.Attributes()["ssh_username"].(types.String)
		if !ok || user.ValueString() != "prior-user" {
			t.Fatalf("expected non-PUT section's non-secret leaf to also keep prior entirely, got %v", got.Mgmt.Attributes()["ssh_username"])
		}
	})
}

// ---------------------------------------------------------------------------
// TestCarrySecretObject_secretLeafMatrix: direct unit test of the shared
// helper, covering all 4 traps documented on carrySecretObject.
// ---------------------------------------------------------------------------

func TestCarrySecretObject_secretLeafMatrix(t *testing.T) {
	priorObj, diags := types.ObjectValue(mgmtSecretAttrTypes, map[string]attr.Value{
		"ssh_username": types.StringValue("prior-user"),
		"ssh_password": types.StringValue("prior-secret"),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}

	t.Run("null secret falls back to prior", func(t *testing.T) {
		planObj, diags := types.ObjectValue(mgmtSecretAttrTypes, map[string]attr.Value{
			"ssh_username": types.StringValue("plan-user"),
			"ssh_password": types.StringNull(),
		})
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics building fixture: %v", diags)
		}
		got, gotDiags := carrySecretObject(planObj, priorObj, "ssh_password")
		if gotDiags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", gotDiags)
		}
		pw := got.Attributes()["ssh_password"].(types.String)
		if pw.ValueString() != "prior-secret" {
			t.Fatalf("null secret: got %q, want prior-secret", pw.ValueString())
		}
	})

	t.Run("non-empty secret uses plan", func(t *testing.T) {
		planObj, diags := types.ObjectValue(mgmtSecretAttrTypes, map[string]attr.Value{
			"ssh_username": types.StringValue("plan-user"),
			"ssh_password": types.StringValue("new-secret"),
		})
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics building fixture: %v", diags)
		}
		got, gotDiags := carrySecretObject(planObj, priorObj, "ssh_password")
		if gotDiags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", gotDiags)
		}
		pw := got.Attributes()["ssh_password"].(types.String)
		if pw.ValueString() != "new-secret" {
			t.Fatalf("non-empty secret: got %q, want new-secret", pw.ValueString())
		}
	})

	// Trap 2: an empty-string secret WAS sent (rotate-to-empty) and must be
	// kept from plan, not silently replaced by prior.
	t.Run("empty string secret is rotate-to-empty, uses plan not prior", func(t *testing.T) {
		planObj, diags := types.ObjectValue(mgmtSecretAttrTypes, map[string]attr.Value{
			"ssh_username": types.StringValue("plan-user"),
			"ssh_password": types.StringValue(""),
		})
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics building fixture: %v", diags)
		}
		got, gotDiags := carrySecretObject(planObj, priorObj, "ssh_password")
		if gotDiags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", gotDiags)
		}
		pw := got.Attributes()["ssh_password"].(types.String)
		if pw.IsNull() {
			t.Fatalf("empty string secret: got null, want StringValue(\"\")")
		}
		if pw.ValueString() != "" {
			t.Fatalf("empty string secret: got %q, want empty string (rotate-to-empty, not prior)", pw.ValueString())
		}
	})

	// Trap 1: unknown must be treated exactly like null -> falls back to
	// prior.
	t.Run("unknown secret treated like null, falls back to prior", func(t *testing.T) {
		planObj, diags := types.ObjectValue(mgmtSecretAttrTypes, map[string]attr.Value{
			"ssh_username": types.StringValue("plan-user"),
			"ssh_password": types.StringUnknown(),
		})
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics building fixture: %v", diags)
		}
		got, gotDiags := carrySecretObject(planObj, priorObj, "ssh_password")
		if gotDiags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", gotDiags)
		}
		pw := got.Attributes()["ssh_password"].(types.String)
		if pw.ValueString() != "prior-secret" {
			t.Fatalf("unknown secret: got %q, want prior-secret (unknown treated like null)", pw.ValueString())
		}
	})

	t.Run("non-secret sibling leaf always comes from plan", func(t *testing.T) {
		planObj, diags := types.ObjectValue(mgmtSecretAttrTypes, map[string]attr.Value{
			"ssh_username": types.StringValue("plan-user"),
			"ssh_password": types.StringNull(),
		})
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics building fixture: %v", diags)
		}
		got, gotDiags := carrySecretObject(planObj, priorObj, "ssh_password")
		if gotDiags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", gotDiags)
		}
		user := got.Attributes()["ssh_username"].(types.String)
		if user.ValueString() != "plan-user" {
			t.Fatalf("non-secret sibling: got %q, want plan-user", user.ValueString())
		}
	})

	// Trap 3: a null/unknown parent object must return priorObj unchanged,
	// never manufacture a known object from a null section.
	t.Run("null parent object returns prior unchanged", func(t *testing.T) {
		nullPlan := types.ObjectNull(mgmtSecretAttrTypes)
		got, gotDiags := carrySecretObject(nullPlan, priorObj, "ssh_password")
		if gotDiags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", gotDiags)
		}
		if !got.Equal(priorObj) {
			t.Fatalf("null parent: got %v, want prior object unchanged (%v)", got, priorObj)
		}
	})

	t.Run("unknown parent object returns prior unchanged", func(t *testing.T) {
		unknownPlan := types.ObjectUnknown(mgmtSecretAttrTypes)
		got, gotDiags := carrySecretObject(unknownPlan, priorObj, "ssh_password")
		if gotDiags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", gotDiags)
		}
		if !got.Equal(priorObj) {
			t.Fatalf("unknown parent: got %v, want prior object unchanged (%v)", got, priorObj)
		}
	})
}
