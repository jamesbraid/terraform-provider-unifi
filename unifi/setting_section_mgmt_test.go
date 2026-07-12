package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// TestMgmtSection_GoldenReproduction proves overlay() reproduces the Task-22
// golden PUT body (TestGolden_mgmt): this is the trickiest section, combining
// a write-only secret (ssh_password, wire x_ssh_password), a nested
// object-list (ssh_keys, wire x_ssh_keys), many ssh_*->x_ssh_* wire-key
// remaps, top-level RMW (alert_enabled/boot_sound/led_enabled/
// outdoor_mode_enabled/x_ssh_bind_wildcard are unmodeled by the schema), AND
// per-element RMW: the golden's x_ssh_keys[0] carries unmodeled "date":""
// and "fingerprint":"" that must come from the snapshot's same-index base
// element via overlayObjectList's same-index preservation, NOT from the
// model. The seed base below plants that index-0 element deliberately.
func TestMgmtSection_GoldenReproduction(t *testing.T) {
	ctx := context.Background()
	sec := mgmtSection{}

	sshKeys, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: mgmtSSHKeyAttrTypes},
		[]sshKeyModel{{
			Name:    types.StringValue("test-ssh-key"),
			Type:    types.StringValue("ssh-ed25519"),
			Key:     types.StringValue("ssh-ed25519 AAAATESTKEYMATERIAL test-key"),
			Comment: types.StringValue("test key"),
		}})
	if diags.HasError() {
		t.Fatalf("building ssh_keys list: %v", diags)
	}

	m := settingMgmtModel{
		AutoUpgrade:            types.BoolValue(true),
		AutoUpgradeHour:        types.Int64Value(3),
		SSHEnabled:             types.BoolValue(true),
		SSHKeys:                sshKeys,
		AdvancedFeatureEnabled: types.BoolValue(true),
		DebugToolsEnabled:      types.BoolValue(false),
		DirectConnectEnabled:   types.BoolValue(false),
		UnifiIdpEnabled:        types.BoolValue(false),
		WifimanEnabled:         types.BoolValue(false),
		SSHUsername:            types.StringValue("testadmin"),
		SSHPassword:            types.StringValue("test-password"),
		SSHAuthPasswordEnabled: types.BoolValue(true),
	}
	obj, objDiags := types.ObjectValueFrom(ctx, mgmtAttrTypes, m)
	if objDiags.HasError() {
		t.Fatalf("building mgmt object: %v", objDiags)
	}

	model := settingResourceModel{Mgmt: obj}
	prior := settingResourceModel{}
	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "mgmt"},
		Data: map[string]any{
			"alert_enabled":        true,
			"boot_sound":           true,
			"led_enabled":          true,
			"outdoor_mode_enabled": false,
			"x_ssh_bind_wildcard":  false,
			"x_ssh_keys": []any{
				map[string]any{"date": "", "fingerprint": ""},
			},
		},
	}})

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}
	if rs.Key != "mgmt" {
		t.Errorf("rs.Key = %q, want %q", rs.Key, "mgmt")
	}

	assertPUTBodyMatchesGolden(t, rs, goldenMgmt)
}

// TestMgmtSection_DecodeRoundTrip proves decode() reads the modeled leaves
// from a snapshot section's data through the WIRE keys (x_ssh_enabled,
// x_ssh_username, x_ssh_keys, x_ssh_auth_password_enabled), decodes the
// ssh_keys nested object list, and NEVER reads the masked x_ssh_password
// wire value: SSHPassword must come from prior instead.
func TestMgmtSection_DecodeRoundTrip(t *testing.T) {
	ctx := context.Background()
	sec := mgmtSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "mgmt"},
		Data: map[string]any{
			"auto_upgrade":                true,
			"auto_upgrade_hour":           float64(3),
			"advanced_feature_enabled":    true,
			"debug_tools_enabled":         false,
			"direct_connect_enabled":      false,
			"unifi_idp_enabled":           false,
			"wifiman_enabled":             false,
			"x_ssh_enabled":               true,
			"x_ssh_username":              "testadmin",
			"x_ssh_password":              "MASKED",
			"x_ssh_auth_password_enabled": true,
			"x_ssh_keys": []any{
				map[string]any{
					"name":        "test-ssh-key",
					"type":        "ssh-ed25519",
					"key":         "ssh-ed25519 AAAATESTKEYMATERIAL test-key",
					"comment":     "test key",
					"date":        "2024-01-01",
					"fingerprint": "aa:bb:cc",
				},
			},
		},
	}})

	priorMgmt := settingMgmtModel{
		SSHPassword: types.StringValue("real-pw"),
		SSHKeys:     types.ListNull(types.ObjectType{AttrTypes: mgmtSSHKeyAttrTypes}),
	}
	priorObj, pDiags := types.ObjectValueFrom(ctx, mgmtAttrTypes, priorMgmt)
	if pDiags.HasError() {
		t.Fatalf("building prior mgmt object: %v", pDiags)
	}
	prior := settingResourceModel{Mgmt: priorObj}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	if model.Mgmt.IsNull() || model.Mgmt.IsUnknown() {
		t.Fatalf("model.Mgmt is null/unknown after decode")
	}

	var got settingMgmtModel
	if diags := model.Mgmt.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingMgmtModel: %v", diags)
	}

	if !got.AutoUpgrade.ValueBool() {
		t.Errorf("AutoUpgrade = %v, want true", got.AutoUpgrade)
	}
	if got.AutoUpgradeHour.ValueInt64() != 3 {
		t.Errorf("AutoUpgradeHour = %v, want 3", got.AutoUpgradeHour)
	}
	if !got.AdvancedFeatureEnabled.ValueBool() {
		t.Errorf("AdvancedFeatureEnabled = %v, want true", got.AdvancedFeatureEnabled)
	}
	if got.DebugToolsEnabled.ValueBool() {
		t.Errorf("DebugToolsEnabled = %v, want false", got.DebugToolsEnabled)
	}
	if got.DirectConnectEnabled.ValueBool() {
		t.Errorf("DirectConnectEnabled = %v, want false", got.DirectConnectEnabled)
	}
	if got.UnifiIdpEnabled.ValueBool() {
		t.Errorf("UnifiIdpEnabled = %v, want false", got.UnifiIdpEnabled)
	}
	if got.WifimanEnabled.ValueBool() {
		t.Errorf("WifimanEnabled = %v, want false", got.WifimanEnabled)
	}
	if !got.SSHEnabled.ValueBool() {
		t.Errorf("SSHEnabled = %v, want true", got.SSHEnabled)
	}
	if got.SSHUsername.ValueString() != "testadmin" {
		t.Errorf("SSHUsername = %q, want %q", got.SSHUsername.ValueString(), "testadmin")
	}
	if !got.SSHAuthPasswordEnabled.ValueBool() {
		t.Errorf("SSHAuthPasswordEnabled = %v, want true", got.SSHAuthPasswordEnabled)
	}

	if got.SSHKeys.IsNull() || got.SSHKeys.IsUnknown() {
		t.Fatalf("SSHKeys is null/unknown after decode")
	}
	var sshKeys []sshKeyModel
	if diags := got.SSHKeys.ElementsAs(ctx, &sshKeys, false); diags.HasError() {
		t.Fatalf("extracting SSHKeys: %v", diags)
	}
	if len(sshKeys) != 1 {
		t.Fatalf("SSHKeys = %v, want 1 element", sshKeys)
	}
	k := sshKeys[0]
	if k.Name.ValueString() != "test-ssh-key" {
		t.Errorf("SSHKeys[0].Name = %q, want %q", k.Name.ValueString(), "test-ssh-key")
	}
	if k.Type.ValueString() != "ssh-ed25519" {
		t.Errorf("SSHKeys[0].Type = %q, want %q", k.Type.ValueString(), "ssh-ed25519")
	}
	if k.Key.ValueString() != "ssh-ed25519 AAAATESTKEYMATERIAL test-key" {
		t.Errorf("SSHKeys[0].Key = %q, want %q", k.Key.ValueString(), "ssh-ed25519 AAAATESTKEYMATERIAL test-key")
	}
	if k.Comment.ValueString() != "test key" {
		t.Errorf("SSHKeys[0].Comment = %q, want %q", k.Comment.ValueString(), "test key")
	}

	// The write-only secret must come from prior, NEVER from the masked
	// wire value "MASKED".
	if got.SSHPassword.ValueString() != "real-pw" {
		t.Errorf("SSHPassword = %q, want %q (prior) — masked x_ssh_password must not leak", got.SSHPassword.ValueString(), "real-pw")
	}
}

// TestMgmtSection_SecretOverlay proves overlay()'s write-only-secret policy
// for the x_ssh_password wire key: a configured (set) model password is
// written verbatim, while a null model password DELETES x_ssh_password from
// the PUT body entirely — the masked value read back from the controller
// must never be re-sent.
func TestMgmtSection_SecretOverlay(t *testing.T) {
	ctx := context.Background()
	sec := mgmtSection{}

	t.Run("set password is written", func(t *testing.T) {
		m := settingMgmtModel{
			SSHPassword: types.StringValue("new-password"),
			SSHKeys:     types.ListNull(types.ObjectType{AttrTypes: mgmtSSHKeyAttrTypes}),
		}
		obj, diags := types.ObjectValueFrom(ctx, mgmtAttrTypes, m)
		if diags.HasError() {
			t.Fatalf("building mgmt object: %v", diags)
		}
		model := settingResourceModel{Mgmt: obj}
		prior := settingResourceModel{}
		snap := newRawSettings(nil)

		rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
		if oDiags.HasError() {
			t.Fatalf("overlay diagnostics: %v", oDiags)
		}
		if !configured {
			t.Fatalf("overlay configured = false, want true")
		}
		if got, ok := rs.Data["x_ssh_password"]; !ok || got != "new-password" {
			t.Errorf("rs.Data[%q] = %v (ok=%v), want %q", "x_ssh_password", got, ok, "new-password")
		}
	})

	t.Run("null password deletes masked wire value", func(t *testing.T) {
		m := settingMgmtModel{
			SSHPassword: types.StringNull(),
			SSHKeys:     types.ListNull(types.ObjectType{AttrTypes: mgmtSSHKeyAttrTypes}),
		}
		obj, diags := types.ObjectValueFrom(ctx, mgmtAttrTypes, m)
		if diags.HasError() {
			t.Fatalf("building mgmt object: %v", diags)
		}
		model := settingResourceModel{Mgmt: obj}
		prior := settingResourceModel{}
		snap := newRawSettings([]settings.RawSetting{{
			BaseSetting: settings.BaseSetting{Key: "mgmt"},
			Data: map[string]any{
				"x_ssh_password": "MASKED",
			},
		}})

		rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
		if oDiags.HasError() {
			t.Fatalf("overlay diagnostics: %v", oDiags)
		}
		if !configured {
			t.Fatalf("overlay configured = false, want true")
		}
		if got, ok := rs.Data["x_ssh_password"]; ok {
			t.Errorf("rs.Data[%q] = %v, want key deleted (mask must never be re-sent)", "x_ssh_password", got)
		}
	})
}

// TestMgmtSection_CarryBestEffortSecretMatrix pins bestEffortObject's
// per-leaf plan/prior choice for the mgmt object's secret matrix: plan's
// ssh_password null or unknown falls back to prior's; a set plan password
// (including an intentional empty-string rotate-to-empty) is kept from plan.
// A sibling non-secret leaf (ssh_username) always comes from plan regardless
// of the secret's state.
func TestMgmtSection_CarryBestEffortSecretMatrix(t *testing.T) {
	ctx := context.Background()
	sec := mgmtSection{}

	newMgmt := func(t *testing.T, sshUsername string, sshPassword types.String) types.Object {
		t.Helper()
		m := settingMgmtModel{
			SSHUsername: types.StringValue(sshUsername),
			SSHPassword: sshPassword,
			SSHKeys:     types.ListNull(types.ObjectType{AttrTypes: mgmtSSHKeyAttrTypes}),
		}
		obj, diags := types.ObjectValueFrom(ctx, mgmtAttrTypes, m)
		if diags.HasError() {
			t.Fatalf("building mgmt object: %v", diags)
		}
		return obj
	}

	extract := func(t *testing.T, obj types.Object) settingMgmtModel {
		t.Helper()
		var m settingMgmtModel
		if diags := obj.As(ctx, &m, basetypes.ObjectAsOptions{}); diags.HasError() {
			t.Fatalf("extracting settingMgmtModel: %v", diags)
		}
		return m
	}

	cases := []struct {
		name         string
		planPassword types.String
		wantPassword string
	}{
		{"plan null falls back to prior", types.StringNull(), "prior-pw"},
		{"plan unknown falls back to prior", types.StringUnknown(), "prior-pw"},
		{"plan set non-empty kept from plan", types.StringValue("new-pw"), "new-pw"},
		{"plan set empty (rotate-to-empty) kept from plan", types.StringValue(""), ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			priorObj := newMgmt(t, "prior-user", types.StringValue("prior-pw"))
			planObj := newMgmt(t, "plan-user", tc.planPassword)

			plan := settingResourceModel{Mgmt: planObj}
			prior := settingResourceModel{Mgmt: priorObj}
			dst := settingResourceModel{}

			diags := sec.carryBestEffort(&dst, plan, prior)
			if diags.HasError() {
				t.Fatalf("carryBestEffort diagnostics: %v", diags)
			}

			if dst.Mgmt.IsNull() || dst.Mgmt.IsUnknown() {
				t.Fatalf("dst.Mgmt is null/unknown after carryBestEffort")
			}

			got := extract(t, dst.Mgmt)
			if got.SSHPassword.IsUnknown() {
				t.Fatalf("dst SSHPassword is unknown, want a known value")
			}
			var gotPassword string
			if !got.SSHPassword.IsNull() {
				gotPassword = got.SSHPassword.ValueString()
			}
			if gotPassword != tc.wantPassword {
				t.Errorf("SSHPassword = %q, want %q", gotPassword, tc.wantPassword)
			}

			// Sibling leaf always comes from plan.
			if got.SSHUsername.ValueString() != "plan-user" {
				t.Errorf("SSHUsername = %q, want %q (from plan)", got.SSHUsername.ValueString(), "plan-user")
			}
		})
	}
}

// TestMgmtSection_SshKeysPerElementPreservation proves the per-element RMW
// behavior specific to ssh_keys: overlaying a 1-element model onto a base
// whose same-index element carries unmodeled date/fingerprint fields
// preserves those fields in the output element alongside the model's
// updated name/type/key/comment.
func TestMgmtSection_SshKeysPerElementPreservation(t *testing.T) {
	ctx := context.Background()
	sec := mgmtSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "mgmt"},
		Data: map[string]any{
			"x_ssh_keys": []any{
				map[string]any{
					"name":        "old",
					"type":        "ssh-rsa",
					"key":         "old-key-material",
					"comment":     "old comment",
					"date":        "D",
					"fingerprint": "F",
				},
			},
		},
	}})

	sshKeys, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: mgmtSSHKeyAttrTypes},
		[]sshKeyModel{{
			Name:    types.StringValue("new"),
			Type:    types.StringValue("ssh-ed25519"),
			Key:     types.StringValue("new-key-material"),
			Comment: types.StringValue("new comment"),
		}})
	if diags.HasError() {
		t.Fatalf("building ssh_keys list: %v", diags)
	}

	m := settingMgmtModel{
		SSHKeys: sshKeys,
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

	if elem["name"] != "new" {
		t.Errorf("x_ssh_keys[0][name] = %v, want %q", elem["name"], "new")
	}
	if elem["type"] != "ssh-ed25519" {
		t.Errorf("x_ssh_keys[0][type] = %v, want %q", elem["type"], "ssh-ed25519")
	}
	if elem["key"] != "new-key-material" {
		t.Errorf("x_ssh_keys[0][key] = %v, want %q", elem["key"], "new-key-material")
	}
	if elem["comment"] != "new comment" {
		t.Errorf("x_ssh_keys[0][comment] = %v, want %q", elem["comment"], "new comment")
	}
	// Per-element RMW: unmodeled date/fingerprint from the same-index base
	// element must survive untouched.
	if elem["date"] != "D" {
		t.Errorf("x_ssh_keys[0][date] = %v, want %q (preserved from base)", elem["date"], "D")
	}
	if elem["fingerprint"] != "F" {
		t.Errorf("x_ssh_keys[0][fingerprint] = %v, want %q (preserved from base)", elem["fingerprint"], "F")
	}
}

// TestMgmtSection_Preservation proves overlay() preserves an unmodeled
// top-level key already present in the snapshot's section data (RMW).
func TestMgmtSection_Preservation(t *testing.T) {
	ctx := context.Background()
	sec := mgmtSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "mgmt"},
		Data: map[string]any{
			"alert_enabled": true,
			"x_unmanaged":   "keep",
		},
	}})

	m := settingMgmtModel{
		SSHKeys: types.ListNull(types.ObjectType{AttrTypes: mgmtSSHKeyAttrTypes}),
	}
	obj, diags := types.ObjectValueFrom(ctx, mgmtAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building mgmt object: %v", diags)
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

	if got, ok := rs.Data["alert_enabled"]; !ok || got != true {
		t.Errorf("rs.Data[%q] = %v (ok=%v), want %v", "alert_enabled", got, ok, true)
	}
	if got, ok := rs.Data["x_unmanaged"]; !ok || got != "keep" {
		t.Errorf("rs.Data[%q] = %v (ok=%v), want %q", "x_unmanaged", got, ok, "keep")
	}
}

// TestMgmtSection_NotConfigured proves overlay() returns configured == false
// and a zero-value RawSetting when model.Mgmt is null.
func TestMgmtSection_NotConfigured(t *testing.T) {
	ctx := context.Background()
	sec := mgmtSection{}

	model := settingResourceModel{Mgmt: types.ObjectNull(mgmtAttrTypes)}
	prior := settingResourceModel{}
	snap := newRawSettings(nil)

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if configured {
		t.Fatalf("overlay configured = true, want false")
	}
	if rs.Key != "" || len(rs.Data) != 0 {
		t.Errorf("overlay returned non-zero RawSetting when not configured: %+v", rs)
	}
}

// TestMgmtSection_InterfaceWiring is a light structural check that the
// section is registered and key()/attrName() both return "mgmt" (no
// key/attrName divergence for this section, despite the ssh_* wire remaps
// living inside it).
func TestMgmtSection_InterfaceWiring(t *testing.T) {
	sec := mgmtSection{}
	if sec.key() != "mgmt" {
		t.Errorf("key() = %q, want %q", sec.key(), "mgmt")
	}
	if sec.attrName() != "mgmt" {
		t.Errorf("attrName() = %q, want %q", sec.attrName(), "mgmt")
	}

	var foundByKey, foundByAttrName bool
	for _, s := range settingSections {
		if s.key() == "mgmt" {
			foundByKey = true
		}
		if s.attrName() == "mgmt" {
			foundByAttrName = true
		}
	}
	if !foundByKey {
		t.Errorf("no section in settingSections registry has key() == %q", "mgmt")
	}
	if !foundByAttrName {
		t.Errorf("no section in settingSections registry has attrName() == %q", "mgmt")
	}
}
