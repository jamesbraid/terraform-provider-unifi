package unifi

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// --- low-level codec: codecString ---

func TestCodecString_presentEmptyIsValueNotNull(t *testing.T) {
	v, diags := codecString(map[string]any{"k": ""}, "k")
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if v.IsNull() {
		t.Fatalf("expected StringValue(\"\"), got null")
	}
	if v.ValueString() != "" {
		t.Fatalf("expected empty string, got %q", v.ValueString())
	}
}

func TestCodecString_absentIsNull(t *testing.T) {
	v, diags := codecString(map[string]any{}, "k")
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !v.IsNull() {
		t.Fatalf("expected null for absent key, got %v", v)
	}
}

func TestCodecString_explicitNullIsNull(t *testing.T) {
	v, diags := codecString(map[string]any{"k": nil}, "k")
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !v.IsNull() {
		t.Fatalf("expected null for explicit-null key, got %v", v)
	}
}

func TestCodecString_wrongTypeIsDiagnostic(t *testing.T) {
	_, diags := codecString(map[string]any{"k": 42.0}, "k")
	if !diags.HasError() {
		t.Fatalf("expected diagnostic for wrong type, got none")
	}
}

func TestCodecString_presentValue(t *testing.T) {
	v, diags := codecString(map[string]any{"k": "hello"}, "k")
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if v.ValueString() != "hello" {
		t.Fatalf("expected \"hello\", got %q", v.ValueString())
	}
}

// --- low-level codec: codecBool ---

func TestCodecBool_presentValue(t *testing.T) {
	v, diags := codecBool(map[string]any{"k": true}, "k")
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if v.IsNull() || !v.ValueBool() {
		t.Fatalf("expected true, got %v", v)
	}
}

func TestCodecBool_absentIsNull(t *testing.T) {
	v, diags := codecBool(map[string]any{}, "k")
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !v.IsNull() {
		t.Fatalf("expected null for absent key, got %v", v)
	}
}

func TestCodecBool_wrongTypeIsDiagnostic(t *testing.T) {
	_, diags := codecBool(map[string]any{"k": "not-a-bool"}, "k")
	if !diags.HasError() {
		t.Fatalf("expected diagnostic for wrong type, got none")
	}
}

// --- low-level codec: codecInt64 ---

func TestCodecInt64_presentValue(t *testing.T) {
	v, diags := codecInt64(map[string]any{"k": 42.0}, "k")
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if v.ValueInt64() != 42 {
		t.Fatalf("expected 42, got %d", v.ValueInt64())
	}
}

func TestCodecInt64_absentIsNull(t *testing.T) {
	v, diags := codecInt64(map[string]any{}, "k")
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !v.IsNull() {
		t.Fatalf("expected null for absent key, got %v", v)
	}
}

func TestCodecInt64_fractionalIsDiagnostic(t *testing.T) {
	_, diags := codecInt64(map[string]any{"k": 1.9}, "k")
	if !diags.HasError() {
		t.Fatalf("expected diagnostic for fractional value, got none")
	}
}

func TestCodecInt64_wrongTypeIsDiagnostic(t *testing.T) {
	_, diags := codecInt64(map[string]any{"k": "nope"}, "k")
	if !diags.HasError() {
		t.Fatalf("expected diagnostic for wrong type, got none")
	}
}

// --- low-level codec: codecStringList ---

func TestCodecStringList_presentEmptyIsValueNotNull(t *testing.T) {
	ctx := context.Background()
	v, diags := codecStringList(ctx, map[string]any{"k": []any{}}, "k")
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if v.IsNull() {
		t.Fatalf("expected empty ListValue, got null")
	}
	if len(v.Elements()) != 0 {
		t.Fatalf("expected 0 elements, got %d", len(v.Elements()))
	}
}

func TestCodecStringList_absentIsNull(t *testing.T) {
	ctx := context.Background()
	v, diags := codecStringList(ctx, map[string]any{}, "k")
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !v.IsNull() {
		t.Fatalf("expected null for absent key, got %v", v)
	}
}

func TestCodecStringList_presentValues(t *testing.T) {
	ctx := context.Background()
	v, diags := codecStringList(ctx, map[string]any{"k": []any{"a", "b"}}, "k")
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	elems := v.Elements()
	if len(elems) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(elems))
	}
}

func TestCodecStringList_wrongElementTypeIsDiagnostic(t *testing.T) {
	ctx := context.Background()
	_, diags := codecStringList(ctx, map[string]any{"k": []any{"a", 5.0}}, "k")
	if !diags.HasError() {
		t.Fatalf("expected diagnostic for wrong element type, got none")
	}
}

func TestCodecStringList_wrongTypeIsDiagnostic(t *testing.T) {
	ctx := context.Background()
	_, diags := codecStringList(ctx, map[string]any{"k": "not-a-list"}, "k")
	if !diags.HasError() {
		t.Fatalf("expected diagnostic for wrong type, got none")
	}
}

// --- low-level setters: putString/putInt64/putBool/putStringList ---

func TestPutString_writesEmpty(t *testing.T) {
	out := map[string]any{}
	putString(out, "k", types.StringValue(""))
	v, ok := out["k"]
	if !ok {
		t.Fatalf("expected key to be written")
	}
	if v != "" {
		t.Fatalf("expected empty string, got %v", v)
	}
}

func TestPutString_skipsNull(t *testing.T) {
	out := map[string]any{}
	putString(out, "k", types.StringNull())
	if _, ok := out["k"]; ok {
		t.Fatalf("expected null to be skipped, key present with value %v", out["k"])
	}
}

func TestPutString_skipsUnknown(t *testing.T) {
	out := map[string]any{}
	putString(out, "k", types.StringUnknown())
	if _, ok := out["k"]; ok {
		t.Fatalf("expected unknown to be skipped, key present with value %v", out["k"])
	}
}

func TestPutString_writesValue(t *testing.T) {
	out := map[string]any{}
	putString(out, "k", types.StringValue("hi"))
	if out["k"] != "hi" {
		t.Fatalf("expected \"hi\", got %v", out["k"])
	}
}

func TestPutInt64_writesValue(t *testing.T) {
	out := map[string]any{}
	putInt64(out, "k", types.Int64Value(7))
	if out["k"] != float64(7) {
		t.Fatalf("expected 7.0, got %v", out["k"])
	}
}

func TestPutInt64_skipsNull(t *testing.T) {
	out := map[string]any{}
	putInt64(out, "k", types.Int64Null())
	if _, ok := out["k"]; ok {
		t.Fatalf("expected null to be skipped")
	}
}

func TestPutBool_writesFalse(t *testing.T) {
	out := map[string]any{}
	putBool(out, "k", types.BoolValue(false))
	v, ok := out["k"]
	if !ok {
		t.Fatalf("expected key to be written for false (present-empty analog)")
	}
	if v != false {
		t.Fatalf("expected false, got %v", v)
	}
}

func TestPutBool_skipsNull(t *testing.T) {
	out := map[string]any{}
	putBool(out, "k", types.BoolNull())
	if _, ok := out["k"]; ok {
		t.Fatalf("expected null to be skipped")
	}
}

func TestPutStringList_writesEmptyList(t *testing.T) {
	ctx := context.Background()
	out := map[string]any{}
	empty, diags := types.ListValue(types.StringType, []attr.Value{})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}
	putDiags := putStringList(ctx, out, "k", empty)
	if putDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", putDiags)
	}
	v, ok := out["k"]
	if !ok {
		t.Fatalf("expected key to be written for empty list")
	}
	list, ok := v.([]any)
	if !ok || len(list) != 0 {
		t.Fatalf("expected empty []any, got %v", v)
	}
}

func TestPutStringList_skipsNull(t *testing.T) {
	ctx := context.Background()
	out := map[string]any{}
	putDiags := putStringList(ctx, out, "k", types.ListNull(types.StringType))
	if putDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", putDiags)
	}
	if _, ok := out["k"]; ok {
		t.Fatalf("expected null to be skipped")
	}
}

func TestPutStringList_skipsUnknown(t *testing.T) {
	ctx := context.Background()
	out := map[string]any{}
	putDiags := putStringList(ctx, out, "k", types.ListUnknown(types.StringType))
	if putDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", putDiags)
	}
	if _, ok := out["k"]; ok {
		t.Fatalf("expected unknown to be skipped")
	}
}

func TestPutStringList_writesValues(t *testing.T) {
	ctx := context.Background()
	out := map[string]any{}
	l, diags := types.ListValue(types.StringType, []attr.Value{types.StringValue("a"), types.StringValue("b")})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}
	putDiags := putStringList(ctx, out, "k", l)
	if putDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", putDiags)
	}
	list, ok := out["k"].([]any)
	if !ok || len(list) != 2 {
		t.Fatalf("expected 2-element []any, got %v", out["k"])
	}
}

// --- ownership-aware layer: decodeString ---

func TestDecodeString_writeOnlySecretPreservesPriorAndNeverTouchesData(t *testing.T) {
	data := map[string]any{"k": "******"}
	prior := types.StringValue("keep")
	v, diags := decodeString(data, "k", ownerWriteOnlySecret, prior)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if v.ValueString() != "keep" {
		t.Fatalf("expected prior value \"keep\" preserved, got %q", v.ValueString())
	}
	if data["k"] != "******" {
		t.Fatalf("expected data map untouched, got %v", data["k"])
	}
}

func TestDecodeString_managedReadsFromData(t *testing.T) {
	data := map[string]any{"k": "from-api"}
	prior := types.StringValue("stale")
	v, diags := decodeString(data, "k", ownerManaged, prior)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if v.ValueString() != "from-api" {
		t.Fatalf("expected value read from data, got %q", v.ValueString())
	}
}

func TestDecodeString_computedReadsFromData(t *testing.T) {
	data := map[string]any{"k": "computed-val"}
	v, diags := decodeString(data, "k", ownerComputed, types.StringNull())
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if v.ValueString() != "computed-val" {
		t.Fatalf("expected value read from data, got %q", v.ValueString())
	}
}

func TestDecodeString_preservedUnmanagedPreservesPrior(t *testing.T) {
	data := map[string]any{"k": "from-api-ignored"}
	prior := types.StringValue("prior-kept")
	v, diags := decodeString(data, "k", ownerPreservedUnmanaged, prior)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if v.ValueString() != "prior-kept" {
		t.Fatalf("expected prior preserved for PreservedUnmanaged, got %q", v.ValueString())
	}
}

// --- ownership-aware layer: overlayString ---

func TestOverlayString_computedWritesNothing(t *testing.T) {
	out := map[string]any{"k": "original"}
	overlayString(out, "k", ownerComputed, types.StringValue("x"))
	if out["k"] != "original" {
		t.Fatalf("expected snapshot value untouched, got %v", out["k"])
	}
}

func TestOverlayString_managedWritesEmpty(t *testing.T) {
	out := map[string]any{"k": "original"}
	overlayString(out, "k", ownerManaged, types.StringValue(""))
	if out["k"] != "" {
		t.Fatalf("expected empty string written, got %v", out["k"])
	}
}

func TestOverlayString_managedNullPreservesSnapshot(t *testing.T) {
	out := map[string]any{"k": "original"}
	overlayString(out, "k", ownerManaged, types.StringNull())
	if out["k"] != "original" {
		t.Fatalf("expected snapshot value preserved on null, got %v", out["k"])
	}
}

func TestOverlayString_coManagedWritesPresent(t *testing.T) {
	out := map[string]any{"k": "original"}
	overlayString(out, "k", ownerCoManaged, types.StringValue("new"))
	if out["k"] != "new" {
		t.Fatalf("expected new value written, got %v", out["k"])
	}
}

func TestOverlayString_generatedSecretWritesNothing(t *testing.T) {
	out := map[string]any{"k": "original"}
	overlayString(out, "k", ownerGeneratedSecret, types.StringValue("attempted"))
	if out["k"] != "original" {
		t.Fatalf("expected snapshot value untouched for GeneratedSecret, got %v", out["k"])
	}
}

func TestOverlayString_preservedUnmanagedWritesNothing(t *testing.T) {
	out := map[string]any{"k": "original"}
	overlayString(out, "k", ownerPreservedUnmanaged, types.StringValue("attempted"))
	if out["k"] != "original" {
		t.Fatalf("expected snapshot value untouched for PreservedUnmanaged, got %v", out["k"])
	}
}

// --- CRITICAL: write-only-secret delete-on-null/unknown (the rule the two-line
// brief snippet gets wrong) ---

func TestOverlayString_writeOnlySecretNullDeletesKey(t *testing.T) {
	out := map[string]any{"x_secret": "******"}
	overlayString(out, "x_secret", ownerWriteOnlySecret, types.StringNull())
	if _, ok := out["x_secret"]; ok {
		t.Fatalf("expected key deleted for null write-only-secret config, still present: %v", out["x_secret"])
	}
}

func TestOverlayString_writeOnlySecretUnknownDeletesKey(t *testing.T) {
	out := map[string]any{"x_secret": "******"}
	overlayString(out, "x_secret", ownerWriteOnlySecret, types.StringUnknown())
	if _, ok := out["x_secret"]; ok {
		t.Fatalf("expected key deleted for unknown write-only-secret config, still present: %v", out["x_secret"])
	}
}

func TestOverlayString_writeOnlySecretEmptyValueWritesNotDeletes(t *testing.T) {
	out := map[string]any{"x_secret": "******"}
	overlayString(out, "x_secret", ownerWriteOnlySecret, types.StringValue(""))
	v, ok := out["x_secret"]
	if !ok {
		t.Fatalf("expected key present after configured-empty write-only-secret overlay (intentional clear)")
	}
	if v != "" {
		t.Fatalf("expected empty string written, got %v", v)
	}
}

func TestOverlayString_writeOnlySecretSetValueWrites(t *testing.T) {
	out := map[string]any{"x_secret": "******"}
	overlayString(out, "x_secret", ownerWriteOnlySecret, types.StringValue("new-secret"))
	if out["x_secret"] != "new-secret" {
		t.Fatalf("expected new secret written, got %v", out["x_secret"])
	}
}

func TestOverlayString_writeOnlySecretNullOnAbsentKeyIsNoop(t *testing.T) {
	out := map[string]any{}
	overlayString(out, "x_secret", ownerWriteOnlySecret, types.StringNull())
	if _, ok := out["x_secret"]; ok {
		t.Fatalf("expected key to remain absent")
	}
}

// --- ownership-aware layer: Int64/Bool/StringList analogues ---

func TestDecodeInt64_writeOnlySecretPreservesPrior(t *testing.T) {
	data := map[string]any{"k": 99.0}
	prior := types.Int64Value(5)
	v, diags := decodeInt64(data, "k", ownerWriteOnlySecret, prior)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if v.ValueInt64() != 5 {
		t.Fatalf("expected prior 5 preserved, got %d", v.ValueInt64())
	}
}

func TestDecodeInt64_managedReadsFromData(t *testing.T) {
	data := map[string]any{"k": 42.0}
	v, diags := decodeInt64(data, "k", ownerManaged, types.Int64Null())
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if v.ValueInt64() != 42 {
		t.Fatalf("expected 42, got %d", v.ValueInt64())
	}
}

func TestOverlayInt64_computedWritesNothing(t *testing.T) {
	out := map[string]any{"k": 1.0}
	overlayInt64(out, "k", ownerComputed, types.Int64Value(99))
	if out["k"] != 1.0 {
		t.Fatalf("expected snapshot value untouched, got %v", out["k"])
	}
}

func TestOverlayInt64_managedWrites(t *testing.T) {
	out := map[string]any{"k": 1.0}
	overlayInt64(out, "k", ownerManaged, types.Int64Value(99))
	if out["k"] != float64(99) {
		t.Fatalf("expected 99.0 written, got %v", out["k"])
	}
}

func TestOverlayInt64_writeOnlySecretNullDeletesKey(t *testing.T) {
	out := map[string]any{"k": 1.0}
	overlayInt64(out, "k", ownerWriteOnlySecret, types.Int64Null())
	if _, ok := out["k"]; ok {
		t.Fatalf("expected key deleted for null write-only-secret int64, still present: %v", out["k"])
	}
}

func TestOverlayInt64_writeOnlySecretSetValueWrites(t *testing.T) {
	out := map[string]any{"k": 1.0}
	overlayInt64(out, "k", ownerWriteOnlySecret, types.Int64Value(7))
	if out["k"] != float64(7) {
		t.Fatalf("expected 7.0 written, got %v", out["k"])
	}
}

func TestOverlayInt64_writeOnlySecretUnknownDeletesKey(t *testing.T) {
	out := map[string]any{"k": 1.0}
	overlayInt64(out, "k", ownerWriteOnlySecret, types.Int64Unknown())
	if _, ok := out["k"]; ok {
		t.Fatalf("expected key deleted for unknown write-only-secret int64, still present: %v", out["k"])
	}
}

func TestOverlayInt64_writeOnlySecretZeroValueWritesNotDeletes(t *testing.T) {
	out := map[string]any{"k": "******"}
	overlayInt64(out, "k", ownerWriteOnlySecret, types.Int64Value(0))
	v, ok := out["k"]
	if !ok {
		t.Fatalf("expected key present after configured-zero write-only-secret int64 overlay (intentional write, not delete)")
	}
	if v != float64(0) {
		t.Fatalf("expected 0.0 written, got %v", v)
	}
}

func TestDecodeBool_writeOnlySecretPreservesPrior(t *testing.T) {
	data := map[string]any{"k": false}
	prior := types.BoolValue(true)
	v, diags := decodeBool(data, "k", ownerWriteOnlySecret, prior)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !v.ValueBool() {
		t.Fatalf("expected prior true preserved, got %v", v.ValueBool())
	}
}

func TestDecodeBool_managedReadsFromData(t *testing.T) {
	data := map[string]any{"k": true}
	v, diags := decodeBool(data, "k", ownerManaged, types.BoolNull())
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !v.ValueBool() {
		t.Fatalf("expected true, got %v", v.ValueBool())
	}
}

func TestOverlayBool_computedWritesNothing(t *testing.T) {
	out := map[string]any{"k": true}
	overlayBool(out, "k", ownerComputed, types.BoolValue(false))
	if out["k"] != true {
		t.Fatalf("expected snapshot value untouched, got %v", out["k"])
	}
}

func TestOverlayBool_managedWritesFalse(t *testing.T) {
	out := map[string]any{"k": true}
	overlayBool(out, "k", ownerManaged, types.BoolValue(false))
	if out["k"] != false {
		t.Fatalf("expected false written, got %v", out["k"])
	}
}

func TestOverlayBool_writeOnlySecretNullDeletesKey(t *testing.T) {
	out := map[string]any{"k": true}
	overlayBool(out, "k", ownerWriteOnlySecret, types.BoolNull())
	if _, ok := out["k"]; ok {
		t.Fatalf("expected key deleted for null write-only-secret bool, still present: %v", out["k"])
	}
}

func TestOverlayBool_writeOnlySecretSetValueWrites(t *testing.T) {
	out := map[string]any{"k": true}
	overlayBool(out, "k", ownerWriteOnlySecret, types.BoolValue(false))
	if out["k"] != false {
		t.Fatalf("expected false written, got %v", out["k"])
	}
}

func TestOverlayBool_writeOnlySecretUnknownDeletesKey(t *testing.T) {
	out := map[string]any{"k": true}
	overlayBool(out, "k", ownerWriteOnlySecret, types.BoolUnknown())
	if _, ok := out["k"]; ok {
		t.Fatalf("expected key deleted for unknown write-only-secret bool, still present: %v", out["k"])
	}
}

func TestOverlayBool_writeOnlySecretFalseValueWritesNotDeletes(t *testing.T) {
	out := map[string]any{"k": "******"}
	overlayBool(out, "k", ownerWriteOnlySecret, types.BoolValue(false))
	v, ok := out["k"]
	if !ok {
		t.Fatalf("expected key present after configured-false write-only-secret bool overlay (intentional write, not delete)")
	}
	if v != false {
		t.Fatalf("expected false written, got %v", v)
	}
}

func TestDecodeStringList_writeOnlySecretPreservesPrior(t *testing.T) {
	ctx := context.Background()
	data := map[string]any{"k": []any{"from-api"}}
	prior, diags := types.ListValue(types.StringType, []attr.Value{types.StringValue("prior-a")})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}
	v, decodeDiags := decodeStringList(ctx, data, "k", ownerWriteOnlySecret, prior)
	if decodeDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", decodeDiags)
	}
	elems := v.Elements()
	if len(elems) != 1 || elems[0].(types.String).ValueString() != "prior-a" {
		t.Fatalf("expected prior list preserved, got %v", v)
	}
}

func TestDecodeStringList_managedReadsFromData(t *testing.T) {
	ctx := context.Background()
	data := map[string]any{"k": []any{"a", "b"}}
	v, diags := decodeStringList(ctx, data, "k", ownerManaged, types.ListNull(types.StringType))
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if len(v.Elements()) != 2 {
		t.Fatalf("expected 2 elements read from data, got %d", len(v.Elements()))
	}
}

func TestOverlayStringList_computedWritesNothing(t *testing.T) {
	ctx := context.Background()
	out := map[string]any{"k": []any{"orig"}}
	newList, diags := types.ListValue(types.StringType, []attr.Value{types.StringValue("new")})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}
	overlayDiags := overlayStringList(ctx, out, "k", ownerComputed, newList)
	if overlayDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", overlayDiags)
	}
	list := out["k"].([]any)
	if len(list) != 1 || list[0] != "orig" {
		t.Fatalf("expected snapshot value untouched, got %v", out["k"])
	}
}

func TestOverlayStringList_managedWritesEmptyList(t *testing.T) {
	ctx := context.Background()
	out := map[string]any{"k": []any{"orig"}}
	empty, diags := types.ListValue(types.StringType, []attr.Value{})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}
	overlayDiags := overlayStringList(ctx, out, "k", ownerManaged, empty)
	if overlayDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", overlayDiags)
	}
	list, ok := out["k"].([]any)
	if !ok || len(list) != 0 {
		t.Fatalf("expected empty list written (present-empty), got %v", out["k"])
	}
}

func TestOverlayStringList_writeOnlySecretNullDeletesKey(t *testing.T) {
	ctx := context.Background()
	out := map[string]any{"k": []any{"masked"}}
	overlayDiags := overlayStringList(ctx, out, "k", ownerWriteOnlySecret, types.ListNull(types.StringType))
	if overlayDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", overlayDiags)
	}
	if _, ok := out["k"]; ok {
		t.Fatalf("expected key deleted for null write-only-secret list, still present: %v", out["k"])
	}
}

func TestOverlayStringList_writeOnlySecretUnknownDeletesKey(t *testing.T) {
	ctx := context.Background()
	out := map[string]any{"k": []any{"masked"}}
	overlayDiags := overlayStringList(ctx, out, "k", ownerWriteOnlySecret, types.ListUnknown(types.StringType))
	if overlayDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", overlayDiags)
	}
	if _, ok := out["k"]; ok {
		t.Fatalf("expected key deleted for unknown write-only-secret list, still present: %v", out["k"])
	}
}

func TestOverlayStringList_writeOnlySecretSetValueWrites(t *testing.T) {
	ctx := context.Background()
	out := map[string]any{"k": []any{"masked"}}
	newList, diags := types.ListValue(types.StringType, []attr.Value{types.StringValue("new-a"), types.StringValue("new-b")})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}
	overlayDiags := overlayStringList(ctx, out, "k", ownerWriteOnlySecret, newList)
	if overlayDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", overlayDiags)
	}
	list, ok := out["k"].([]any)
	if !ok || len(list) != 2 || list[0] != "new-a" || list[1] != "new-b" {
		t.Fatalf("expected new list written, got %v", out["k"])
	}
}

func TestOverlayStringList_writeOnlySecretEmptyValueWritesNotDeletes(t *testing.T) {
	ctx := context.Background()
	out := map[string]any{"k": []any{"masked"}}
	empty, diags := types.ListValue(types.StringType, []attr.Value{})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}
	overlayDiags := overlayStringList(ctx, out, "k", ownerWriteOnlySecret, empty)
	if overlayDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", overlayDiags)
	}
	list, ok := out["k"].([]any)
	if !ok {
		t.Fatalf("expected key present after configured-empty write-only-secret list overlay (intentional clear)")
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list written, got %v", list)
	}
}

// --- nested shapes: decodeObject / overlayObject ---

func nestedTestAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"name":   types.StringType,
		"secret": types.StringType,
	}
}

func TestDecodeObject_readsManagedChildrenAndPreservesSecretLeafFromPrior(t *testing.T) {
	ctx := context.Background()
	data := map[string]any{
		"nested": map[string]any{
			"name":   "from-api",
			"secret": "******",
		},
	}
	own := map[string]ownershipClass{
		"nested.name":   ownerManaged,
		"nested.secret": ownerWriteOnlySecret,
	}
	attrTypes := nestedTestAttrTypes()
	priorObj, diags := types.ObjectValue(attrTypes, map[string]attr.Value{
		"name":   types.StringValue("prior-name"),
		"secret": types.StringValue("prior-secret"),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}

	v, decodeDiags := decodeObject(ctx, data, "nested", own, "nested", priorObj, attrTypes)
	if decodeDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", decodeDiags)
	}
	if v.IsNull() {
		t.Fatalf("expected populated object, got null")
	}
	attrs := v.Attributes()
	name, ok := attrs["name"].(types.String)
	if !ok || name.ValueString() != "from-api" {
		t.Fatalf("expected name=from-api (managed reads from data), got %v", attrs["name"])
	}
	secret, ok := attrs["secret"].(types.String)
	if !ok || secret.ValueString() != "prior-secret" {
		t.Fatalf("expected secret=prior-secret (write-only-secret preserves prior), got %v", attrs["secret"])
	}
}

func TestDecodeObject_absentKeyIsNullObject(t *testing.T) {
	ctx := context.Background()
	data := map[string]any{}
	own := map[string]ownershipClass{
		"nested.name":   ownerManaged,
		"nested.secret": ownerWriteOnlySecret,
	}
	attrTypes := nestedTestAttrTypes()
	v, diags := decodeObject(ctx, data, "nested", own, "nested", types.ObjectNull(attrTypes), attrTypes)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !v.IsNull() {
		t.Fatalf("expected null object for absent key, got %v", v)
	}
}

func TestOverlayObject_writesManagedChildAndDeletesSecretLeafOnNull(t *testing.T) {
	ctx := context.Background()
	out := map[string]any{
		"nested": map[string]any{
			"name":   "orig-name",
			"secret": "******",
		},
	}
	own := map[string]ownershipClass{
		"nested.name":   ownerManaged,
		"nested.secret": ownerWriteOnlySecret,
	}
	attrTypes := nestedTestAttrTypes()
	cfg, diags := types.ObjectValue(attrTypes, map[string]attr.Value{
		"name":   types.StringValue("new-name"),
		"secret": types.StringNull(),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}

	overlayDiags := overlayObject(ctx, out, "nested", own, "nested", cfg)
	if overlayDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", overlayDiags)
	}
	nested, ok := out["nested"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested map, got %v", out["nested"])
	}
	if nested["name"] != "new-name" {
		t.Fatalf("expected name overwritten to new-name, got %v", nested["name"])
	}
	if _, ok := nested["secret"]; ok {
		t.Fatalf("expected secret leaf deleted on null config, still present: %v", nested["secret"])
	}
}

func TestOverlayObject_writesManagedChildAndDeletesSecretLeafOnUnknown(t *testing.T) {
	ctx := context.Background()
	out := map[string]any{
		"nested": map[string]any{
			"name":   "orig-name",
			"secret": "******",
		},
	}
	own := map[string]ownershipClass{
		"nested.name":   ownerManaged,
		"nested.secret": ownerWriteOnlySecret,
	}
	attrTypes := nestedTestAttrTypes()
	cfg, diags := types.ObjectValue(attrTypes, map[string]attr.Value{
		"name":   types.StringValue("new-name"),
		"secret": types.StringUnknown(),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}

	overlayDiags := overlayObject(ctx, out, "nested", own, "nested", cfg)
	if overlayDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", overlayDiags)
	}
	nested, ok := out["nested"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested map, got %v", out["nested"])
	}
	if nested["name"] != "new-name" {
		t.Fatalf("expected name overwritten to new-name, got %v", nested["name"])
	}
	if _, ok := nested["secret"]; ok {
		t.Fatalf("expected secret leaf deleted on unknown config, still present: %v", nested["secret"])
	}
}

func TestOverlayObject_nullConfigIsNoop(t *testing.T) {
	ctx := context.Background()
	out := map[string]any{
		"nested": map[string]any{
			"name":   "orig-name",
			"secret": "******",
		},
	}
	own := map[string]ownershipClass{
		"nested.name":   ownerManaged,
		"nested.secret": ownerWriteOnlySecret,
	}
	attrTypes := nestedTestAttrTypes()
	overlayDiags := overlayObject(ctx, out, "nested", own, "nested", types.ObjectNull(attrTypes))
	if overlayDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", overlayDiags)
	}
	nested := out["nested"].(map[string]any)
	if nested["name"] != "orig-name" {
		t.Fatalf("expected snapshot untouched on null object config, got %v", nested["name"])
	}
	if nested["secret"] != "******" {
		t.Fatalf("expected secret leaf untouched on null object config, got %v", nested["secret"])
	}
}

// --- nested shapes: decodeObjectList / overlayObjectList ---

func TestDecodeObjectList_readsElementsAndPreservesSecretLeafFromMatchingPriorElement(t *testing.T) {
	ctx := context.Background()
	data := map[string]any{
		"keys": []any{
			map[string]any{"name": "key-a", "password": "******"},
			map[string]any{"name": "key-b", "password": "******"},
		},
	}
	own := map[string]ownershipClass{
		"keys.name":     ownerManaged,
		"keys.password": ownerWriteOnlySecret,
	}
	attrTypes := map[string]attr.Type{
		"name":     types.StringType,
		"password": types.StringType,
	}
	elemType := types.ObjectType{AttrTypes: attrTypes}

	priorElemA, diags := types.ObjectValue(attrTypes, map[string]attr.Value{
		"name":     types.StringValue("key-a"),
		"password": types.StringValue("prior-pw-a"),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}
	priorElemB, diags := types.ObjectValue(attrTypes, map[string]attr.Value{
		"name":     types.StringValue("key-b"),
		"password": types.StringValue("prior-pw-b"),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}
	prior, diags := types.ListValue(elemType, []attr.Value{priorElemA, priorElemB})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}

	v, decodeDiags := decodeObjectList(ctx, data, "keys", own, "keys", prior, elemType)
	if decodeDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", decodeDiags)
	}
	elems := v.Elements()
	if len(elems) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(elems))
	}
	first := elems[0].(types.Object).Attributes()
	if first["name"].(types.String).ValueString() != "key-a" {
		t.Fatalf("expected first name=key-a, got %v", first["name"])
	}
	if first["password"].(types.String).ValueString() != "prior-pw-a" {
		t.Fatalf("expected first password preserved from matching prior element, got %v", first["password"])
	}
	second := elems[1].(types.Object).Attributes()
	if second["password"].(types.String).ValueString() != "prior-pw-b" {
		t.Fatalf("expected second password preserved from matching prior element, got %v", second["password"])
	}
}

func TestDecodeObjectList_absentKeyIsNullList(t *testing.T) {
	ctx := context.Background()
	data := map[string]any{}
	own := map[string]ownershipClass{"keys.name": ownerManaged}
	attrTypes := map[string]attr.Type{"name": types.StringType}
	elemType := types.ObjectType{AttrTypes: attrTypes}
	v, diags := decodeObjectList(ctx, data, "keys", own, "keys", types.ListNull(elemType), elemType)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !v.IsNull() {
		t.Fatalf("expected null list for absent key, got %v", v)
	}
}

func TestOverlayObjectList_writesElementsAndDeletesSecretLeafOnNullElement(t *testing.T) {
	ctx := context.Background()
	out := map[string]any{
		"keys": []any{
			map[string]any{"name": "key-a", "password": "******"},
		},
	}
	own := map[string]ownershipClass{
		"keys.name":     ownerManaged,
		"keys.password": ownerWriteOnlySecret,
	}
	attrTypes := map[string]attr.Type{
		"name":     types.StringType,
		"password": types.StringType,
	}
	elemType := types.ObjectType{AttrTypes: attrTypes}
	cfgElem, diags := types.ObjectValue(attrTypes, map[string]attr.Value{
		"name":     types.StringValue("key-a-renamed"),
		"password": types.StringNull(),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}
	cfg, diags := types.ListValue(elemType, []attr.Value{cfgElem})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}

	overlayDiags := overlayObjectList(ctx, out, "keys", own, "keys", cfg)
	if overlayDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", overlayDiags)
	}
	list, ok := out["keys"].([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("expected 1-element list, got %v", out["keys"])
	}
	elem := list[0].(map[string]any)
	if elem["name"] != "key-a-renamed" {
		t.Fatalf("expected name overwritten, got %v", elem["name"])
	}
	if _, ok := elem["password"]; ok {
		t.Fatalf("expected password leaf deleted on null element config, still present: %v", elem["password"])
	}
}

func TestOverlayObjectList_writesElementsAndDeletesSecretLeafOnUnknownElement(t *testing.T) {
	ctx := context.Background()
	out := map[string]any{
		"keys": []any{
			map[string]any{"name": "key-a", "password": "******"},
		},
	}
	own := map[string]ownershipClass{
		"keys.name":     ownerManaged,
		"keys.password": ownerWriteOnlySecret,
	}
	attrTypes := map[string]attr.Type{
		"name":     types.StringType,
		"password": types.StringType,
	}
	elemType := types.ObjectType{AttrTypes: attrTypes}
	cfgElem, diags := types.ObjectValue(attrTypes, map[string]attr.Value{
		"name":     types.StringValue("key-a-renamed"),
		"password": types.StringUnknown(),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}
	cfg, diags := types.ListValue(elemType, []attr.Value{cfgElem})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}

	overlayDiags := overlayObjectList(ctx, out, "keys", own, "keys", cfg)
	if overlayDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", overlayDiags)
	}
	list, ok := out["keys"].([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("expected 1-element list, got %v", out["keys"])
	}
	elem := list[0].(map[string]any)
	if elem["name"] != "key-a-renamed" {
		t.Fatalf("expected name overwritten, got %v", elem["name"])
	}
	if _, ok := elem["password"]; ok {
		t.Fatalf("expected password leaf deleted on unknown element config, still present: %v", elem["password"])
	}
}

func TestOverlayObjectList_nullConfigIsNoop(t *testing.T) {
	ctx := context.Background()
	out := map[string]any{
		"keys": []any{
			map[string]any{"name": "key-a", "password": "******"},
		},
	}
	own := map[string]ownershipClass{
		"keys.name":     ownerManaged,
		"keys.password": ownerWriteOnlySecret,
	}
	attrTypes := map[string]attr.Type{
		"name":     types.StringType,
		"password": types.StringType,
	}
	elemType := types.ObjectType{AttrTypes: attrTypes}
	overlayDiags := overlayObjectList(ctx, out, "keys", own, "keys", types.ListNull(elemType))
	if overlayDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", overlayDiags)
	}
	list := out["keys"].([]any)
	elem := list[0].(map[string]any)
	if elem["name"] != "key-a" || elem["password"] != "******" {
		t.Fatalf("expected snapshot untouched on null list config, got %v", elem)
	}
}

// ---------------------------------------------------------------------------
// Task 16b: generalized nested codec — typed leaves + path-prefix recursion.
// ---------------------------------------------------------------------------

// testTrackingAttrTypes/testSuppressionAlertAttrTypes/testDohCustomServerAttrTypes model
// the ips/doh schema shapes described in Task 16b's brief: double nesting
// (suppression_alerts.tracking), int64 leaves (gid/id), and a bool leaf
// (custom_servers.enabled).

func testTrackingAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"direction": types.StringType,
	}
}

func testSuppressionAlertAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"gid":      types.Int64Type,
		"id":       types.Int64Type,
		"tracking": types.ListType{ElemType: types.ObjectType{AttrTypes: testTrackingAttrTypes()}},
	}
}

func testDohCustomServerAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"enabled": types.BoolType,
	}
}

// --- 1. Double-nested path lookup ---

// TestDecodeObjectFields_doubleNestedPathLookupUsesFullDottedPath proves the
// tracking leaf's class is looked up under the full dotted path
// "suppression_alerts.tracking.direction", not "direction" or
// "tracking.direction". A distinctive class (ownerWriteOnlySecret) is given
// ONLY to that full path; its delete-on-null behavior must fire, proving the
// lookup used the full path rather than a short/partial one.
func TestDecodeObjectFields_doubleNestedPathLookupUsesFullDottedPath(t *testing.T) {
	ctx := context.Background()
	own := map[string]ownershipClass{
		"suppression_alerts.gid":                ownerManaged,
		"suppression_alerts.id":                 ownerManaged,
		"suppression_alerts.tracking.direction": ownerWriteOnlySecret,
	}
	attrTypes := testSuppressionAlertAttrTypes()
	prior, diags := types.ObjectValue(attrTypes, map[string]attr.Value{
		"gid": types.Int64Value(1),
		"id":  types.Int64Value(2),
		"tracking": mustListValue(t, types.ObjectType{AttrTypes: testTrackingAttrTypes()}, []attr.Value{
			mustObjectValue(t, testTrackingAttrTypes(), map[string]attr.Value{
				"direction": types.StringValue("prior-direction"),
			}),
		}),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}

	nested := map[string]any{
		"gid": 1.0,
		"id":  2.0,
		"tracking": []any{
			map[string]any{"direction": "from-api"},
		},
	}

	v, decodeDiags := decodeObjectFields(ctx, nested, "suppression_alerts", "suppression_alerts", own, prior, attrTypes)
	if decodeDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", decodeDiags)
	}
	tracking := v.Attributes()["tracking"].(types.List).Elements()
	if len(tracking) != 1 {
		t.Fatalf("expected 1 tracking element, got %d", len(tracking))
	}
	direction := tracking[0].(types.Object).Attributes()["direction"].(types.String)
	if direction.ValueString() != "prior-direction" {
		t.Fatalf("expected tracking.direction preserved from prior (write-only-secret), got %v", direction)
	}
}

// TestDecodeObjectFields_shortKeyOwnershipDoesNotApply proves that an
// ownership map keyed by the SHORT leaf name ("direction") instead of the
// full dotted path does NOT satisfy the lookup for a nested leaf: it must
// hit the missing-ownership-entry diagnostic, not silently apply.
func TestDecodeObjectFields_shortKeyOwnershipDoesNotApply(t *testing.T) {
	ctx := context.Background()
	own := map[string]ownershipClass{
		"suppression_alerts.gid": ownerManaged,
		"suppression_alerts.id":  ownerManaged,
		// Deliberately wrong: short key instead of the full dotted path.
		"direction": ownerWriteOnlySecret,
	}
	attrTypes := testSuppressionAlertAttrTypes()
	nested := map[string]any{
		"gid": 1.0,
		"id":  2.0,
		"tracking": []any{
			map[string]any{"direction": "from-api"},
		},
	}

	_, decodeDiags := decodeObjectFields(ctx, nested, "suppression_alerts", "suppression_alerts", own, types.ObjectNull(attrTypes), attrTypes)
	if !decodeDiags.HasError() {
		t.Fatalf("expected missing-ownership-entry diagnostic for short-keyed map, got none")
	}
}

// --- 2. Type coverage: bool false, int64 0 round-trip ---

func TestDecodeObjectFields_boolFalseAndInt64ZeroAreNotDropped(t *testing.T) {
	ctx := context.Background()
	own := map[string]ownershipClass{
		"custom_servers.enabled": ownerManaged,
	}
	attrTypes := testDohCustomServerAttrTypes()
	nested := map[string]any{"enabled": false}

	v, diags := decodeObjectFields(ctx, nested, "custom_servers", "custom_servers", own, types.ObjectNull(attrTypes), attrTypes)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	enabled, ok := v.Attributes()["enabled"].(types.Bool)
	if !ok || enabled.IsNull() {
		t.Fatalf("expected bool false to decode as a value, got %v", v.Attributes()["enabled"])
	}
	if enabled.ValueBool() != false {
		t.Fatalf("expected enabled=false, got %v", enabled.ValueBool())
	}
}

func TestOverlayObjectFields_boolFalseIsWrittenNotDropped(t *testing.T) {
	ctx := context.Background()
	own := map[string]ownershipClass{
		"custom_servers.enabled": ownerManaged,
	}
	attrTypes := testDohCustomServerAttrTypes()
	cfg, diags := types.ObjectValue(attrTypes, map[string]attr.Value{
		"enabled": types.BoolValue(false),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}

	nested := map[string]any{}
	overlayDiags := overlayObjectFields(ctx, nested, "custom_servers", "custom_servers", own, cfg)
	if overlayDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", overlayDiags)
	}
	v, ok := nested["enabled"].(bool)
	if !ok {
		t.Fatalf("expected enabled key written as bool, got %v (%T)", nested["enabled"], nested["enabled"])
	}
	if v != false {
		t.Fatalf("expected enabled=false written, got %v", v)
	}
}

func TestDecodeObjectFields_int64ZeroIsNotDropped(t *testing.T) {
	ctx := context.Background()
	own := map[string]ownershipClass{
		"suppression_alerts.gid":                ownerManaged,
		"suppression_alerts.id":                 ownerManaged,
		"suppression_alerts.tracking.direction": ownerManaged,
	}
	attrTypes := testSuppressionAlertAttrTypes()
	nested := map[string]any{
		"gid":      0.0,
		"id":       0.0,
		"tracking": []any{},
	}

	v, diags := decodeObjectFields(ctx, nested, "suppression_alerts", "suppression_alerts", own, types.ObjectNull(attrTypes), attrTypes)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	gid, ok := v.Attributes()["gid"].(types.Int64)
	if !ok || gid.IsNull() {
		t.Fatalf("expected int64 0 to decode as a value, got %v", v.Attributes()["gid"])
	}
	if gid.ValueInt64() != 0 {
		t.Fatalf("expected gid=0, got %v", gid.ValueInt64())
	}
}

func TestOverlayObjectFields_int64ZeroLandsAsFloat64(t *testing.T) {
	ctx := context.Background()
	own := map[string]ownershipClass{
		"suppression_alerts.gid":                ownerManaged,
		"suppression_alerts.id":                 ownerManaged,
		"suppression_alerts.tracking.direction": ownerManaged,
	}
	attrTypes := testSuppressionAlertAttrTypes()
	cfg, diags := types.ObjectValue(attrTypes, map[string]attr.Value{
		"gid":      types.Int64Value(0),
		"id":       types.Int64Value(5),
		"tracking": types.ListNull(types.ObjectType{AttrTypes: testTrackingAttrTypes()}),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}

	nested := map[string]any{}
	overlayDiags := overlayObjectFields(ctx, nested, "suppression_alerts", "suppression_alerts", own, cfg)
	if overlayDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", overlayDiags)
	}
	gid, ok := nested["gid"].(float64)
	if !ok {
		t.Fatalf("expected gid written as float64 (per putInt64), got %v (%T)", nested["gid"], nested["gid"])
	}
	if gid != 0 {
		t.Fatalf("expected gid=0, got %v", gid)
	}
	id, ok := nested["id"].(float64)
	if !ok || id != 5 {
		t.Fatalf("expected id=5 as float64, got %v (%T)", nested["id"], nested["id"])
	}
}

// --- 3. List semantics: null/unknown, known-empty, nested known-empty ---

func TestDecodeObjectList_nullListDecodesToNullList(t *testing.T) {
	ctx := context.Background()
	own := map[string]ownershipClass{"custom_servers.enabled": ownerManaged}
	elemType := types.ObjectType{AttrTypes: testDohCustomServerAttrTypes()}
	v, diags := decodeObjectList(ctx, map[string]any{}, "custom_servers", own, "custom_servers", types.ListNull(elemType), elemType)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !v.IsNull() {
		t.Fatalf("expected null list for absent key, got %v", v)
	}
}

func TestOverlayObjectList_unknownConfigIsNoop(t *testing.T) {
	ctx := context.Background()
	own := map[string]ownershipClass{"custom_servers.enabled": ownerManaged}
	elemType := types.ObjectType{AttrTypes: testDohCustomServerAttrTypes()}
	out := map[string]any{
		"custom_servers": []any{
			map[string]any{"enabled": true},
		},
	}
	overlayDiags := overlayObjectList(ctx, out, "custom_servers", own, "custom_servers", types.ListUnknown(elemType))
	if overlayDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", overlayDiags)
	}
	list := out["custom_servers"].([]any)
	elem := list[0].(map[string]any)
	if elem["enabled"] != true {
		t.Fatalf("expected base untouched on unknown list config, got %v", elem)
	}
}

func TestDecodeObjectList_knownEmptyDecodesToEmptyNonNullList(t *testing.T) {
	ctx := context.Background()
	own := map[string]ownershipClass{"custom_servers.enabled": ownerManaged}
	elemType := types.ObjectType{AttrTypes: testDohCustomServerAttrTypes()}
	data := map[string]any{"custom_servers": []any{}}
	v, diags := decodeObjectList(ctx, data, "custom_servers", own, "custom_servers", types.ListNull(elemType), elemType)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if v.IsNull() {
		t.Fatalf("expected non-null empty list for a known-empty array, got null")
	}
	if len(v.Elements()) != 0 {
		t.Fatalf("expected 0 elements, got %d", len(v.Elements()))
	}
}

func TestOverlayObjectList_knownEmptyOverlaysAsEmptyArray(t *testing.T) {
	ctx := context.Background()
	own := map[string]ownershipClass{"custom_servers.enabled": ownerManaged}
	elemType := types.ObjectType{AttrTypes: testDohCustomServerAttrTypes()}
	cfg, diags := types.ListValue(elemType, []attr.Value{})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}
	out := map[string]any{
		"custom_servers": []any{
			map[string]any{"enabled": true},
		},
	}
	overlayDiags := overlayObjectList(ctx, out, "custom_servers", own, "custom_servers", cfg)
	if overlayDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", overlayDiags)
	}
	list, ok := out["custom_servers"].([]any)
	if !ok {
		t.Fatalf("expected custom_servers to remain a []any, got %T", out["custom_servers"])
	}
	if len(list) != 0 {
		t.Fatalf("expected empty array overlay, got %v", list)
	}
}

func TestOverlayObjectFields_knownEmptyNestedTrackingClearsElementTracking(t *testing.T) {
	ctx := context.Background()
	own := map[string]ownershipClass{
		"suppression_alerts.gid":                ownerManaged,
		"suppression_alerts.id":                 ownerManaged,
		"suppression_alerts.tracking.direction": ownerManaged,
	}
	attrTypes := testSuppressionAlertAttrTypes()
	trackingElemType := types.ObjectType{AttrTypes: testTrackingAttrTypes()}
	emptyTracking, diags := types.ListValue(trackingElemType, []attr.Value{})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}
	cfg, diags := types.ObjectValue(attrTypes, map[string]attr.Value{
		"gid":      types.Int64Value(1),
		"id":       types.Int64Value(2),
		"tracking": emptyTracking,
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}

	nested := map[string]any{
		"gid": 1.0,
		"id":  2.0,
		"tracking": []any{
			map[string]any{"direction": "stale"},
		},
	}
	overlayDiags := overlayObjectFields(ctx, nested, "suppression_alerts", "suppression_alerts", own, cfg)
	if overlayDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", overlayDiags)
	}
	tracking, ok := nested["tracking"].([]any)
	if !ok {
		t.Fatalf("expected tracking to remain a []any, got %T", nested["tracking"])
	}
	if len(tracking) != 0 {
		t.Fatalf("expected tracking cleared to empty, got %v", tracking)
	}
}

// --- 4. Ordering + same-index preservation ---

func TestOverlayObjectList_sameIndexElementRetainsBaseFieldOmittedFromConfig(t *testing.T) {
	ctx := context.Background()
	own := map[string]ownershipClass{
		"items.name":  ownerManaged,
		"items.extra": ownerPreservedUnmanaged,
	}
	attrTypes := map[string]attr.Type{"name": types.StringType}
	elemType := types.ObjectType{AttrTypes: attrTypes}

	out := map[string]any{
		"items": []any{
			map[string]any{"name": "orig-a", "extra": "keep-me-a"},
			map[string]any{"name": "orig-b", "extra": "keep-me-b"},
		},
	}
	cfgElemA := mustObjectValue(t, attrTypes, map[string]attr.Value{"name": types.StringValue("new-a")})
	cfgElemB := mustObjectValue(t, attrTypes, map[string]attr.Value{"name": types.StringValue("new-b")})
	cfg := mustListValue(t, elemType, []attr.Value{cfgElemA, cfgElemB})

	overlayDiags := overlayObjectList(ctx, out, "items", own, "items", cfg)
	if overlayDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", overlayDiags)
	}
	list := out["items"].([]any)
	if len(list) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(list))
	}
	elemA := list[0].(map[string]any)
	if elemA["name"] != "new-a" {
		t.Fatalf("expected element 0 name overwritten, got %v", elemA["name"])
	}
	if elemA["extra"] != "keep-me-a" {
		t.Fatalf("expected element 0 to retain base 'extra' field omitted from config, got %v", elemA["extra"])
	}
	elemB := list[1].(map[string]any)
	if elemB["extra"] != "keep-me-b" {
		t.Fatalf("expected element 1 to retain base 'extra' field omitted from config, got %v", elemB["extra"])
	}
}

func TestOverlayObjectList_reorderRemoveIsPositionalNotKeyMatched(t *testing.T) {
	ctx := context.Background()
	own := map[string]ownershipClass{
		"items.name":  ownerManaged,
		"items.extra": ownerPreservedUnmanaged,
	}
	attrTypes := map[string]attr.Type{"name": types.StringType}
	elemType := types.ObjectType{AttrTypes: attrTypes}

	// Base has 3 elements; config has only 1 (a removal). Positional
	// semantics: the single config element lands at index 0 and inherits
	// index 0's base "extra", NOT the base element it shares a "name" with.
	out := map[string]any{
		"items": []any{
			map[string]any{"name": "orig-a", "extra": "extra-a"},
			map[string]any{"name": "orig-b", "extra": "extra-b"},
			map[string]any{"name": "orig-c", "extra": "extra-c"},
		},
	}
	cfgElem := mustObjectValue(t, attrTypes, map[string]attr.Value{"name": types.StringValue("orig-c")})
	cfg := mustListValue(t, elemType, []attr.Value{cfgElem})

	overlayDiags := overlayObjectList(ctx, out, "items", own, "items", cfg)
	if overlayDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", overlayDiags)
	}
	list := out["items"].([]any)
	if len(list) != 1 {
		t.Fatalf("expected 1 element after removal, got %d", len(list))
	}
	elem := list[0].(map[string]any)
	if elem["name"] != "orig-c" {
		t.Fatalf("expected name=orig-c from config, got %v", elem["name"])
	}
	if elem["extra"] != "extra-a" {
		t.Fatalf("expected positional (index-0) base preservation to pull 'extra-a', got %v — same-index semantics violated", elem["extra"])
	}
}

func TestDecodeObjectList_apiResponseOrderPreserved(t *testing.T) {
	ctx := context.Background()
	own := map[string]ownershipClass{"items.name": ownerManaged}
	attrTypes := map[string]attr.Type{"name": types.StringType}
	elemType := types.ObjectType{AttrTypes: attrTypes}
	data := map[string]any{
		"items": []any{
			map[string]any{"name": "third"},
			map[string]any{"name": "first"},
			map[string]any{"name": "second"},
		},
	}
	v, diags := decodeObjectList(ctx, data, "items", own, "items", types.ListNull(elemType), elemType)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	elems := v.Elements()
	want := []string{"third", "first", "second"}
	for i, w := range want {
		got := elems[i].(types.Object).Attributes()["name"].(types.String).ValueString()
		if got != w {
			t.Fatalf("element %d: expected %q (API order preserved), got %q", i, w, got)
		}
	}
}

// --- 5. No snapshot mutation ---

func TestOverlayObjectList_doesNotMutateSourceSnapshot(t *testing.T) {
	ctx := context.Background()
	own := map[string]ownershipClass{
		"suppression_alerts.gid":                ownerManaged,
		"suppression_alerts.id":                 ownerManaged,
		"suppression_alerts.tracking.direction": ownerManaged,
	}
	attrTypes := testSuppressionAlertAttrTypes()
	elemType := types.ObjectType{AttrTypes: attrTypes}

	rawData := map[string]any{
		"suppression_alerts": []any{
			map[string]any{
				"gid": 1.0,
				"id":  2.0,
				"tracking": []any{
					map[string]any{"direction": "both"},
				},
			},
		},
	}
	snap := newRawSettings([]settings.RawSetting{
		{BaseSetting: settings.BaseSetting{Key: "ips"}, Data: rawData},
	})

	// Snapshot the original Data (deep copy) before any overlay to compare
	// against afterward.
	originalData := deepCopyAny(rawData).(map[string]any)

	base := snap.dataCopy("ips")
	cfgElem := mustObjectValue(t, attrTypes, map[string]attr.Value{
		"gid": types.Int64Value(99),
		"id":  types.Int64Value(100),
		"tracking": mustListValue(t, types.ObjectType{AttrTypes: testTrackingAttrTypes()}, []attr.Value{
			mustObjectValue(t, testTrackingAttrTypes(), map[string]attr.Value{"direction": types.StringValue("egress")}),
		}),
	})
	cfg := mustListValue(t, elemType, []attr.Value{cfgElem})

	overlayDiags := overlayObjectList(ctx, base, "suppression_alerts", own, "suppression_alerts", cfg)
	if overlayDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", overlayDiags)
	}

	// The overlay must have actually changed `base` (sanity: prove the test
	// isn't vacuously true because overlay no-oped).
	if reflect.DeepEqual(base["suppression_alerts"], originalData["suppression_alerts"]) {
		t.Fatalf("test fixture invalid: overlay did not change base at all")
	}

	afterSec, ok := snap.section("ips")
	if !ok {
		t.Fatalf("expected ips section present in snapshot")
	}
	afterData := afterSec.Data
	if !reflect.DeepEqual(afterData, originalData) {
		t.Fatalf("overlay mutated the source snapshot's Data.\nbefore: %#v\nafter:  %#v", originalData, afterData)
	}
}

// --- 6. Malformed shapes -> diagnostics ---

func TestDecodeObjectList_outerNotArrayIsDiagnostic(t *testing.T) {
	ctx := context.Background()
	own := map[string]ownershipClass{"items.name": ownerManaged}
	attrTypes := map[string]attr.Type{"name": types.StringType}
	elemType := types.ObjectType{AttrTypes: attrTypes}
	data := map[string]any{"items": "not-an-array"}
	_, diags := decodeObjectList(ctx, data, "items", own, "items", types.ListNull(elemType), elemType)
	if !diags.HasError() {
		t.Fatalf("expected diagnostic for non-array outer value, got none")
	}
}

func TestDecodeObjectList_elementNotObjectIsDiagnostic(t *testing.T) {
	ctx := context.Background()
	own := map[string]ownershipClass{"items.name": ownerManaged}
	attrTypes := map[string]attr.Type{"name": types.StringType}
	elemType := types.ObjectType{AttrTypes: attrTypes}
	data := map[string]any{"items": []any{"not-an-object"}}
	_, diags := decodeObjectList(ctx, data, "items", own, "items", types.ListNull(elemType), elemType)
	if !diags.HasError() {
		t.Fatalf("expected diagnostic for non-object element, got none")
	}
}

func TestDecodeObjectFields_boolLeafWithStringRawValueIsDiagnostic(t *testing.T) {
	ctx := context.Background()
	own := map[string]ownershipClass{"custom_servers.enabled": ownerManaged}
	attrTypes := testDohCustomServerAttrTypes()
	nested := map[string]any{"enabled": "not-a-bool"}
	_, diags := decodeObjectFields(ctx, nested, "custom_servers", "custom_servers", own, types.ObjectNull(attrTypes), attrTypes)
	if !diags.HasError() {
		t.Fatalf("expected diagnostic for bool leaf with string raw value, got none")
	}
}

func TestDecodeObjectFields_int64LeafWithFractionalValueIsDiagnostic(t *testing.T) {
	ctx := context.Background()
	own := map[string]ownershipClass{
		"suppression_alerts.gid":                ownerManaged,
		"suppression_alerts.id":                 ownerManaged,
		"suppression_alerts.tracking.direction": ownerManaged,
	}
	attrTypes := testSuppressionAlertAttrTypes()
	nested := map[string]any{
		"gid":      1.9,
		"id":       2.0,
		"tracking": []any{},
	}
	_, diags := decodeObjectFields(ctx, nested, "suppression_alerts", "suppression_alerts", own, types.ObjectNull(attrTypes), attrTypes)
	if !diags.HasError() {
		t.Fatalf("expected diagnostic for fractional int64 leaf, got none")
	}
}

func TestDecodeObjectFields_malformedLeafYieldsDiagnosticNotPartialOutput(t *testing.T) {
	ctx := context.Background()
	own := map[string]ownershipClass{"custom_servers.enabled": ownerManaged}
	attrTypes := testDohCustomServerAttrTypes()
	nested := map[string]any{"enabled": "not-a-bool"}
	v, diags := decodeObjectFields(ctx, nested, "custom_servers", "custom_servers", own, types.ObjectNull(attrTypes), attrTypes)
	if !diags.HasError() {
		t.Fatalf("expected diagnostic, got none")
	}
	if !v.IsUnknown() {
		t.Fatalf("expected Unknown object (no partial output) on malformed leaf, got %v", v)
	}
}

// --- 7. Missing-ownership-entry -> diagnostic ---

func TestDecodeObjectFields_missingOwnershipEntryIsDiagnosticNotSilentManaged(t *testing.T) {
	ctx := context.Background()
	// "custom_servers.enabled" is deliberately absent from own.
	own := map[string]ownershipClass{}
	attrTypes := testDohCustomServerAttrTypes()
	nested := map[string]any{"enabled": true}
	_, diags := decodeObjectFields(ctx, nested, "custom_servers", "custom_servers", own, types.ObjectNull(attrTypes), attrTypes)
	if !diags.HasError() {
		t.Fatalf("expected missing-ownership-entry diagnostic, got none (would silently default to ownerManaged)")
	}
}

func TestOverlayObjectFields_missingOwnershipEntryIsDiagnosticNotSilentManaged(t *testing.T) {
	ctx := context.Background()
	own := map[string]ownershipClass{}
	attrTypes := testDohCustomServerAttrTypes()
	cfg := mustObjectValue(t, attrTypes, map[string]attr.Value{"enabled": types.BoolValue(true)})
	nested := map[string]any{}
	diags := overlayObjectFields(ctx, nested, "custom_servers", "custom_servers", own, cfg)
	if !diags.HasError() {
		t.Fatalf("expected missing-ownership-entry diagnostic, got none")
	}
	if _, ok := nested["enabled"]; ok {
		t.Fatalf("expected no write to occur when ownership lookup fails, got %v", nested["enabled"])
	}
}

// --- 8. Nested write-only-secret (future-proofing) ---

func TestDecodeObjectList_nestedWriteOnlySecretPreservesPriorNeverReadsData(t *testing.T) {
	ctx := context.Background()
	own := map[string]ownershipClass{
		"some_list.token": ownerWriteOnlySecret,
	}
	attrTypes := map[string]attr.Type{"token": types.StringType}
	elemType := types.ObjectType{AttrTypes: attrTypes}
	priorElem := mustObjectValue(t, attrTypes, map[string]attr.Value{"token": types.StringValue("prior-token")})
	prior := mustListValue(t, elemType, []attr.Value{priorElem})

	data := map[string]any{
		"some_list": []any{
			map[string]any{"token": "masked-value-from-api"},
		},
	}
	v, diags := decodeObjectList(ctx, data, "some_list", own, "some_list", prior, elemType)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	token := v.Elements()[0].(types.Object).Attributes()["token"].(types.String)
	if token.ValueString() != "prior-token" {
		t.Fatalf("expected token preserved from prior (never read masked API value), got %v", token)
	}
}

func TestOverlayObjectList_nestedWriteOnlySecretDeletesOnNullAndUnknownWritesOnSet(t *testing.T) {
	ctx := context.Background()
	own := map[string]ownershipClass{
		"some_list.token": ownerWriteOnlySecret,
	}
	attrTypes := map[string]attr.Type{"token": types.StringType}
	elemType := types.ObjectType{AttrTypes: attrTypes}

	t.Run("null deletes", func(t *testing.T) {
		out := map[string]any{
			"some_list": []any{
				map[string]any{"token": "stale-masked"},
			},
		}
		cfgElem := mustObjectValue(t, attrTypes, map[string]attr.Value{"token": types.StringNull()})
		cfg := mustListValue(t, elemType, []attr.Value{cfgElem})
		diags := overlayObjectList(ctx, out, "some_list", own, "some_list", cfg)
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		elem := out["some_list"].([]any)[0].(map[string]any)
		if _, ok := elem["token"]; ok {
			t.Fatalf("expected token deleted on null config, still present: %v", elem["token"])
		}
	})

	t.Run("unknown deletes", func(t *testing.T) {
		out := map[string]any{
			"some_list": []any{
				map[string]any{"token": "stale-masked"},
			},
		}
		cfgElem := mustObjectValue(t, attrTypes, map[string]attr.Value{"token": types.StringUnknown()})
		cfg := mustListValue(t, elemType, []attr.Value{cfgElem})
		diags := overlayObjectList(ctx, out, "some_list", own, "some_list", cfg)
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		elem := out["some_list"].([]any)[0].(map[string]any)
		if _, ok := elem["token"]; ok {
			t.Fatalf("expected token deleted on unknown config, still present: %v", elem["token"])
		}
	})

	t.Run("set value writes", func(t *testing.T) {
		out := map[string]any{
			"some_list": []any{
				map[string]any{"token": "stale-masked"},
			},
		}
		cfgElem := mustObjectValue(t, attrTypes, map[string]attr.Value{"token": types.StringValue("new-token")})
		cfg := mustListValue(t, elemType, []attr.Value{cfgElem})
		diags := overlayObjectList(ctx, out, "some_list", own, "some_list", cfg)
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		elem := out["some_list"].([]any)[0].(map[string]any)
		if elem["token"] != "new-token" {
			t.Fatalf("expected token written for set config value, got %v", elem["token"])
		}
	})

	// per-element isolation across MULTIPLE list elements: one overlay call,
	// element 0's secret goes null (delete-on-null) while element 1's secret
	// is set (write-on-set), in the SAME call. This is the case the
	// single-element subtests above cannot exercise: overlayObjectList seeds
	// each element's elemOut from out[key][i] specifically (setting_codec.go
	// "elemOut = m" in the per-index loop), one map per index. A naive
	// implementation that shared a single map/pointer across elements — e.g.
	// reusing baseElems[0] for every index, or seeding elemOut once outside
	// the loop — would leak element 1's write into element 0's map (or vice
	// versa), and this subtest's cross-element assertions below would catch
	// that: element 0 would unexpectedly gain a "token" key (from element
	// 1's write landing in the shared map) or element 1's "label" would read
	// back as element 0's stale "label-a" instead of "label-b".
	t.Run("per-element isolation across two elements in one call", func(t *testing.T) {
		multiOwn := map[string]ownershipClass{
			"multi_list.token": ownerWriteOnlySecret,
			"multi_list.label": ownerManaged,
		}
		multiAttrTypes := map[string]attr.Type{
			"token": types.StringType,
			"label": types.StringType,
		}
		multiElemType := types.ObjectType{AttrTypes: multiAttrTypes}

		out := map[string]any{
			"multi_list": []any{
				map[string]any{"token": "stale-masked-a", "label": "label-a"},
				map[string]any{"token": "stale-masked-b", "label": "label-b"},
			},
		}
		// element 0: secret left null (delete), label omitted from config
		// change (still ownerManaged so it's overwritten by cfg's value).
		cfgElem0 := mustObjectValue(t, multiAttrTypes, map[string]attr.Value{
			"token": types.StringNull(),
			"label": types.StringValue("label-a"),
		})
		// element 1: secret set to a new value.
		cfgElem1 := mustObjectValue(t, multiAttrTypes, map[string]attr.Value{
			"token": types.StringValue("new-token-b"),
			"label": types.StringValue("label-b"),
		})
		cfg := mustListValue(t, multiElemType, []attr.Value{cfgElem0, cfgElem1})

		diags := overlayObjectList(ctx, out, "multi_list", multiOwn, "multi_list", cfg)
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		list := out["multi_list"].([]any)
		if len(list) != 2 {
			t.Fatalf("expected 2 elements, got %d", len(list))
		}

		elem0 := list[0].(map[string]any)
		if _, ok := elem0["token"]; ok {
			t.Fatalf("expected element 0's masked token deleted on null config, still present: %v", elem0["token"])
		}
		if elem0["label"] != "label-a" {
			t.Fatalf("expected element 0's own (non-secret) base field to survive untouched, got %v", elem0["label"])
		}

		elem1 := list[1].(map[string]any)
		if elem1["token"] != "new-token-b" {
			t.Fatalf("expected element 1's token replaced with the set config value, got %v", elem1["token"])
		}
		if elem1["label"] != "label-b" {
			t.Fatalf("expected element 1's own (non-secret) base field to survive untouched, got %v", elem1["label"])
		}

		// Cross-element non-interference, stated explicitly: element 1's
		// write must not bleed into element 0, and element 0's delete must
		// not bleed into element 1.
		if elem0["token"] == "new-token-b" {
			t.Fatalf("element 1's written token leaked into element 0")
		}
		if elem1["token"] == "stale-masked-a" {
			t.Fatalf("element 0's stale masked token leaked into element 1")
		}
	})
}

// --- 9. Round trips ---

func TestDecodeObjectList_dohCustomServerRoundTripWithFalseEnabled(t *testing.T) {
	ctx := context.Background()
	own := map[string]ownershipClass{"custom_servers.enabled": ownerManaged}
	attrTypes := testDohCustomServerAttrTypes()
	elemType := types.ObjectType{AttrTypes: attrTypes}
	data := map[string]any{
		"custom_servers": []any{
			map[string]any{"enabled": false},
		},
	}
	v, diags := decodeObjectList(ctx, data, "custom_servers", own, "custom_servers", types.ListNull(elemType), elemType)
	if diags.HasError() {
		t.Fatalf("unexpected decode diagnostics: %v", diags)
	}
	elems := v.Elements()
	if len(elems) != 1 {
		t.Fatalf("expected 1 element, got %d", len(elems))
	}
	enabled := elems[0].(types.Object).Attributes()["enabled"].(types.Bool)
	if enabled.IsNull() || enabled.ValueBool() != false {
		t.Fatalf("expected enabled=false decoded, got %v", enabled)
	}

	out := map[string]any{}
	overlayDiags := overlayObjectList(ctx, out, "custom_servers", own, "custom_servers", v)
	if overlayDiags.HasError() {
		t.Fatalf("unexpected overlay diagnostics: %v", overlayDiags)
	}
	list := out["custom_servers"].([]any)
	elem := list[0].(map[string]any)
	if b, ok := elem["enabled"].(bool); !ok || b != false {
		t.Fatalf("expected enabled=false overlaid, got %v (%T)", elem["enabled"], elem["enabled"])
	}
}

func TestDecodeObjectList_ipsSuppressionAlertsRoundTripWithMultipleTrackingEntriesOnBothElements(t *testing.T) {
	ctx := context.Background()
	own := map[string]ownershipClass{
		"suppression_alerts.gid":                ownerManaged,
		"suppression_alerts.id":                 ownerManaged,
		"suppression_alerts.tracking.direction": ownerManaged,
	}
	attrTypes := testSuppressionAlertAttrTypes()
	elemType := types.ObjectType{AttrTypes: attrTypes}
	data := map[string]any{
		"suppression_alerts": []any{
			map[string]any{
				"gid": 1.0,
				"id":  100.0,
				"tracking": []any{
					map[string]any{"direction": "ingress"},
					map[string]any{"direction": "egress"},
				},
			},
			map[string]any{
				"gid": 2.0,
				"id":  200.0,
				"tracking": []any{
					map[string]any{"direction": "both"},
					map[string]any{"direction": "egress"},
				},
			},
		},
	}

	v, diags := decodeObjectList(ctx, data, "suppression_alerts", own, "suppression_alerts", types.ListNull(elemType), elemType)
	if diags.HasError() {
		t.Fatalf("unexpected decode diagnostics: %v", diags)
	}
	elems := v.Elements()
	if len(elems) != 2 {
		t.Fatalf("expected 2 alert elements, got %d", len(elems))
	}

	first := elems[0].(types.Object).Attributes()
	if first["gid"].(types.Int64).ValueInt64() != 1 || first["id"].(types.Int64).ValueInt64() != 100 {
		t.Fatalf("expected first alert gid=1/id=100, got gid=%v id=%v", first["gid"], first["id"])
	}
	firstTracking := first["tracking"].(types.List).Elements()
	if len(firstTracking) != 2 {
		t.Fatalf("expected first alert to have 2 tracking entries, got %d", len(firstTracking))
	}
	if firstTracking[0].(types.Object).Attributes()["direction"].(types.String).ValueString() != "ingress" {
		t.Fatalf("expected first tracking entry order preserved (ingress first), got %v", firstTracking[0])
	}
	if firstTracking[1].(types.Object).Attributes()["direction"].(types.String).ValueString() != "egress" {
		t.Fatalf("expected second tracking entry order preserved (egress second), got %v", firstTracking[1])
	}

	second := elems[1].(types.Object).Attributes()
	if second["gid"].(types.Int64).ValueInt64() != 2 || second["id"].(types.Int64).ValueInt64() != 200 {
		t.Fatalf("expected second alert gid=2/id=200, got gid=%v id=%v", second["gid"], second["id"])
	}
	secondTracking := second["tracking"].(types.List).Elements()
	if len(secondTracking) != 2 {
		t.Fatalf("expected second alert to have 2 tracking entries, got %d", len(secondTracking))
	}
	if secondTracking[0].(types.Object).Attributes()["direction"].(types.String).ValueString() != "both" {
		t.Fatalf("expected second alert's first tracking entry order preserved (both first), got %v", secondTracking[0])
	}
	if secondTracking[1].(types.Object).Attributes()["direction"].(types.String).ValueString() != "egress" {
		t.Fatalf("expected second alert's second tracking entry order preserved (egress second), got %v", secondTracking[1])
	}

	out := map[string]any{}
	overlayDiags := overlayObjectList(ctx, out, "suppression_alerts", own, "suppression_alerts", v)
	if overlayDiags.HasError() {
		t.Fatalf("unexpected overlay diagnostics: %v", overlayDiags)
	}
	list := out["suppression_alerts"].([]any)
	if len(list) != 2 {
		t.Fatalf("expected 2 elements overlaid, got %d", len(list))
	}
	e0 := list[0].(map[string]any)
	if e0["gid"] != float64(1) || e0["id"] != float64(100) {
		t.Fatalf("expected element 0 gid=1/id=100 as float64, got gid=%v id=%v", e0["gid"], e0["id"])
	}
	e0Tracking := e0["tracking"].([]any)
	if len(e0Tracking) != 2 {
		t.Fatalf("expected element 0 tracking to have 2 entries after overlay, got %d", len(e0Tracking))
	}
	if e0Tracking[0].(map[string]any)["direction"] != "ingress" || e0Tracking[1].(map[string]any)["direction"] != "egress" {
		t.Fatalf("expected element 0 tracking order preserved on overlay, got %v", e0Tracking)
	}
	e1 := list[1].(map[string]any)
	if e1["gid"] != float64(2) || e1["id"] != float64(200) {
		t.Fatalf("expected element 1 gid=2/id=200 as float64, got gid=%v id=%v", e1["gid"], e1["id"])
	}
	e1Tracking := e1["tracking"].([]any)
	if len(e1Tracking) != 2 {
		t.Fatalf("expected element 1 tracking to have 2 entries after overlay, got %d", len(e1Tracking))
	}
	if e1Tracking[0].(map[string]any)["direction"] != "both" || e1Tracking[1].(map[string]any)["direction"] != "egress" {
		t.Fatalf("expected element 1 tracking order preserved on overlay, got %v", e1Tracking)
	}
}

// --- test helpers ---

func mustObjectValue(t *testing.T, attrTypes map[string]attr.Type, attrs map[string]attr.Value) types.Object {
	t.Helper()
	v, diags := types.ObjectValue(attrTypes, attrs)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building object fixture: %v", diags)
	}
	return v
}

func mustListValue(t *testing.T, elemType attr.Type, elems []attr.Value) types.List {
	t.Helper()
	v, diags := types.ListValue(elemType, elems)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building list fixture: %v", diags)
	}
	return v
}
