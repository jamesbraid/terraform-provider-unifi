package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// goldenMagicSiteToSiteVpn is this section's fresh golden PUT body constant,
// captured from overlay()'s actual output against an empty snapshot — see
// TestMagicSiteToSiteVpnSection_GoldenReproduction.
const goldenMagicSiteToSiteVpn = `{"key": "magic_site_to_site_vpn", "enabled": true}`

// TestMagicSiteToSiteVpnSection_GoldenReproduction proves overlay()
// reproduces this section's golden PUT body.
func TestMagicSiteToSiteVpnSection_GoldenReproduction(t *testing.T) {
	ctx := context.Background()
	sec := magicSiteToSiteVpnSection{}

	m := settingMagicSiteToSiteVpnModel{Enabled: types.BoolValue(true)}
	obj, diags := types.ObjectValueFrom(ctx, magicSiteToSiteVpnAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building magic_site_to_site_vpn object: %v", diags)
	}

	model := settingResourceModel{MagicSiteToSiteVpn: obj}
	prior := settingResourceModel{}
	snap := newRawSettings(nil) // empty snapshot: section absent

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}
	if rs.Key != "magic_site_to_site_vpn" {
		t.Errorf("rs.Key = %q, want %q", rs.Key, "magic_site_to_site_vpn")
	}

	assertPUTBodyMatchesGolden(t, rs, goldenMagicSiteToSiteVpn)
}

// TestMagicSiteToSiteVpnSection_DecodeRoundTrip proves decode() reads
// "enabled" from a snapshot section's data.
func TestMagicSiteToSiteVpnSection_DecodeRoundTrip(t *testing.T) {
	ctx := context.Background()
	sec := magicSiteToSiteVpnSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "magic_site_to_site_vpn"},
		Data: map[string]any{
			"enabled": true,
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	if model.MagicSiteToSiteVpn.IsNull() || model.MagicSiteToSiteVpn.IsUnknown() {
		t.Fatalf("model.MagicSiteToSiteVpn is null/unknown after decode")
	}

	var got settingMagicSiteToSiteVpnModel
	if diags := model.MagicSiteToSiteVpn.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingMagicSiteToSiteVpnModel: %v", diags)
	}

	if !got.Enabled.ValueBool() {
		t.Errorf("Enabled = %v, want true", got.Enabled)
	}
}

// TestMagicSiteToSiteVpnSection_PreservesUnmodeledGeneratedField IS this
// section's Test<Xxx>Section_Preservation test, using a deliberately-named
// unmodeled key (x_unmanaged_example, NOT a guessed real field name) so the
// test's intent — proving the RMW preservation MECHANISM, not a specific
// hypothesized field — is unambiguous to a future reader. x_unmanaged_example
// stands in for a hypothesized controller-generated secret/key; none is
// confirmed to exist (see the design spec's "Primary open item"). This test
// proves the mechanism holds regardless of whether such a field is ever
// confirmed.
func TestMagicSiteToSiteVpnSection_PreservesUnmodeledGeneratedField(t *testing.T) {
	ctx := context.Background()
	sec := magicSiteToSiteVpnSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "magic_site_to_site_vpn"},
		Data: map[string]any{
			"enabled":             true,
			"x_unmanaged_example": "controller-set-value",
		},
	}})

	m := settingMagicSiteToSiteVpnModel{Enabled: types.BoolValue(true)}
	obj, diags := types.ObjectValueFrom(ctx, magicSiteToSiteVpnAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building magic_site_to_site_vpn object: %v", diags)
	}

	model := settingResourceModel{MagicSiteToSiteVpn: obj}
	rs, configured, oDiags := sec.overlay(ctx, model, settingResourceModel{}, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatal("overlay configured = false, want true")
	}
	if got, ok := rs.Data["x_unmanaged_example"]; !ok || got != "controller-set-value" {
		t.Errorf("rs.Data[x_unmanaged_example] = %v (ok=%v), want %q — the preserve-by-default RMW mechanism must carry an unmodeled field through untouched, which is how a real generated secret (if confirmed to exist) would survive without any special-case code", got, ok, "controller-set-value")
	}
}

// TestMagicSiteToSiteVpnSection_NotConfigured proves overlay() returns
// configured == false and a zero-value RawSetting when
// model.MagicSiteToSiteVpn is null.
func TestMagicSiteToSiteVpnSection_NotConfigured(t *testing.T) {
	ctx := context.Background()
	sec := magicSiteToSiteVpnSection{}

	model := settingResourceModel{MagicSiteToSiteVpn: types.ObjectNull(magicSiteToSiteVpnAttrTypes)}
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

// TestMagicSiteToSiteVpnSection_InterfaceWiring is a light structural check
// that the section is registered and key()/attrName() both return
// "magic_site_to_site_vpn".
func TestMagicSiteToSiteVpnSection_InterfaceWiring(t *testing.T) {
	sec := magicSiteToSiteVpnSection{}
	if sec.key() != "magic_site_to_site_vpn" {
		t.Errorf("key() = %q, want %q", sec.key(), "magic_site_to_site_vpn")
	}
	if sec.attrName() != "magic_site_to_site_vpn" {
		t.Errorf("attrName() = %q, want %q", sec.attrName(), "magic_site_to_site_vpn")
	}

	var found bool
	for _, s := range settingSections {
		if s.key() == "magic_site_to_site_vpn" && s.attrName() == "magic_site_to_site_vpn" {
			found = true
		}
	}
	if !found {
		t.Errorf("no section in settingSections registry has key()==attrName()==%q", "magic_site_to_site_vpn")
	}
}
