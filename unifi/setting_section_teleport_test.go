package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// goldenTeleport is this section's fresh golden PUT body constant, captured
// from overlay()'s actual output against an empty snapshot — see
// TestTeleportSection_GoldenReproduction.
const goldenTeleport = `{"key": "teleport", "enabled": true, "subnet_cidr": "10.200.0.0/24"}`

// TestTeleportSection_GoldenReproduction proves overlay() reproduces this
// section's golden PUT body.
func TestTeleportSection_GoldenReproduction(t *testing.T) {
	ctx := context.Background()
	sec := teleportSection{}

	m := settingTeleportModel{
		Enabled:    types.BoolValue(true),
		SubnetCidr: types.StringValue("10.200.0.0/24"),
	}
	obj, diags := types.ObjectValueFrom(ctx, teleportAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building teleport object: %v", diags)
	}

	model := settingResourceModel{Teleport: obj}
	prior := settingResourceModel{}
	snap := newRawSettings(nil) // empty snapshot: section absent

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}
	if rs.Key != "teleport" {
		t.Errorf("rs.Key = %q, want %q", rs.Key, "teleport")
	}

	assertPUTBodyMatchesGolden(t, rs, goldenTeleport)
}

// TestTeleportSection_DecodeRoundTrip proves decode() reads a snapshot
// section's fields into model.Teleport.
func TestTeleportSection_DecodeRoundTrip(t *testing.T) {
	ctx := context.Background()
	sec := teleportSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "teleport"},
		Data: map[string]any{
			"enabled":     true,
			"subnet_cidr": "10.200.0.0/24",
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	if model.Teleport.IsNull() || model.Teleport.IsUnknown() {
		t.Fatalf("model.Teleport is null/unknown after decode")
	}

	var got settingTeleportModel
	if diags := model.Teleport.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingTeleportModel: %v", diags)
	}

	if !got.Enabled.ValueBool() {
		t.Errorf("Enabled = %v, want true", got.Enabled)
	}
	if got.SubnetCidr.ValueString() != "10.200.0.0/24" {
		t.Errorf("SubnetCidr = %q, want %q", got.SubnetCidr.ValueString(), "10.200.0.0/24")
	}
}

// TestTeleportSection_PresentEmpty proves the present-empty codec contract
// for subnet_cidr: a snapshot with subnet_cidr:"" decodes to
// StringValue(""), never collapsed to null — the wire-blessed "disabled,
// no subnet" shape.
func TestTeleportSection_PresentEmpty(t *testing.T) {
	ctx := context.Background()
	sec := teleportSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "teleport"},
		Data: map[string]any{
			"enabled":     false,
			"subnet_cidr": "",
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	var got settingTeleportModel
	if diags := model.Teleport.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingTeleportModel: %v", diags)
	}

	if got.SubnetCidr.IsNull() {
		t.Errorf("SubnetCidr is null, want present-empty StringValue(\"\")")
	}
	if got.SubnetCidr.ValueString() != "" {
		t.Errorf("SubnetCidr = %q, want empty string", got.SubnetCidr.ValueString())
	}
}

// TestTeleportSection_Preservation proves overlay() preserves an unmodeled
// key already present in the snapshot's section data.
func TestTeleportSection_Preservation(t *testing.T) {
	ctx := context.Background()
	sec := teleportSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "teleport"},
		Data: map[string]any{
			"enabled":     true,
			"subnet_cidr": "10.200.0.0/24",
			"x_unmanaged": "keep",
		},
	}})

	m := settingTeleportModel{
		Enabled:    types.BoolValue(true),
		SubnetCidr: types.StringValue("10.200.0.0/24"),
	}
	obj, diags := types.ObjectValueFrom(ctx, teleportAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building teleport object: %v", diags)
	}

	model := settingResourceModel{Teleport: obj}
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
}

// TestTeleportSection_NotConfigured proves overlay() returns configured ==
// false and a zero-value RawSetting when model.Teleport is null.
func TestTeleportSection_NotConfigured(t *testing.T) {
	ctx := context.Background()
	sec := teleportSection{}

	model := settingResourceModel{Teleport: types.ObjectNull(teleportAttrTypes)}
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

// TestTeleportSection_InterfaceWiring is a light structural check that the
// section is registered and key()/attrName() both return "teleport".
func TestTeleportSection_InterfaceWiring(t *testing.T) {
	sec := teleportSection{}
	if sec.key() != "teleport" {
		t.Errorf("key() = %q, want %q", sec.key(), "teleport")
	}
	if sec.attrName() != "teleport" {
		t.Errorf("attrName() = %q, want %q", sec.attrName(), "teleport")
	}

	var found bool
	for _, s := range settingSections {
		if s.key() == "teleport" && s.attrName() == "teleport" {
			found = true
		}
	}
	if !found {
		t.Errorf("no section in settingSections registry has key()==attrName()==%q", "teleport")
	}
}

// TestTeleportSection_EnabledSubnetCidrCoupling proves the *absence* of
// enforced coupling between enabled/subnet_cidr is a deliberate, tested
// contract, not an oversight: configuring subnet_cidr to a non-empty value
// while enabled = false must not be rejected by overlay, and clearing
// subnet_cidr to "" while enabled = true must be accepted and sent to the
// wire as an explicit empty string (the wire-blessed clear).
func TestTeleportSection_EnabledSubnetCidrCoupling(t *testing.T) {
	ctx := context.Background()
	sec := teleportSection{}

	t.Run("subnet_cidr set while disabled is not rejected by overlay", func(t *testing.T) {
		m := settingTeleportModel{
			Enabled:    types.BoolValue(false),
			SubnetCidr: types.StringValue("10.200.0.0/24"),
		}
		obj, diags := types.ObjectValueFrom(ctx, teleportAttrTypes, m)
		if diags.HasError() {
			t.Fatalf("building teleport object: %v", diags)
		}
		model := settingResourceModel{Teleport: obj}
		rs, configured, oDiags := sec.overlay(ctx, model, settingResourceModel{}, newRawSettings(nil))
		if oDiags.HasError() {
			t.Fatalf("overlay diagnostics: %v", oDiags)
		}
		if !configured {
			t.Fatal("overlay configured = false, want true")
		}
		if rs.Data["enabled"] != false {
			t.Errorf("rs.Data[enabled] = %v, want false", rs.Data["enabled"])
		}
		if rs.Data["subnet_cidr"] != "10.200.0.0/24" {
			t.Errorf("rs.Data[subnet_cidr] = %v, want 10.200.0.0/24 (no coupling enforcement)", rs.Data["subnet_cidr"])
		}
	})

	t.Run("empty subnet_cidr while enabled sends an explicit clear", func(t *testing.T) {
		m := settingTeleportModel{
			Enabled:    types.BoolValue(true),
			SubnetCidr: types.StringValue(""),
		}
		obj, diags := types.ObjectValueFrom(ctx, teleportAttrTypes, m)
		if diags.HasError() {
			t.Fatalf("building teleport object: %v", diags)
		}
		model := settingResourceModel{Teleport: obj}
		rs, configured, oDiags := sec.overlay(ctx, model, settingResourceModel{}, newRawSettings(nil))
		if oDiags.HasError() {
			t.Fatalf("overlay diagnostics: %v", oDiags)
		}
		if !configured {
			t.Fatal("overlay configured = false, want true")
		}
		if rs.Data["subnet_cidr"] != "" {
			t.Errorf(`rs.Data[subnet_cidr] = %v, want "" (explicit clear, wire-blessed via the |^$ regex alternative)`, rs.Data["subnet_cidr"])
		}
	})
}
