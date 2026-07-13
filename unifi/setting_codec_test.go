package unifi

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// hasWarning reports whether diags contains at least one SeverityWarning
// diagnostic, mirroring diag.Diagnostics.HasError()'s shape for the warning
// severity that remote type-drift tolerance (Task 1) relies on.
func hasWarning(diags diag.Diagnostics) bool {
	for _, d := range diags {
		if d.Severity() == diag.SeverityWarning {
			return true
		}
	}
	return false
}

// --- low-level codec: codecString ---

func TestCodecString_presentEmptyIsValueNotNull(t *testing.T) {
	v, diags := codecString(map[string]any{"k": ""}, "k", types.StringNull())
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
	v, diags := codecString(map[string]any{}, "k", types.StringNull())
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !v.IsNull() {
		t.Fatalf("expected null for absent key, got %v", v)
	}
}

func TestCodecString_explicitNullIsNull(t *testing.T) {
	v, diags := codecString(map[string]any{"k": nil}, "k", types.StringNull())
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !v.IsNull() {
		t.Fatalf("expected null for explicit-null key, got %v", v)
	}
}

func TestCodecString_typeDriftWarnsAndRetainsPrior(t *testing.T) {
	prior := types.StringValue("prior-value")
	v, diags := codecString(map[string]any{"k": 42.0}, "k", prior)
	if diags.HasError() {
		t.Fatalf("type drift must not be an error, got: %v", diags)
	}
	if !hasWarning(diags) {
		t.Fatalf("type drift must produce a warning")
	}
	if !v.Equal(prior) {
		t.Fatalf("type drift must retain prior %v, got %v", prior, v)
	}
}

func TestCodecString_absenceYieldsNullNotPrior(t *testing.T) {
	v, diags := codecString(map[string]any{}, "k", types.StringValue("prior-value"))
	if diags.HasError() || !v.IsNull() {
		t.Fatalf("absence must clear to null (not retain prior); got %v %v", v, diags)
	}
}

func TestCodecString_presentValue(t *testing.T) {
	v, diags := codecString(map[string]any{"k": "hello"}, "k", types.StringNull())
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if v.ValueString() != "hello" {
		t.Fatalf("expected \"hello\", got %q", v.ValueString())
	}
}

// --- low-level codec: codecBool ---

func TestCodecBool_presentValue(t *testing.T) {
	v, diags := codecBool(map[string]any{"k": true}, "k", types.BoolNull())
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if v.IsNull() || !v.ValueBool() {
		t.Fatalf("expected true, got %v", v)
	}
}

func TestCodecBool_absentIsNull(t *testing.T) {
	v, diags := codecBool(map[string]any{}, "k", types.BoolNull())
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !v.IsNull() {
		t.Fatalf("expected null for absent key, got %v", v)
	}
}

func TestCodecBool_typeDriftWarnsAndRetainsPrior(t *testing.T) {
	data := map[string]any{"enabled": "true"} // controller returned a STRING for a bool field
	prior := types.BoolValue(true)
	got, diags := codecBool(data, "enabled", prior)
	if diags.HasError() {
		t.Fatalf("type drift must not be an error, got: %v", diags)
	}
	if !hasWarning(diags) {
		t.Fatalf("type drift must produce a warning")
	}
	if !got.Equal(prior) {
		t.Fatalf("type drift must retain prior %v, got %v", prior, got)
	}
}

func TestCodecBool_typeDriftNullPriorYieldsNull(t *testing.T) {
	data := map[string]any{"enabled": "true"}
	got, diags := codecBool(data, "enabled", types.BoolNull()) // import: no prior
	if diags.HasError() || !got.IsNull() {
		t.Fatalf("type drift with null prior must yield null, no error; got %v %v", got, diags)
	}
}

func TestCodecBool_absenceYieldsNullNotPrior(t *testing.T) {
	got, diags := codecBool(map[string]any{}, "enabled", types.BoolValue(true)) // key absent
	if diags.HasError() || !got.IsNull() {
		t.Fatalf("absence must clear to null (not retain prior); got %v %v", got, diags)
	}
}

// --- low-level codec: codecInt64 ---

func TestCodecInt64_presentValue(t *testing.T) {
	v, diags := codecInt64(map[string]any{"k": 42.0}, "k", types.Int64Null())
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if v.ValueInt64() != 42 {
		t.Fatalf("expected 42, got %d", v.ValueInt64())
	}
}

func TestCodecInt64_absentIsNull(t *testing.T) {
	v, diags := codecInt64(map[string]any{}, "k", types.Int64Null())
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !v.IsNull() {
		t.Fatalf("expected null for absent key, got %v", v)
	}
}

func TestCodecInt64_fractionalWarnsAndRetainsPrior(t *testing.T) {
	prior := types.Int64Value(3)
	v, diags := codecInt64(map[string]any{"k": 1.9}, "k", prior)
	if diags.HasError() {
		t.Fatalf("fractional value must not be an error, got: %v", diags)
	}
	if !hasWarning(diags) {
		t.Fatalf("fractional value must produce a warning")
	}
	if !v.Equal(prior) {
		t.Fatalf("fractional value must retain prior %v, got %v (must not silently truncate)", prior, v)
	}
}

func TestCodecInt64_typeDriftWarnsAndRetainsPrior(t *testing.T) {
	prior := types.Int64Value(3)
	v, diags := codecInt64(map[string]any{"k": "nope"}, "k", prior)
	if diags.HasError() {
		t.Fatalf("type drift must not be an error, got: %v", diags)
	}
	if !hasWarning(diags) {
		t.Fatalf("type drift must produce a warning")
	}
	if !v.Equal(prior) {
		t.Fatalf("type drift must retain prior %v, got %v", prior, v)
	}
}

func TestCodecInt64_absenceYieldsNullNotPrior(t *testing.T) {
	v, diags := codecInt64(map[string]any{}, "k", types.Int64Value(3))
	if diags.HasError() || !v.IsNull() {
		t.Fatalf("absence must clear to null (not retain prior); got %v %v", v, diags)
	}
}

// --- low-level codec: codecStringList ---

func TestCodecStringList_presentEmptyIsValueNotNull(t *testing.T) {
	ctx := context.Background()
	v, diags := codecStringList(ctx, map[string]any{"k": []any{}}, "k", types.ListNull(types.StringType))
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
	v, diags := codecStringList(ctx, map[string]any{}, "k", types.ListNull(types.StringType))
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !v.IsNull() {
		t.Fatalf("expected null for absent key, got %v", v)
	}
}

func TestCodecStringList_presentValues(t *testing.T) {
	ctx := context.Background()
	v, diags := codecStringList(ctx, map[string]any{"k": []any{"a", "b"}}, "k", types.ListNull(types.StringType))
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	elems := v.Elements()
	if len(elems) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(elems))
	}
}

func TestCodecStringList_wrongElementTypeWarnsAndRetainsPrior(t *testing.T) {
	ctx := context.Background()
	prior, priorDiags := types.ListValue(types.StringType, []attr.Value{types.StringValue("prior")})
	if priorDiags.HasError() {
		t.Fatalf("unexpected diagnostics building prior: %v", priorDiags)
	}
	v, diags := codecStringList(ctx, map[string]any{"k": []any{"a", 5.0}}, "k", prior)
	if diags.HasError() {
		t.Fatalf("wrong element type must not be an error, got: %v", diags)
	}
	if !hasWarning(diags) {
		t.Fatalf("wrong element type must produce a warning")
	}
	if !v.Equal(prior) {
		t.Fatalf("wrong element type must retain prior %v, got %v", prior, v)
	}
}

func TestCodecStringList_wrongTypeWarnsAndRetainsPrior(t *testing.T) {
	ctx := context.Background()
	prior, priorDiags := types.ListValue(types.StringType, []attr.Value{types.StringValue("prior")})
	if priorDiags.HasError() {
		t.Fatalf("unexpected diagnostics building prior: %v", priorDiags)
	}
	v, diags := codecStringList(ctx, map[string]any{"k": "not-a-list"}, "k", prior)
	if diags.HasError() {
		t.Fatalf("wrong type must not be an error, got: %v", diags)
	}
	if !hasWarning(diags) {
		t.Fatalf("wrong type must produce a warning")
	}
	if !v.Equal(prior) {
		t.Fatalf("wrong type must retain prior %v, got %v", prior, v)
	}
}

func TestCodecStringList_absenceYieldsNullNotPrior(t *testing.T) {
	ctx := context.Background()
	prior, priorDiags := types.ListValue(types.StringType, []attr.Value{types.StringValue("prior")})
	if priorDiags.HasError() {
		t.Fatalf("unexpected diagnostics building prior: %v", priorDiags)
	}
	v, diags := codecStringList(ctx, map[string]any{}, "k", prior)
	if diags.HasError() || !v.IsNull() {
		t.Fatalf("absence must clear to null (not retain prior); got %v %v", v, diags)
	}
}

// --- low-level codec: codecInt64List (new: radio_ai's five []int64 fields) ---

func TestCodecInt64List_absentIsNull(t *testing.T) {
	ctx := context.Background()
	v, diags := codecInt64List(ctx, map[string]any{}, "k", types.ListNull(types.Int64Type))
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !v.IsNull() {
		t.Fatalf("expected null for absent key, got %v", v)
	}
}

func TestCodecInt64List_presentEmptyIsValueNotNull(t *testing.T) {
	ctx := context.Background()
	v, diags := codecInt64List(ctx, map[string]any{"k": []any{}}, "k", types.ListNull(types.Int64Type))
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

func TestCodecInt64List_presentValues(t *testing.T) {
	ctx := context.Background()
	v, diags := codecInt64List(ctx, map[string]any{"k": []any{float64(36), float64(40)}}, "k", types.ListNull(types.Int64Type))
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	elems := v.Elements()
	if len(elems) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(elems))
	}
	if elems[0].(types.Int64).ValueInt64() != 36 || elems[1].(types.Int64).ValueInt64() != 40 {
		t.Fatalf("expected [36 40], got %v", elems)
	}
}

func TestCodecInt64List_fractionalElementWarnsAndRetainsPrior(t *testing.T) {
	ctx := context.Background()
	prior, priorDiags := types.ListValue(types.Int64Type, []attr.Value{types.Int64Value(1)})
	if priorDiags.HasError() {
		t.Fatalf("unexpected diagnostics building prior: %v", priorDiags)
	}
	v, diags := codecInt64List(ctx, map[string]any{"k": []any{float64(1.9)}}, "k", prior)
	if diags.HasError() {
		t.Fatalf("fractional element must not be an error, got: %v", diags)
	}
	if !hasWarning(diags) {
		t.Fatalf("fractional element must produce a warning")
	}
	if !v.Equal(prior) {
		t.Fatalf("fractional element must retain prior %v, got %v", prior, v)
	}
}

func TestCodecInt64List_wrongElementTypeWarnsAndRetainsPrior(t *testing.T) {
	ctx := context.Background()
	prior, priorDiags := types.ListValue(types.Int64Type, []attr.Value{types.Int64Value(1)})
	if priorDiags.HasError() {
		t.Fatalf("unexpected diagnostics building prior: %v", priorDiags)
	}
	v, diags := codecInt64List(ctx, map[string]any{"k": []any{"not-a-number"}}, "k", prior)
	if diags.HasError() {
		t.Fatalf("wrong element type must not be an error, got: %v", diags)
	}
	if !hasWarning(diags) {
		t.Fatalf("wrong element type must produce a warning")
	}
	if !v.Equal(prior) {
		t.Fatalf("wrong element type must retain prior %v, got %v", prior, v)
	}
}

func TestCodecInt64List_wrongTypeWarnsAndRetainsPrior(t *testing.T) {
	ctx := context.Background()
	prior, priorDiags := types.ListValue(types.Int64Type, []attr.Value{types.Int64Value(1)})
	if priorDiags.HasError() {
		t.Fatalf("unexpected diagnostics building prior: %v", priorDiags)
	}
	v, diags := codecInt64List(ctx, map[string]any{"k": "not-a-list"}, "k", prior)
	if diags.HasError() {
		t.Fatalf("wrong type must not be an error, got: %v", diags)
	}
	if !hasWarning(diags) {
		t.Fatalf("wrong type must produce a warning")
	}
	if !v.Equal(prior) {
		t.Fatalf("wrong type must retain prior %v, got %v", prior, v)
	}
}

func TestPutInt64List_writesValues(t *testing.T) {
	out := map[string]any{}
	l, diags := types.ListValue(types.Int64Type, []attr.Value{types.Int64Value(36), types.Int64Value(40)})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}
	putDiags := putInt64List(out, "k", l)
	if putDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", putDiags)
	}
	list, ok := out["k"].([]any)
	if !ok || len(list) != 2 {
		t.Fatalf("expected 2-element []any, got %v", out["k"])
	}
	if list[0] != float64(36) || list[1] != float64(40) {
		t.Fatalf("expected [36.0 40.0], got %v", list)
	}
}

func TestPutInt64List_writesEmptyList(t *testing.T) {
	out := map[string]any{}
	empty, diags := types.ListValue(types.Int64Type, []attr.Value{})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}
	putDiags := putInt64List(out, "k", empty)
	if putDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", putDiags)
	}
	list, ok := out["k"].([]any)
	if !ok || len(list) != 0 {
		t.Fatalf("expected empty []any, got %v", out["k"])
	}
}

func TestPutInt64List_skipsNull(t *testing.T) {
	out := map[string]any{}
	putDiags := putInt64List(out, "k", types.ListNull(types.Int64Type))
	if putDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", putDiags)
	}
	if _, ok := out["k"]; ok {
		t.Fatalf("expected null to be skipped")
	}
}

func TestPutInt64List_skipsUnknown(t *testing.T) {
	out := map[string]any{}
	putDiags := putInt64List(out, "k", types.ListUnknown(types.Int64Type))
	if putDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", putDiags)
	}
	if _, ok := out["k"]; ok {
		t.Fatalf("expected unknown to be skipped")
	}
}

func TestDecodeInt64List_passesThroughToCodecInt64List(t *testing.T) {
	ctx := context.Background()
	data := map[string]any{"k": []any{float64(1), float64(2)}}
	v, diags := decodeInt64List(ctx, data, "k", types.ListNull(types.Int64Type))
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if len(v.Elements()) != 2 {
		t.Fatalf("expected 2 elements read from data, got %d", len(v.Elements()))
	}
}

func TestOverlayInt64List_writesEmptyList(t *testing.T) {
	ctx := context.Background()
	out := map[string]any{"k": []any{float64(1)}}
	empty, diags := types.ListValue(types.Int64Type, []attr.Value{})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}
	overlayDiags := overlayInt64List(ctx, out, "k", empty)
	if overlayDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", overlayDiags)
	}
	list, ok := out["k"].([]any)
	if !ok || len(list) != 0 {
		t.Fatalf("expected empty list written (present-empty), got %v", out["k"])
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

// --- decode/overlay layer: class-free, thin over the low-level codec ---
//
// Every decode*/overlay* wrapper is now a direct pass-through to its codec*/
// put* counterpart (see setting_codec.go's package doc comment): decode*
// passes prior straight through, overlay* always writes the managed path.
// These tests pin that pass-through directly rather than re-testing the
// codec*/put* behavior already covered above.

func TestDecodeString_passesThroughToCodecString(t *testing.T) {
	data := map[string]any{"k": "from-api"}
	prior := types.StringValue("stale")
	v, diags := decodeString(data, "k", prior)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if v.ValueString() != "from-api" {
		t.Fatalf("expected value read from data, got %q", v.ValueString())
	}
}

func TestOverlayString_writesKnownIncludingEmpty(t *testing.T) {
	out := map[string]any{"k": "original"}
	overlayString(out, "k", types.StringValue(""))
	if out["k"] != "" {
		t.Fatalf("expected empty string written, got %v", out["k"])
	}
}

func TestOverlayString_nullPreservesSnapshot(t *testing.T) {
	out := map[string]any{"k": "original"}
	overlayString(out, "k", types.StringNull())
	if out["k"] != "original" {
		t.Fatalf("expected snapshot value preserved on null, got %v", out["k"])
	}
}

func TestDecodeInt64_passesThroughToCodecInt64(t *testing.T) {
	data := map[string]any{"k": 42.0}
	v, diags := decodeInt64(data, "k", types.Int64Null())
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if v.ValueInt64() != 42 {
		t.Fatalf("expected 42, got %d", v.ValueInt64())
	}
}

func TestOverlayInt64_writesKnown(t *testing.T) {
	out := map[string]any{"k": 1.0}
	overlayInt64(out, "k", types.Int64Value(99))
	if out["k"] != float64(99) {
		t.Fatalf("expected 99.0 written, got %v", out["k"])
	}
}

func TestDecodeBool_passesThroughToCodecBool(t *testing.T) {
	data := map[string]any{"k": true}
	v, diags := decodeBool(data, "k", types.BoolNull())
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !v.ValueBool() {
		t.Fatalf("expected true, got %v", v.ValueBool())
	}
}

func TestOverlayBool_writesFalse(t *testing.T) {
	out := map[string]any{"k": true}
	overlayBool(out, "k", types.BoolValue(false))
	if out["k"] != false {
		t.Fatalf("expected false written, got %v", out["k"])
	}
}

func TestDecodeStringList_passesThroughToCodecStringList(t *testing.T) {
	ctx := context.Background()
	data := map[string]any{"k": []any{"a", "b"}}
	v, diags := decodeStringList(ctx, data, "k", types.ListNull(types.StringType))
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if len(v.Elements()) != 2 {
		t.Fatalf("expected 2 elements read from data, got %d", len(v.Elements()))
	}
}

func TestOverlayStringList_writesEmptyList(t *testing.T) {
	ctx := context.Background()
	out := map[string]any{"k": []any{"orig"}}
	empty, diags := types.ListValue(types.StringType, []attr.Value{})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}
	overlayDiags := overlayStringList(ctx, out, "k", empty)
	if overlayDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", overlayDiags)
	}
	list, ok := out["k"].([]any)
	if !ok || len(list) != 0 {
		t.Fatalf("expected empty list written (present-empty), got %v", out["k"])
	}
}

// --- nested shapes: decodeObject / overlayObject ---

func nestedTestAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"name": types.StringType,
		"note": types.StringType,
	}
}

func TestDecodeObject_readsChildrenFromData(t *testing.T) {
	ctx := context.Background()
	data := map[string]any{
		"nested": map[string]any{
			"name": "from-api",
			"note": "also-from-api",
		},
	}
	attrTypes := nestedTestAttrTypes()
	priorObj, diags := types.ObjectValue(attrTypes, map[string]attr.Value{
		"name": types.StringValue("prior-name"),
		"note": types.StringValue("prior-note"),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}

	v, decodeDiags := decodeObject(ctx, data, "nested", priorObj, attrTypes)
	if decodeDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", decodeDiags)
	}
	if v.IsNull() {
		t.Fatalf("expected populated object, got null")
	}
	attrs := v.Attributes()
	name, ok := attrs["name"].(types.String)
	if !ok || name.ValueString() != "from-api" {
		t.Fatalf("expected name=from-api, got %v", attrs["name"])
	}
	note, ok := attrs["note"].(types.String)
	if !ok || note.ValueString() != "also-from-api" {
		t.Fatalf("expected note=also-from-api, got %v", attrs["note"])
	}
}

func TestDecodeObject_absentKeyIsNullObject(t *testing.T) {
	ctx := context.Background()
	data := map[string]any{}
	attrTypes := nestedTestAttrTypes()
	v, diags := decodeObject(ctx, data, "nested", types.ObjectNull(attrTypes), attrTypes)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !v.IsNull() {
		t.Fatalf("expected null object for absent key, got %v", v)
	}
}

func TestDecodeObject_nonMapValueWarnsAndRetainsPrior(t *testing.T) {
	ctx := context.Background()
	data := map[string]any{"nested": "not-an-object"}
	attrTypes := nestedTestAttrTypes()
	prior := mustObjectValue(t, attrTypes, map[string]attr.Value{
		"name": types.StringValue("prior-name"),
		"note": types.StringValue("prior-note"),
	})
	v, diags := decodeObject(ctx, data, "nested", prior, attrTypes)
	if diags.HasError() {
		t.Fatalf("non-map value must not be an error, got: %v", diags)
	}
	if !hasWarning(diags) {
		t.Fatalf("non-map value must produce a warning")
	}
	if !v.Equal(prior) {
		t.Fatalf("non-map value must retain prior %v, got %v", prior, v)
	}
}

func TestOverlayObject_writesChildren(t *testing.T) {
	ctx := context.Background()
	out := map[string]any{
		"nested": map[string]any{
			"name": "orig-name",
			"note": "orig-note",
		},
	}
	attrTypes := nestedTestAttrTypes()
	cfg, diags := types.ObjectValue(attrTypes, map[string]attr.Value{
		"name": types.StringValue("new-name"),
		"note": types.StringNull(),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}

	overlayDiags := overlayObject(ctx, out, "nested", cfg)
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
	// note is null in cfg: the managed overlay path leaves the snapshot's
	// copied value untouched (matches overlayString's null-preserves
	// behavior).
	if nested["note"] != "orig-note" {
		t.Fatalf("expected note preserved on null config, got %v", nested["note"])
	}
}

func TestOverlayObject_nullConfigIsNoop(t *testing.T) {
	ctx := context.Background()
	out := map[string]any{
		"nested": map[string]any{
			"name": "orig-name",
			"note": "orig-note",
		},
	}
	attrTypes := nestedTestAttrTypes()
	overlayDiags := overlayObject(ctx, out, "nested", types.ObjectNull(attrTypes))
	if overlayDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", overlayDiags)
	}
	nested := out["nested"].(map[string]any)
	if nested["name"] != "orig-name" {
		t.Fatalf("expected snapshot untouched on null object config, got %v", nested["name"])
	}
	if nested["note"] != "orig-note" {
		t.Fatalf("expected snapshot untouched on null object config, got %v", nested["note"])
	}
}

// --- nested shapes: decodeObjectList / overlayObjectList ---

func TestDecodeObjectList_readsElementsFromData(t *testing.T) {
	ctx := context.Background()
	data := map[string]any{
		"keys": []any{
			map[string]any{"name": "key-a"},
			map[string]any{"name": "key-b"},
		},
	}
	attrTypes := map[string]attr.Type{
		"name": types.StringType,
	}
	elemType := types.ObjectType{AttrTypes: attrTypes}

	v, decodeDiags := decodeObjectList(ctx, data, "keys", types.ListNull(elemType), elemType)
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
	second := elems[1].(types.Object).Attributes()
	if second["name"].(types.String).ValueString() != "key-b" {
		t.Fatalf("expected second name=key-b, got %v", second["name"])
	}
}

func TestDecodeObjectList_absentKeyIsNullList(t *testing.T) {
	ctx := context.Background()
	data := map[string]any{}
	attrTypes := map[string]attr.Type{"name": types.StringType}
	elemType := types.ObjectType{AttrTypes: attrTypes}
	v, diags := decodeObjectList(ctx, data, "keys", types.ListNull(elemType), elemType)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !v.IsNull() {
		t.Fatalf("expected null list for absent key, got %v", v)
	}
}

func TestOverlayObjectList_writesElements(t *testing.T) {
	ctx := context.Background()
	out := map[string]any{
		"keys": []any{
			map[string]any{"name": "key-a"},
		},
	}
	attrTypes := map[string]attr.Type{
		"name": types.StringType,
	}
	elemType := types.ObjectType{AttrTypes: attrTypes}
	cfgElem, diags := types.ObjectValue(attrTypes, map[string]attr.Value{
		"name": types.StringValue("key-a-renamed"),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}
	cfg, diags := types.ListValue(elemType, []attr.Value{cfgElem})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}

	overlayDiags := overlayObjectList(ctx, out, "keys", cfg)
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
}

func TestOverlayObjectList_nullConfigIsNoop(t *testing.T) {
	ctx := context.Background()
	out := map[string]any{
		"keys": []any{
			map[string]any{"name": "key-a"},
		},
	}
	attrTypes := map[string]attr.Type{
		"name": types.StringType,
	}
	elemType := types.ObjectType{AttrTypes: attrTypes}
	overlayDiags := overlayObjectList(ctx, out, "keys", types.ListNull(elemType))
	if overlayDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", overlayDiags)
	}
	list := out["keys"].([]any)
	elem := list[0].(map[string]any)
	if elem["name"] != "key-a" {
		t.Fatalf("expected snapshot untouched on null list config, got %v", elem)
	}
}

// ---------------------------------------------------------------------------
// Generalized nested codec: typed leaves + recursion.
// ---------------------------------------------------------------------------

// testTrackingAttrTypes/testSuppressionAlertAttrTypes/testDohCustomServerAttrTypes model
// the ips/doh schema shapes: double nesting (suppression_alerts.tracking),
// int64 leaves (gid/id), and a bool leaf (custom_servers.enabled).

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

// --- 1. Double-nested recursion ---

// TestDecodeObjectFields_doubleNestedRecursesIntoTrackingList proves
// decodeObjectFields recurses through a further-nested object-list child
// (suppression_alerts.tracking) and reads its own leaf (direction) from the
// API, not from prior — i.e. the recursion actually reaches the innermost
// leaf rather than stopping at the first level.
func TestDecodeObjectFields_doubleNestedRecursesIntoTrackingList(t *testing.T) {
	ctx := context.Background()
	attrTypes := testSuppressionAlertAttrTypes()
	prior := mustObjectValue(t, attrTypes, map[string]attr.Value{
		"gid": types.Int64Value(1),
		"id":  types.Int64Value(2),
		"tracking": mustListValue(t, types.ObjectType{AttrTypes: testTrackingAttrTypes()}, []attr.Value{
			mustObjectValue(t, testTrackingAttrTypes(), map[string]attr.Value{
				"direction": types.StringValue("prior-direction"),
			}),
		}),
	})

	nested := map[string]any{
		"gid": 1.0,
		"id":  2.0,
		"tracking": []any{
			map[string]any{"direction": "from-api"},
		},
	}

	v, decodeDiags := decodeObjectFields(ctx, nested, "suppression_alerts", prior, attrTypes)
	if decodeDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", decodeDiags)
	}
	tracking := v.Attributes()["tracking"].(types.List).Elements()
	if len(tracking) != 1 {
		t.Fatalf("expected 1 tracking element, got %d", len(tracking))
	}
	direction := tracking[0].(types.Object).Attributes()["direction"].(types.String)
	if direction.ValueString() != "from-api" {
		t.Fatalf("expected tracking.direction read from data, got %v", direction)
	}
}

// --- 2. Type coverage: bool false, int64 0 round-trip ---

func TestDecodeObjectFields_boolFalseAndInt64ZeroAreNotDropped(t *testing.T) {
	ctx := context.Background()
	attrTypes := testDohCustomServerAttrTypes()
	nested := map[string]any{"enabled": false}

	v, diags := decodeObjectFields(ctx, nested, "custom_servers", types.ObjectNull(attrTypes), attrTypes)
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
	attrTypes := testDohCustomServerAttrTypes()
	cfg, diags := types.ObjectValue(attrTypes, map[string]attr.Value{
		"enabled": types.BoolValue(false),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}

	nested := map[string]any{}
	overlayDiags := overlayObjectFields(ctx, nested, "custom_servers", cfg)
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
	attrTypes := testSuppressionAlertAttrTypes()
	nested := map[string]any{
		"gid":      0.0,
		"id":       0.0,
		"tracking": []any{},
	}

	v, diags := decodeObjectFields(ctx, nested, "suppression_alerts", types.ObjectNull(attrTypes), attrTypes)
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
	overlayDiags := overlayObjectFields(ctx, nested, "suppression_alerts", cfg)
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
	elemType := types.ObjectType{AttrTypes: testDohCustomServerAttrTypes()}
	v, diags := decodeObjectList(ctx, map[string]any{}, "custom_servers", types.ListNull(elemType), elemType)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !v.IsNull() {
		t.Fatalf("expected null list for absent key, got %v", v)
	}
}

func TestOverlayObjectList_unknownConfigIsNoop(t *testing.T) {
	ctx := context.Background()
	elemType := types.ObjectType{AttrTypes: testDohCustomServerAttrTypes()}
	out := map[string]any{
		"custom_servers": []any{
			map[string]any{"enabled": true},
		},
	}
	overlayDiags := overlayObjectList(ctx, out, "custom_servers", types.ListUnknown(elemType))
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
	elemType := types.ObjectType{AttrTypes: testDohCustomServerAttrTypes()}
	data := map[string]any{"custom_servers": []any{}}
	v, diags := decodeObjectList(ctx, data, "custom_servers", types.ListNull(elemType), elemType)
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
	overlayDiags := overlayObjectList(ctx, out, "custom_servers", cfg)
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
	overlayDiags := overlayObjectFields(ctx, nested, "suppression_alerts", cfg)
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

// --- 4. Ordering + fresh-element build (no positional carry) ---

// TestOverlayObjectList_freshElementDropsBaseFieldOmittedFromConfig proves
// each output element is built FRESH from cfg's modeled leaves: a field
// present on the base's same-index element but absent from cfg's attrTypes
// (e.g. an unmodeled field a section hasn't opted into re-adding) is simply
// not present in the output — it is never carried over by list position.
func TestOverlayObjectList_freshElementDropsBaseFieldOmittedFromConfig(t *testing.T) {
	ctx := context.Background()
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

	overlayDiags := overlayObjectList(ctx, out, "items", cfg)
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
	if _, ok := elemA["extra"]; ok {
		t.Fatalf("expected element 0 to be built fresh (no base 'extra' carried by position), got %v", elemA["extra"])
	}
	elemB := list[1].(map[string]any)
	if _, ok := elemB["extra"]; ok {
		t.Fatalf("expected element 1 to be built fresh (no base 'extra' carried by position), got %v", elemB["extra"])
	}
}

// TestOverlayObjectList_reorderDoesNotCrossAttachBaseFields is the general
// (non-mgmt) regression test for the same-index metadata cross-attachment
// bug: reordering/removing elements must never let an unmodeled base field
// belonging to one logical element land on a different element solely
// because of list position. Base has 3 elements; config reorders to just
// [orig-c, orig-a] (element "orig-b" removed, order changed). Every output
// element must be a fresh build — none of them may pick up ANY base
// "extra" value, regardless of what position they land at.
func TestOverlayObjectList_reorderDoesNotCrossAttachBaseFields(t *testing.T) {
	ctx := context.Background()
	attrTypes := map[string]attr.Type{"name": types.StringType}
	elemType := types.ObjectType{AttrTypes: attrTypes}

	out := map[string]any{
		"items": []any{
			map[string]any{"name": "orig-a", "extra": "extra-a"},
			map[string]any{"name": "orig-b", "extra": "extra-b"},
			map[string]any{"name": "orig-c", "extra": "extra-c"},
		},
	}
	cfgElem0 := mustObjectValue(t, attrTypes, map[string]attr.Value{"name": types.StringValue("orig-c")})
	cfgElem1 := mustObjectValue(t, attrTypes, map[string]attr.Value{"name": types.StringValue("orig-a")})
	cfg := mustListValue(t, elemType, []attr.Value{cfgElem0, cfgElem1})

	overlayDiags := overlayObjectList(ctx, out, "items", cfg)
	if overlayDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", overlayDiags)
	}
	list := out["items"].([]any)
	if len(list) != 2 {
		t.Fatalf("expected 2 elements after reorder+removal, got %d", len(list))
	}
	elem0 := list[0].(map[string]any)
	if elem0["name"] != "orig-c" {
		t.Fatalf("expected element 0 name=orig-c from config, got %v", elem0["name"])
	}
	if v, ok := elem0["extra"]; ok {
		t.Fatalf("element 0 (orig-c) must not cross-attach any base 'extra' value by position, got %v", v)
	}
	elem1 := list[1].(map[string]any)
	if elem1["name"] != "orig-a" {
		t.Fatalf("expected element 1 name=orig-a from config, got %v", elem1["name"])
	}
	if v, ok := elem1["extra"]; ok {
		t.Fatalf("element 1 (orig-a) must not cross-attach any base 'extra' value by position, got %v", v)
	}
}

func TestDecodeObjectList_apiResponseOrderPreserved(t *testing.T) {
	ctx := context.Background()
	attrTypes := map[string]attr.Type{"name": types.StringType}
	elemType := types.ObjectType{AttrTypes: attrTypes}
	data := map[string]any{
		"items": []any{
			map[string]any{"name": "third"},
			map[string]any{"name": "first"},
			map[string]any{"name": "second"},
		},
	}
	v, diags := decodeObjectList(ctx, data, "items", types.ListNull(elemType), elemType)
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

	overlayDiags := overlayObjectList(ctx, base, "suppression_alerts", cfg)
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

// --- 6. Malformed shapes -> warning + retain prior (Task 1: remote type drift) ---

func TestDecodeObjectList_outerNotArrayWarnsAndRetainsPrior(t *testing.T) {
	ctx := context.Background()
	attrTypes := map[string]attr.Type{"name": types.StringType}
	elemType := types.ObjectType{AttrTypes: attrTypes}
	priorElem := mustObjectValue(t, attrTypes, map[string]attr.Value{"name": types.StringValue("prior")})
	prior := mustListValue(t, elemType, []attr.Value{priorElem})
	data := map[string]any{"items": "not-an-array"}
	v, diags := decodeObjectList(ctx, data, "items", prior, elemType)
	if diags.HasError() {
		t.Fatalf("non-array outer value must not be an error, got: %v", diags)
	}
	if !hasWarning(diags) {
		t.Fatalf("non-array outer value must produce a warning")
	}
	if !v.Equal(prior) {
		t.Fatalf("non-array outer value must retain prior list %v, got %v", prior, v)
	}
}

func TestDecodeObjectList_elementNotObjectWarnsAndRetainsPrior(t *testing.T) {
	ctx := context.Background()
	attrTypes := map[string]attr.Type{"name": types.StringType}
	elemType := types.ObjectType{AttrTypes: attrTypes}
	priorElem := mustObjectValue(t, attrTypes, map[string]attr.Value{"name": types.StringValue("prior")})
	prior := mustListValue(t, elemType, []attr.Value{priorElem})
	data := map[string]any{"items": []any{"not-an-object"}}
	v, diags := decodeObjectList(ctx, data, "items", prior, elemType)
	if diags.HasError() {
		t.Fatalf("non-object element must not be an error, got: %v", diags)
	}
	if !hasWarning(diags) {
		t.Fatalf("non-object element must produce a warning")
	}
	if !v.Equal(prior) {
		t.Fatalf("non-object element must retain prior list %v, got %v", prior, v)
	}
}

func TestDecodeObjectFields_boolLeafWithStringRawValueWarnsAndRetainsPrior(t *testing.T) {
	ctx := context.Background()
	attrTypes := testDohCustomServerAttrTypes()
	nested := map[string]any{"enabled": "not-a-bool"}
	prior := mustObjectValue(t, attrTypes, map[string]attr.Value{"enabled": types.BoolValue(true)})
	v, diags := decodeObjectFields(ctx, nested, "custom_servers", prior, attrTypes)
	if diags.HasError() {
		t.Fatalf("bool leaf type drift must not be an error, got: %v", diags)
	}
	if !hasWarning(diags) {
		t.Fatalf("bool leaf type drift must produce a warning")
	}
	got, ok := v.Attributes()["enabled"].(types.Bool)
	if !ok || !got.ValueBool() {
		t.Fatalf("expected enabled to retain prior true, got %v", v.Attributes()["enabled"])
	}
}

func TestDecodeObjectFields_int64LeafWithFractionalValueWarnsAndRetainsPrior(t *testing.T) {
	ctx := context.Background()
	attrTypes := testSuppressionAlertAttrTypes()
	nested := map[string]any{
		"gid":      1.9,
		"id":       2.0,
		"tracking": []any{},
	}
	prior := mustObjectValue(t, attrTypes, map[string]attr.Value{
		"gid":      types.Int64Value(7),
		"id":       types.Int64Value(8),
		"tracking": types.ListNull(types.ObjectType{AttrTypes: testTrackingAttrTypes()}),
	})
	v, diags := decodeObjectFields(ctx, nested, "suppression_alerts", prior, attrTypes)
	if diags.HasError() {
		t.Fatalf("fractional int64 leaf must not be an error, got: %v", diags)
	}
	if !hasWarning(diags) {
		t.Fatalf("fractional int64 leaf must produce a warning")
	}
	got, ok := v.Attributes()["gid"].(types.Int64)
	if !ok || got.ValueInt64() != 7 {
		t.Fatalf("expected gid to retain prior 7 (not silently truncate 1.9), got %v", v.Attributes()["gid"])
	}
}

func TestDecodeObjectFields_malformedLeafYieldsWarningNotPartialOutput(t *testing.T) {
	ctx := context.Background()
	attrTypes := testDohCustomServerAttrTypes()
	nested := map[string]any{"enabled": "not-a-bool"}
	prior := mustObjectValue(t, attrTypes, map[string]attr.Value{"enabled": types.BoolValue(false)})
	v, diags := decodeObjectFields(ctx, nested, "custom_servers", prior, attrTypes)
	if diags.HasError() {
		t.Fatalf("malformed leaf must not be an error, got: %v", diags)
	}
	if !hasWarning(diags) {
		t.Fatalf("malformed leaf must produce a warning")
	}
	if v.IsUnknown() {
		t.Fatalf("expected a known object built from prior/valid leaves (not Unknown) on a warning-only leaf, got %v", v)
	}
}

// --- 7. Round trips ---

func TestDecodeObjectList_dohCustomServerRoundTripWithFalseEnabled(t *testing.T) {
	ctx := context.Background()
	attrTypes := testDohCustomServerAttrTypes()
	elemType := types.ObjectType{AttrTypes: attrTypes}
	data := map[string]any{
		"custom_servers": []any{
			map[string]any{"enabled": false},
		},
	}
	v, diags := decodeObjectList(ctx, data, "custom_servers", types.ListNull(elemType), elemType)
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
	overlayDiags := overlayObjectList(ctx, out, "custom_servers", v)
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

	v, diags := decodeObjectList(ctx, data, "suppression_alerts", types.ListNull(elemType), elemType)
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
	overlayDiags := overlayObjectList(ctx, out, "suppression_alerts", v)
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

// ---------------------------------------------------------------------------
// GoDuration <-> integer-seconds codec.
// ---------------------------------------------------------------------------

// --- ownership-aware layer: decodeGoDuration ---

func TestDecodeGoDuration_readsFromData(t *testing.T) {
	data := map[string]any{"t": float64(3600)}
	v, diags := decodeGoDuration(data, "t", timetypes.NewGoDurationNull(), time.Second)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	dur, durDiags := v.ValueGoDuration()
	if durDiags.HasError() {
		t.Fatalf("unexpected diagnostics reading duration: %v", durDiags)
	}
	if dur != time.Hour {
		t.Fatalf("expected 1h0m0s, got %v", dur)
	}
}

func TestDecodeGoDuration_absentIsNull(t *testing.T) {
	v, diags := decodeGoDuration(map[string]any{}, "t", timetypes.NewGoDurationNull(), time.Second)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !v.IsNull() {
		t.Fatalf("expected null for absent key, got %v", v)
	}
}

func TestCodecGoDuration_fractionalWarnsAndRetainsPrior(t *testing.T) {
	prior := util.DurationValue(5, time.Second)
	v, diags := codecGoDuration(map[string]any{"t": 1.5}, "t", prior, time.Second)
	if diags.HasError() {
		t.Fatalf("fractional value must not be an error, got: %v", diags)
	}
	if !hasWarning(diags) {
		t.Fatalf("fractional value must produce a warning")
	}
	if !v.Equal(prior) {
		t.Fatalf("fractional value must retain prior %v, got %v (must not silently truncate)", prior, v)
	}
}

func TestCodecGoDuration_typeDriftWarnsAndRetainsPrior(t *testing.T) {
	prior := util.DurationValue(5, time.Second)
	v, diags := codecGoDuration(map[string]any{"t": "nope"}, "t", prior, time.Second)
	if diags.HasError() {
		t.Fatalf("type drift must not be an error, got: %v", diags)
	}
	if !hasWarning(diags) {
		t.Fatalf("type drift must produce a warning")
	}
	if !v.Equal(prior) {
		t.Fatalf("type drift must retain prior %v, got %v", prior, v)
	}
}

func TestCodecGoDuration_absenceYieldsNullNotPrior(t *testing.T) {
	v, diags := codecGoDuration(map[string]any{}, "t", util.DurationValue(5, time.Second), time.Second)
	if diags.HasError() || !v.IsNull() {
		t.Fatalf("absence must clear to null (not retain prior); got %v %v", v, diags)
	}
}

// --- ownership-aware layer: overlayGoDuration ---

func TestOverlayGoDuration_writesKnownValue(t *testing.T) {
	out := map[string]any{"t": float64(1)}
	overlayGoDuration(out, "t", util.DurationValue(3600, time.Second), time.Second)
	if out["t"] != float64(3600) {
		t.Fatalf("expected 3600.0 written, got %v", out["t"])
	}
}

// TestOverlayGoDuration_writesKnownZero proves the intentional
// zero-emission behavior non-vacuously: it seeds out["t"] with a sentinel
// (float64(999)) distinct from both the input (0) and the zero-value of the
// map lookup, so a passing assertion can only mean putGoDuration actually
// executed the write rather than the key coincidentally already being 0 or
// absent.
func TestOverlayGoDuration_writesKnownZero(t *testing.T) {
	out := map[string]any{"t": float64(999)}
	overlayGoDuration(out, "t", util.DurationValue(0, time.Second), time.Second)
	v, ok := out["t"]
	if !ok {
		t.Fatalf("expected key present after configured-zero GoDuration overlay (intentional write, not omission)")
	}
	if v != float64(0) {
		t.Fatalf("expected 0.0 written (not omitted, not left at sentinel 999), got %v", v)
	}
}

func TestOverlayGoDuration_nullPreservesSnapshot(t *testing.T) {
	out := map[string]any{"t": float64(42)}
	overlayGoDuration(out, "t", timetypes.NewGoDurationNull(), time.Second)
	if out["t"] != float64(42) {
		t.Fatalf("expected snapshot value preserved on null, got %v", out["t"])
	}
}

// --- round-trip ---

func TestGoDuration_roundTripDecodeOverlay(t *testing.T) {
	data := map[string]any{"t": float64(1800)}
	decoded, decodeDiags := decodeGoDuration(data, "t", timetypes.NewGoDurationNull(), time.Second)
	if decodeDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", decodeDiags)
	}
	dur, durDiags := decoded.ValueGoDuration()
	if durDiags.HasError() {
		t.Fatalf("unexpected diagnostics reading duration: %v", durDiags)
	}
	if dur != 30*time.Minute {
		t.Fatalf("expected 30m0s, got %v", dur)
	}

	out := map[string]any{}
	overlayGoDuration(out, "t", decoded, time.Second)
	if out["t"] != float64(1800) {
		t.Fatalf("expected round-trip to 1800.0, got %v", out["t"])
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
