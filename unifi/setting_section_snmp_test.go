package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// Synthetic fixtures (design spec "Privacy-safe synthetic fixtures") — never
// the well-known SNMPv2 default community string "public", never a real
// passphrase.
const (
	snmpSyntheticCommunity     = "synthetic-ro-community"
	snmpSyntheticUsername      = "snmpv3-svc"
	snmpSyntheticPassword      = "Synthetic-Passw0rd!"
	snmpSyntheticPriorSecret   = "synthetic-prior-secret"
	snmpSyntheticNewSecret     = "synthetic-new-secret"
	snmpSyntheticNewSecret2    = "synthetic-new-secret-2"
	snmpSyntheticMaskedWireVal = "****************"
)

// snmpObject builds a configured snmp types.Object from a settingSnmpModel.
func snmpObject(t *testing.T, ctx context.Context, m settingSnmpModel) types.Object {
	t.Helper()
	obj, diags := types.ObjectValueFrom(ctx, snmpAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building snmp object: %v", diags)
	}
	return obj
}

// snmpExtract extracts a settingSnmpModel back out of a types.Object.
func snmpExtract(t *testing.T, ctx context.Context, obj types.Object) settingSnmpModel {
	t.Helper()
	var m settingSnmpModel
	if diags := obj.As(ctx, &m, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingSnmpModel: %v", diags)
	}
	return m
}

// TestSnmpSection_WireShape is a SYNTHETIC assertion, NOT a golden
// reproduction: there is no recorded/live SNMP controller snapshot backing
// this test (design spec open question #4; plan Task 1 Step 2). It pins the
// JSON shape overlay() intends to produce for a fully-configured model — all
// five fields, wire keys enabled/community/enabledV3/username/x_password —
// so a future refactor can't silently change the wire shape unnoticed. It
// must never be treated as a hard regression gate in the same sense as the
// 13 legacy sections' TestGolden_* fixtures, and it is deliberately excluded
// from unifi/setting_golden_test.go's migration-inventory oracle.
func TestSnmpSection_WireShape(t *testing.T) {
	ctx := context.Background()
	sec := snmpSection{}

	m := settingSnmpModel{
		Enabled:   types.BoolValue(true),
		Community: types.StringValue(snmpSyntheticCommunity),
		EnabledV3: types.BoolValue(true),
		Username:  types.StringValue(snmpSyntheticUsername),
		Password:  types.StringValue(snmpSyntheticPassword),
	}
	obj := snmpObject(t, ctx, m)

	model := settingResourceModel{Snmp: obj}
	prior := settingResourceModel{}
	snap := newRawSettings(nil)

	rs, configured, diags := sec.overlay(ctx, model, prior, snap)
	if diags.HasError() {
		t.Fatalf("overlay diagnostics: %v", diags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}
	if rs.Key != "snmp" {
		t.Errorf("rs.Key = %q, want %q", rs.Key, "snmp")
	}

	const wireShape = `{"enabled":true,"community":"synthetic-ro-community","enabledV3":true,"username":"snmpv3-svc","x_password":"Synthetic-Passw0rd!","key":""}`
	assertPUTBodyMatchesGolden(t, rs, wireShape)
}

// TestSnmpSection_Decode proves decode() reads the non-secret fields
// (enabled, enabledV3, username) from snapshot data, and preserves prior for
// BOTH secret fields (community, password) regardless of what the controller
// returned — simulating a masked wire value for each and asserting decode
// does NOT adopt it.
func TestSnmpSection_Decode(t *testing.T) {
	ctx := context.Background()
	sec := snmpSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "snmp"},
		Data: map[string]any{
			"enabled":    true,
			"enabledV3":  true,
			"username":   snmpSyntheticUsername,
			"community":  snmpSyntheticMaskedWireVal,
			"x_password": snmpSyntheticMaskedWireVal,
		},
	}})

	priorSnmp := settingSnmpModel{
		Enabled:   types.BoolValue(false),
		Community: types.StringValue(snmpSyntheticPriorSecret),
		EnabledV3: types.BoolValue(false),
		Username:  types.StringValue("prior-username"),
		Password:  types.StringValue(snmpSyntheticPriorSecret),
	}
	priorObj := snmpObject(t, ctx, priorSnmp)
	prior := settingResourceModel{Snmp: priorObj}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	if model.Snmp.IsNull() || model.Snmp.IsUnknown() {
		t.Fatalf("model.Snmp is null/unknown after decode")
	}

	got := snmpExtract(t, ctx, model.Snmp)

	if !got.Enabled.ValueBool() {
		t.Errorf("Enabled = %v, want true (from data)", got.Enabled)
	}
	if !got.EnabledV3.ValueBool() {
		t.Errorf("EnabledV3 = %v, want true (from data)", got.EnabledV3)
	}
	if got.Username.ValueString() != snmpSyntheticUsername {
		t.Errorf("Username = %q, want %q (from data)", got.Username.ValueString(), snmpSyntheticUsername)
	}

	// Both write-only secrets must come from prior, NEVER from the masked
	// wire values.
	if got.Community.ValueString() != snmpSyntheticPriorSecret {
		t.Errorf("Community = %q, want %q (prior) — masked community must not leak", got.Community.ValueString(), snmpSyntheticPriorSecret)
	}
	if got.Password.ValueString() != snmpSyntheticPriorSecret {
		t.Errorf("Password = %q, want %q (prior) — masked x_password must not leak", got.Password.ValueString(), snmpSyntheticPriorSecret)
	}
}

// TestSnmpSection_SecretMatrix is the full 3x3 product (plus 2 solo-field
// sanity cells) of both secrets' independently-reachable states: null
// (retain prior), set (send), mask-in-snapshot-with-null-config (retain
// prior, never resend mask). snmp has NO empty-clear/rotate-to-empty
// contract (community's validator LengthBetween(1,256) and password's
// LengthBetween(8,32) both reject "", so config can never carry an empty
// secret to overlay) — that state is deliberately absent from this table.
//
// Each cell asserts BOTH (a) the exact resulting model field values after
// carryBestEffort, AND (b) the exact independent presence/absence of BOTH
// wire keys ("community", "x_password") after overlay.
func TestSnmpSection_SecretMatrix(t *testing.T) {
	ctx := context.Background()
	sec := snmpSection{}

	type secretState struct {
		name       string
		config     types.String // the value carried in "plan"/config for overlay+carryBestEffort
		maskedData bool         // seed snapshot data with a masked wire value for this leaf
	}

	nullState := secretState{name: "null", config: types.StringNull()}
	unknownState := secretState{name: "unknown", config: types.StringUnknown()}
	setState := func(v string) secretState {
		return secretState{name: "set(" + v + ")", config: types.StringValue(v)}
	}
	maskedState := secretState{name: "masked-in-snapshot", config: types.StringNull(), maskedData: true}

	type cell struct {
		num                  int
		community, password  secretState
		wantCommunity        string // expected model value after carryBestEffort
		wantPassword         string
		wantCommunityOnWire  bool
		wantPasswordOnWire   bool
		wantCommunityOnWireV string
		wantPasswordOnWireV  string
	}

	cells := []cell{
		{
			num: 1, community: nullState, password: nullState,
			wantCommunity: snmpSyntheticPriorSecret, wantPassword: snmpSyntheticPriorSecret,
			wantCommunityOnWire: false, wantPasswordOnWire: false,
		},
		{
			num: 2, community: nullState, password: setState(snmpSyntheticNewSecret),
			wantCommunity: snmpSyntheticPriorSecret, wantPassword: snmpSyntheticNewSecret,
			wantCommunityOnWire: false, wantPasswordOnWire: true, wantPasswordOnWireV: snmpSyntheticNewSecret,
		},
		{
			num: 3, community: nullState, password: maskedState,
			wantCommunity: snmpSyntheticPriorSecret, wantPassword: snmpSyntheticPriorSecret,
			wantCommunityOnWire: false, wantPasswordOnWire: false,
		},
		{
			num: 4, community: setState(snmpSyntheticNewSecret), password: nullState,
			wantCommunity: snmpSyntheticNewSecret, wantPassword: snmpSyntheticPriorSecret,
			wantCommunityOnWire: true, wantCommunityOnWireV: snmpSyntheticNewSecret, wantPasswordOnWire: false,
		},
		{
			num: 5, community: setState(snmpSyntheticNewSecret), password: setState(snmpSyntheticNewSecret2),
			wantCommunity: snmpSyntheticNewSecret, wantPassword: snmpSyntheticNewSecret2,
			wantCommunityOnWire: true, wantCommunityOnWireV: snmpSyntheticNewSecret,
			wantPasswordOnWire: true, wantPasswordOnWireV: snmpSyntheticNewSecret2,
		},
		{
			num: 6, community: setState(snmpSyntheticNewSecret), password: maskedState,
			wantCommunity: snmpSyntheticNewSecret, wantPassword: snmpSyntheticPriorSecret,
			wantCommunityOnWire: true, wantCommunityOnWireV: snmpSyntheticNewSecret, wantPasswordOnWire: false,
		},
		{
			num: 7, community: maskedState, password: nullState,
			wantCommunity: snmpSyntheticPriorSecret, wantPassword: snmpSyntheticPriorSecret,
			wantCommunityOnWire: false, wantPasswordOnWire: false,
		},
		{
			num: 8, community: maskedState, password: setState(snmpSyntheticNewSecret),
			wantCommunity: snmpSyntheticPriorSecret, wantPassword: snmpSyntheticNewSecret,
			wantCommunityOnWire: false, wantPasswordOnWire: true, wantPasswordOnWireV: snmpSyntheticNewSecret,
		},
		{
			num: 9, community: maskedState, password: maskedState,
			wantCommunity: snmpSyntheticPriorSecret, wantPassword: snmpSyntheticPriorSecret,
			wantCommunityOnWire: false, wantPasswordOnWire: false,
		},
		{
			num: 10, community: unknownState, password: nullState,
			wantCommunity: snmpSyntheticPriorSecret, wantPassword: snmpSyntheticPriorSecret,
			wantCommunityOnWire: false, wantPasswordOnWire: false,
		},
		{
			num: 11, community: nullState, password: unknownState,
			wantCommunity: snmpSyntheticPriorSecret, wantPassword: snmpSyntheticPriorSecret,
			wantCommunityOnWire: false, wantPasswordOnWire: false,
		},
	}

	for _, c := range cells {
		t.Run(c.community.name+"_x_"+c.password.name, func(t *testing.T) {
			priorSnmp := settingSnmpModel{
				Enabled:   types.BoolValue(true),
				Community: types.StringValue(snmpSyntheticPriorSecret),
				EnabledV3: types.BoolValue(true),
				Username:  types.StringValue(snmpSyntheticUsername),
				Password:  types.StringValue(snmpSyntheticPriorSecret),
			}
			priorObj := snmpObject(t, ctx, priorSnmp)

			configSnmp := settingSnmpModel{
				Enabled:   types.BoolValue(true),
				Community: c.community.config,
				EnabledV3: types.BoolValue(true),
				Username:  types.StringValue(snmpSyntheticUsername),
				Password:  c.password.config,
			}
			configObj := snmpObject(t, ctx, configSnmp)

			// --- carryBestEffort assertion (model after best-effort recovery) ---
			plan := settingResourceModel{Snmp: configObj}
			dst := settingResourceModel{Snmp: priorObj}

			cbeDiags := sec.carryBestEffort(&dst, plan)
			if cbeDiags.HasError() {
				t.Fatalf("carryBestEffort diagnostics: %v", cbeDiags)
			}
			if dst.Snmp.IsNull() || dst.Snmp.IsUnknown() {
				t.Fatalf("dst.Snmp is null/unknown after carryBestEffort")
			}
			gotCbe := snmpExtract(t, ctx, dst.Snmp)
			if gotCbe.Community.ValueString() != c.wantCommunity {
				t.Errorf("cell %d: carryBestEffort Community = %q, want %q", c.num, gotCbe.Community.ValueString(), c.wantCommunity)
			}
			if gotCbe.Password.ValueString() != c.wantPassword {
				t.Errorf("cell %d: carryBestEffort Password = %q, want %q", c.num, gotCbe.Password.ValueString(), c.wantPassword)
			}

			// --- overlay assertion (wire body after overlay) ---
			data := map[string]any{}
			if c.community.maskedData {
				data["community"] = snmpSyntheticMaskedWireVal
			}
			if c.password.maskedData {
				data["x_password"] = snmpSyntheticMaskedWireVal
			}
			snap := newRawSettings([]settings.RawSetting{{
				BaseSetting: settings.BaseSetting{Key: "snmp"},
				Data:        data,
			}})

			model := settingResourceModel{Snmp: configObj}
			prior := settingResourceModel{Snmp: priorObj}

			rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
			if oDiags.HasError() {
				t.Fatalf("cell %d: overlay diagnostics: %v", c.num, oDiags)
			}
			if !configured {
				t.Fatalf("cell %d: overlay configured = false, want true", c.num)
			}

			gotCommunity, communityOnWire := rs.Data["community"]
			if communityOnWire != c.wantCommunityOnWire {
				t.Errorf("cell %d: community on wire = %v, want %v (rs.Data[community]=%v)", c.num, communityOnWire, c.wantCommunityOnWire, gotCommunity)
			}
			if c.wantCommunityOnWire && gotCommunity != c.wantCommunityOnWireV {
				t.Errorf("cell %d: community wire value = %v, want %q", c.num, gotCommunity, c.wantCommunityOnWireV)
			}

			gotPassword, passwordOnWire := rs.Data["x_password"]
			if passwordOnWire != c.wantPasswordOnWire {
				t.Errorf("cell %d: x_password on wire = %v, want %v (rs.Data[x_password]=%v)", c.num, passwordOnWire, c.wantPasswordOnWire, gotPassword)
			}
			if c.wantPasswordOnWire && gotPassword != c.wantPasswordOnWireV {
				t.Errorf("cell %d: x_password wire value = %v, want %q", c.num, gotPassword, c.wantPasswordOnWireV)
			}
		})
	}
}

// TestSnmpSection_CarryBestEffort specifically exercises the chained
// carrySecretObject composition (design spec "Second decision") with BOTH
// secrets resolving independently in the SAME carryBestEffort call: plan has
// community=null (must keep prior "synthetic-prior-secret") and
// password=set (must keep plan's "synthetic-new-secret"), while non-secret
// fields (enabled, enabled_v3, username) come from plan. This is table row
// #2 of TestSnmpSection_SecretMatrix exercised specifically through
// carryBestEffort's chained-call path.
func TestSnmpSection_CarryBestEffort(t *testing.T) {
	ctx := context.Background()
	sec := snmpSection{}

	priorSnmp := settingSnmpModel{
		Enabled:   types.BoolValue(false),
		Community: types.StringValue(snmpSyntheticPriorSecret),
		EnabledV3: types.BoolValue(false),
		Username:  types.StringValue("prior-username"),
		Password:  types.StringValue(snmpSyntheticPriorSecret),
	}
	priorObj := snmpObject(t, ctx, priorSnmp)

	planSnmp := settingSnmpModel{
		Enabled:   types.BoolValue(true),
		Community: types.StringNull(),
		EnabledV3: types.BoolValue(true),
		Username:  types.StringValue(snmpSyntheticUsername),
		Password:  types.StringValue(snmpSyntheticNewSecret),
	}
	planObj := snmpObject(t, ctx, planSnmp)

	plan := settingResourceModel{Snmp: planObj}
	dst := settingResourceModel{Snmp: priorObj}

	diags := sec.carryBestEffort(&dst, plan)
	if diags.HasError() {
		t.Fatalf("carryBestEffort diagnostics: %v", diags)
	}
	if dst.Snmp.IsNull() || dst.Snmp.IsUnknown() {
		t.Fatalf("dst.Snmp is null/unknown after carryBestEffort")
	}

	got := snmpExtract(t, ctx, dst.Snmp)

	if got.Community.ValueString() != snmpSyntheticPriorSecret {
		t.Errorf("Community = %q, want %q (retained from prior)", got.Community.ValueString(), snmpSyntheticPriorSecret)
	}
	if got.Password.ValueString() != snmpSyntheticNewSecret {
		t.Errorf("Password = %q, want %q (kept from plan)", got.Password.ValueString(), snmpSyntheticNewSecret)
	}
	if !got.Enabled.ValueBool() {
		t.Errorf("Enabled = %v, want true (from plan)", got.Enabled)
	}
	if !got.EnabledV3.ValueBool() {
		t.Errorf("EnabledV3 = %v, want true (from plan)", got.EnabledV3)
	}
	if got.Username.ValueString() != snmpSyntheticUsername {
		t.Errorf("Username = %q, want %q (from plan)", got.Username.ValueString(), snmpSyntheticUsername)
	}
}

// TestSnmpSection_Preservation proves overlay() preserves a PLAUSIBLE
// (but not necessarily real — see plan Task 1 Step 2 release-blocker note)
// unmodeled key already present in the snapshot's section data. This test's
// "no extra unmodeled fields" premise is PROVISIONAL: it is derived from
// go-unifi's generated struct only, not a live/recorded controller snapshot.
// It proves the RMW mechanism itself works (base survives untouched), not
// that the field list is complete.
func TestSnmpSection_Preservation(t *testing.T) {
	ctx := context.Background()
	sec := snmpSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "snmp"},
		Data: map[string]any{
			// PLAUSIBLE but unconfirmed key — see release-blocker note above.
			"x_snmp_trap_enabled": true,
		},
	}})

	m := settingSnmpModel{
		Enabled:   types.BoolValue(true),
		Community: types.StringValue(snmpSyntheticCommunity),
		EnabledV3: types.BoolValue(false),
		Username:  types.StringNull(),
		Password:  types.StringNull(),
	}
	obj := snmpObject(t, ctx, m)

	model := settingResourceModel{Snmp: obj}
	prior := settingResourceModel{}

	rs, configured, diags := sec.overlay(ctx, model, prior, snap)
	if diags.HasError() {
		t.Fatalf("overlay diagnostics: %v", diags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}

	if got, ok := rs.Data["x_snmp_trap_enabled"]; !ok || got != true {
		t.Errorf("rs.Data[%q] = %v (ok=%v), want %v", "x_snmp_trap_enabled", got, ok, true)
	}
}

// TestSnmpSection_NotConfigured proves overlay() returns configured == false
// and a zero-value RawSetting when model.Snmp is null, and isConfigured()
// reports false for both null and unknown.
func TestSnmpSection_NotConfigured(t *testing.T) {
	ctx := context.Background()
	sec := snmpSection{}

	model := settingResourceModel{Snmp: types.ObjectNull(snmpAttrTypes)}
	prior := settingResourceModel{}
	snap := newRawSettings(nil)

	rs, configured, diags := sec.overlay(ctx, model, prior, snap)
	if diags.HasError() {
		t.Fatalf("overlay diagnostics: %v", diags)
	}
	if configured {
		t.Fatalf("overlay configured = true, want false")
	}
	if rs.Key != "" || len(rs.Data) != 0 {
		t.Errorf("overlay returned non-zero RawSetting when not configured: %+v", rs)
	}

	if sec.isConfigured(model) {
		t.Errorf("isConfigured(null Snmp) = true, want false")
	}

	unknownModel := settingResourceModel{Snmp: types.ObjectUnknown(snmpAttrTypes)}
	if sec.isConfigured(unknownModel) {
		t.Errorf("isConfigured(unknown Snmp) = true, want false")
	}
}

// TestSnmpSection_DecodeRoundtrip proves decode(overlay(m)) returns m
// unchanged for the non-secret fields (enabled, enabled_v3, username).
// Secrets are excluded from this assertion per their preserve-prior decode
// semantics — roundtrip doesn't apply to them the same way, since decode
// always reads secrets from prior rather than from the (masked) wire value.
func TestSnmpSection_DecodeRoundtrip(t *testing.T) {
	ctx := context.Background()
	sec := snmpSection{}

	m := settingSnmpModel{
		Enabled:   types.BoolValue(true),
		Community: types.StringValue(snmpSyntheticCommunity),
		EnabledV3: types.BoolValue(true),
		Username:  types.StringValue(snmpSyntheticUsername),
		Password:  types.StringValue(snmpSyntheticPassword),
	}
	obj := snmpObject(t, ctx, m)

	model := settingResourceModel{Snmp: obj}
	prior := settingResourceModel{}
	snap := newRawSettings(nil)

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}

	roundtripSnap := newRawSettings([]settings.RawSetting{rs})

	// The prior fed to decode carries the SAME secrets as m, so preserve-prior
	// semantics reproduce m's secret values too — isolating this test to
	// proving the non-secret fields roundtrip through decode(overlay(m)).
	priorForDecode := settingResourceModel{Snmp: obj}
	decoded := settingResourceModel{}
	dDiags := sec.decode(ctx, roundtripSnap, priorForDecode, &decoded)
	if dDiags.HasError() {
		t.Fatalf("decode diagnostics: %v", dDiags)
	}

	got := snmpExtract(t, ctx, decoded.Snmp)

	if got.Enabled.ValueBool() != m.Enabled.ValueBool() {
		t.Errorf("Enabled = %v, want %v", got.Enabled, m.Enabled)
	}
	if got.EnabledV3.ValueBool() != m.EnabledV3.ValueBool() {
		t.Errorf("EnabledV3 = %v, want %v", got.EnabledV3, m.EnabledV3)
	}
	if got.Username.ValueString() != m.Username.ValueString() {
		t.Errorf("Username = %q, want %q", got.Username.ValueString(), m.Username.ValueString())
	}
}

// TestSnmpSection_InterfaceWiring is a light structural check that the
// section is registered and key()/attrName() both return "snmp".
func TestSnmpSection_InterfaceWiring(t *testing.T) {
	sec := snmpSection{}
	if sec.key() != "snmp" {
		t.Errorf("key() = %q, want %q", sec.key(), "snmp")
	}
	if sec.attrName() != "snmp" {
		t.Errorf("attrName() = %q, want %q", sec.attrName(), "snmp")
	}

	var found bool
	for _, s := range settingSections {
		if s.key() == "snmp" && s.attrName() == "snmp" {
			found = true
		}
	}
	if !found {
		t.Errorf("no section in settingSections registry has key()==attrName()==%q", "snmp")
	}
}
