package unifi

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// TestRadiusSection_GoldenReproduction proves overlay() reproduces the
// Task-20 golden PUT body (TestGolden_radius) — this is the first RMW
// section that ALSO carries a write-only secret: the golden's
// configure_whole_network/tunneled_reply/enabled are unmodeled fields from
// the controller's existing section data (RMW), while accounting_enabled,
// acct_port, and secret (wire key x_secret) come from the model. Seeding
// the snapshot base with the RMW fields and overlaying the representative
// model on top must reproduce the golden byte-for-byte.
func TestRadiusSection_GoldenReproduction(t *testing.T) {
	ctx := context.Background()
	sec := radiusSection{}

	m := settingRadiusModel{
		AccountingEnabled: types.BoolValue(true),
		AcctPort:          types.Int64Value(1813),
		Secret:            types.StringValue("test-radius-secret"),
		// AuthPort and InterimUpdateInterval intentionally left null: not
		// configured by this representative model.
		AuthPort:              types.Int64Null(),
		InterimUpdateInterval: timetypes.NewGoDurationNull(),
	}
	obj, diags := types.ObjectValueFrom(ctx, radiusAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building radius object: %v", diags)
	}

	model := settingResourceModel{Radius: obj}
	prior := settingResourceModel{}
	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "radius"},
		Data: map[string]any{
			"configure_whole_network": true,
			"tunneled_reply":          true,
			"enabled":                 false,
		},
	}})

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}
	if rs.Key != "radius" {
		t.Errorf("rs.Key = %q, want %q", rs.Key, "radius")
	}

	assertPUTBodyMatchesGolden(t, rs, goldenRadius)
}

// TestRadiusSection_DecodeRoundTrip proves decode() reads the four
// API-readable leaves from a snapshot section's data, AND that the
// write-only secret leaf (Secret) is NEVER read from the (masked) wire key
// x_secret — it comes from prior instead. A decode that leaked the masked
// value would fail this test.
func TestRadiusSection_DecodeRoundTrip(t *testing.T) {
	ctx := context.Background()
	sec := radiusSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "radius"},
		Data: map[string]any{
			"accounting_enabled":      true,
			"acct_port":               float64(1813),
			"interim_update_interval": float64(30),
			"x_secret":                "MASKED",
			"enabled":                 false,
		},
	}})

	priorRadius := settingRadiusModel{
		Secret: types.StringValue("real-secret"),
	}
	priorObj, pDiags := types.ObjectValueFrom(ctx, radiusAttrTypes, priorRadius)
	if pDiags.HasError() {
		t.Fatalf("building prior radius object: %v", pDiags)
	}
	prior := settingResourceModel{Radius: priorObj}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	if model.Radius.IsNull() || model.Radius.IsUnknown() {
		t.Fatalf("model.Radius is null/unknown after decode")
	}

	var got settingRadiusModel
	if diags := model.Radius.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingRadiusModel: %v", diags)
	}

	if !got.AccountingEnabled.ValueBool() {
		t.Errorf("AccountingEnabled = %v, want true", got.AccountingEnabled)
	}
	if got.AcctPort.ValueInt64() != 1813 {
		t.Errorf("AcctPort = %v, want 1813", got.AcctPort)
	}
	if got.InterimUpdateInterval.IsNull() || got.InterimUpdateInterval.IsUnknown() {
		t.Fatalf("InterimUpdateInterval is null/unknown after decode")
	}
	dur, ddiags := got.InterimUpdateInterval.ValueGoDuration()
	if ddiags.HasError() {
		t.Fatalf("ValueGoDuration: %v", ddiags)
	}
	if dur != 30*time.Second {
		t.Errorf("InterimUpdateInterval = %v, want 30s", dur)
	}

	// The write-only secret must come from prior, NEVER from the masked
	// wire value "MASKED".
	if got.Secret.ValueString() != "real-secret" {
		t.Errorf("Secret = %q, want %q (prior) — masked x_secret must not leak", got.Secret.ValueString(), "real-secret")
	}
}

// TestRadiusSection_SecretOverlay proves overlay()'s write-only-secret
// policy for the x_secret wire key: a configured (set) model secret is
// written verbatim, while a null model secret DELETES x_secret from the PUT
// body entirely — the masked value read back from the controller must never
// be re-sent.
func TestRadiusSection_SecretOverlay(t *testing.T) {
	ctx := context.Background()
	sec := radiusSection{}

	t.Run("set secret is written", func(t *testing.T) {
		m := settingRadiusModel{
			AccountingEnabled:     types.BoolValue(true),
			AcctPort:              types.Int64Value(1813),
			AuthPort:              types.Int64Null(),
			InterimUpdateInterval: timetypes.NewGoDurationNull(),
			Secret:                types.StringValue("new-secret"),
		}
		obj, diags := types.ObjectValueFrom(ctx, radiusAttrTypes, m)
		if diags.HasError() {
			t.Fatalf("building radius object: %v", diags)
		}
		model := settingResourceModel{Radius: obj}
		prior := settingResourceModel{}
		snap := newRawSettings(nil)

		rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
		if oDiags.HasError() {
			t.Fatalf("overlay diagnostics: %v", oDiags)
		}
		if !configured {
			t.Fatalf("overlay configured = false, want true")
		}
		if got, ok := rs.Data["x_secret"]; !ok || got != "new-secret" {
			t.Errorf("rs.Data[%q] = %v (ok=%v), want %q", "x_secret", got, ok, "new-secret")
		}
	})

	t.Run("null secret deletes masked wire value", func(t *testing.T) {
		m := settingRadiusModel{
			AccountingEnabled:     types.BoolValue(true),
			AcctPort:              types.Int64Value(1813),
			AuthPort:              types.Int64Null(),
			InterimUpdateInterval: timetypes.NewGoDurationNull(),
			Secret:                types.StringNull(),
		}
		obj, diags := types.ObjectValueFrom(ctx, radiusAttrTypes, m)
		if diags.HasError() {
			t.Fatalf("building radius object: %v", diags)
		}
		model := settingResourceModel{Radius: obj}
		prior := settingResourceModel{}
		snap := newRawSettings([]settings.RawSetting{{
			BaseSetting: settings.BaseSetting{Key: "radius"},
			Data: map[string]any{
				"x_secret": "MASKED",
			},
		}})

		rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
		if oDiags.HasError() {
			t.Fatalf("overlay diagnostics: %v", oDiags)
		}
		if !configured {
			t.Fatalf("overlay configured = false, want true")
		}
		if got, ok := rs.Data["x_secret"]; ok {
			t.Errorf("rs.Data[%q] = %v, want key deleted (mask must never be re-sent)", "x_secret", got)
		}
	})
}

// TestRadiusSection_CarryBestEffortSecretMatrix pins carrySecretObject's
// per-leaf plan/prior choice for the radius object's secret matrix: plan's
// secret null or unknown falls back to prior's secret; a set plan secret
// (including an intentional empty-string rotate-to-empty) is kept from
// plan. A sibling non-secret leaf (accounting_enabled) always comes from
// plan regardless of the secret's state.
func TestRadiusSection_CarryBestEffortSecretMatrix(t *testing.T) {
	ctx := context.Background()
	sec := radiusSection{}

	newRadius := func(t *testing.T, accountingEnabled bool, secret types.String) types.Object {
		t.Helper()
		m := settingRadiusModel{
			AccountingEnabled:     types.BoolValue(accountingEnabled),
			AcctPort:              types.Int64Value(1813),
			AuthPort:              types.Int64Null(),
			InterimUpdateInterval: timetypes.NewGoDurationNull(),
			Secret:                secret,
		}
		obj, diags := types.ObjectValueFrom(ctx, radiusAttrTypes, m)
		if diags.HasError() {
			t.Fatalf("building radius object: %v", diags)
		}
		return obj
	}

	extract := func(t *testing.T, obj types.Object) settingRadiusModel {
		t.Helper()
		var m settingRadiusModel
		if diags := obj.As(ctx, &m, basetypes.ObjectAsOptions{}); diags.HasError() {
			t.Fatalf("extracting settingRadiusModel: %v", diags)
		}
		return m
	}

	cases := []struct {
		name       string
		planSecret types.String
		want       string
	}{
		{"plan null falls back to prior", types.StringNull(), "prior-secret"},
		{"plan unknown falls back to prior", types.StringUnknown(), "prior-secret"},
		{"plan set non-empty kept from plan", types.StringValue("new-secret"), "new-secret"},
		{"plan set empty (rotate-to-empty) kept from plan", types.StringValue(""), ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			priorObj := newRadius(t, false, types.StringValue("prior-secret"))
			planObj := newRadius(t, true, tc.planSecret)

			plan := settingResourceModel{Radius: planObj}
			// carryBestEffort's new signature reads prior's secret off dst
			// (bestEffortState seeds dst := prior before calling), so dst
			// starts pre-seeded with the prior object here too.
			dst := settingResourceModel{Radius: priorObj}

			diags := sec.carryBestEffort(&dst, plan)
			if diags.HasError() {
				t.Fatalf("carryBestEffort diagnostics: %v", diags)
			}

			if dst.Radius.IsNull() || dst.Radius.IsUnknown() {
				t.Fatalf("dst.Radius is null/unknown after carryBestEffort")
			}

			got := extract(t, dst.Radius)
			if got.Secret.IsUnknown() {
				t.Fatalf("dst secret is unknown, want a known value")
			}
			var gotSecret string
			if !got.Secret.IsNull() {
				gotSecret = got.Secret.ValueString()
			}
			if gotSecret != tc.want {
				t.Errorf("Secret = %q, want %q", gotSecret, tc.want)
			}

			// Sibling leaf always comes from plan.
			if !got.AccountingEnabled.ValueBool() {
				t.Errorf("AccountingEnabled = %v, want true (from plan)", got.AccountingEnabled)
			}
		})
	}
}

// TestRadiusSection_Preservation proves overlay() preserves an unmodeled key
// already present in the snapshot's section data (RMW) — the same mechanism
// exercised at the field level in TestRadiusSection_GoldenReproduction, here
// via a synthetic key.
func TestRadiusSection_Preservation(t *testing.T) {
	ctx := context.Background()
	sec := radiusSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "radius"},
		Data: map[string]any{
			"configure_whole_network": true,
			"x_unmanaged":             "keep",
		},
	}})

	m := settingRadiusModel{
		AccountingEnabled:     types.BoolValue(true),
		AcctPort:              types.Int64Value(1813),
		AuthPort:              types.Int64Null(),
		InterimUpdateInterval: timetypes.NewGoDurationNull(),
		Secret:                types.StringValue("test-radius-secret"),
	}
	obj, diags := types.ObjectValueFrom(ctx, radiusAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building radius object: %v", diags)
	}

	model := settingResourceModel{Radius: obj}
	prior := settingResourceModel{}

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}

	if got, ok := rs.Data["x_unmanaged"]; !ok || got != "keep" {
		t.Errorf("rs.Data[%q] = %v (ok=%v), want %q", "x_unmanaged", got, ok, "keep")
	}
	if got, ok := rs.Data["configure_whole_network"]; !ok || got != true {
		t.Errorf("rs.Data[%q] = %v (ok=%v), want %v", "configure_whole_network", got, ok, true)
	}
}

// TestRadiusSection_NotConfigured proves overlay() returns configured ==
// false and a zero-value RawSetting when model.Radius is null.
func TestRadiusSection_NotConfigured(t *testing.T) {
	ctx := context.Background()
	sec := radiusSection{}

	model := settingResourceModel{Radius: types.ObjectNull(radiusAttrTypes)}
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

// TestRadiusSection_InterfaceWiring is a light structural check that the
// section is registered and key()/attrName() both return "radius" (no
// key/attrName divergence for this section).
func TestRadiusSection_InterfaceWiring(t *testing.T) {
	sec := radiusSection{}
	if sec.key() != "radius" {
		t.Errorf("key() = %q, want %q", sec.key(), "radius")
	}
	if sec.attrName() != "radius" {
		t.Errorf("attrName() = %q, want %q", sec.attrName(), "radius")
	}

	var found bool
	for _, s := range settingSections {
		if s.key() == "radius" && s.attrName() == "radius" {
			found = true
		}
	}
	if !found {
		t.Errorf("no section in settingSections registry has key()==attrName()==%q", "radius")
	}
}
