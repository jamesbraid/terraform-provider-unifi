package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
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
	childOwnership := map[string]ownershipClass{
		"name":   ownerManaged,
		"secret": ownerWriteOnlySecret,
	}
	attrTypes := nestedTestAttrTypes()
	priorObj, diags := types.ObjectValue(attrTypes, map[string]attr.Value{
		"name":   types.StringValue("prior-name"),
		"secret": types.StringValue("prior-secret"),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}

	v, decodeDiags := decodeObject(ctx, data, "nested", childOwnership, priorObj, attrTypes)
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
	childOwnership := map[string]ownershipClass{
		"name":   ownerManaged,
		"secret": ownerWriteOnlySecret,
	}
	attrTypes := nestedTestAttrTypes()
	v, diags := decodeObject(ctx, data, "nested", childOwnership, types.ObjectNull(attrTypes), attrTypes)
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
	childOwnership := map[string]ownershipClass{
		"name":   ownerManaged,
		"secret": ownerWriteOnlySecret,
	}
	attrTypes := nestedTestAttrTypes()
	cfg, diags := types.ObjectValue(attrTypes, map[string]attr.Value{
		"name":   types.StringValue("new-name"),
		"secret": types.StringNull(),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}

	overlayDiags := overlayObject(ctx, out, "nested", childOwnership, cfg)
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
	childOwnership := map[string]ownershipClass{
		"name":   ownerManaged,
		"secret": ownerWriteOnlySecret,
	}
	attrTypes := nestedTestAttrTypes()
	cfg, diags := types.ObjectValue(attrTypes, map[string]attr.Value{
		"name":   types.StringValue("new-name"),
		"secret": types.StringUnknown(),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}

	overlayDiags := overlayObject(ctx, out, "nested", childOwnership, cfg)
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
	childOwnership := map[string]ownershipClass{
		"name":   ownerManaged,
		"secret": ownerWriteOnlySecret,
	}
	attrTypes := nestedTestAttrTypes()
	overlayDiags := overlayObject(ctx, out, "nested", childOwnership, types.ObjectNull(attrTypes))
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
	elemOwnership := map[string]ownershipClass{
		"name":     ownerManaged,
		"password": ownerWriteOnlySecret,
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

	v, decodeDiags := decodeObjectList(ctx, data, "keys", elemOwnership, prior, elemType)
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
	elemOwnership := map[string]ownershipClass{"name": ownerManaged}
	attrTypes := map[string]attr.Type{"name": types.StringType}
	elemType := types.ObjectType{AttrTypes: attrTypes}
	v, diags := decodeObjectList(ctx, data, "keys", elemOwnership, types.ListNull(elemType), elemType)
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
	elemOwnership := map[string]ownershipClass{
		"name":     ownerManaged,
		"password": ownerWriteOnlySecret,
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

	overlayDiags := overlayObjectList(ctx, out, "keys", elemOwnership, cfg)
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
	elemOwnership := map[string]ownershipClass{
		"name":     ownerManaged,
		"password": ownerWriteOnlySecret,
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

	overlayDiags := overlayObjectList(ctx, out, "keys", elemOwnership, cfg)
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
	elemOwnership := map[string]ownershipClass{
		"name":     ownerManaged,
		"password": ownerWriteOnlySecret,
	}
	attrTypes := map[string]attr.Type{
		"name":     types.StringType,
		"password": types.StringType,
	}
	elemType := types.ObjectType{AttrTypes: attrTypes}
	overlayDiags := overlayObjectList(ctx, out, "keys", elemOwnership, types.ListNull(elemType))
	if overlayDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", overlayDiags)
	}
	list := out["keys"].([]any)
	elem := list[0].(map[string]any)
	if elem["name"] != "key-a" || elem["password"] != "******" {
		t.Fatalf("expected snapshot untouched on null list config, got %v", elem)
	}
}
