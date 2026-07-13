package unifi

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// setting_codec.go is the ONE place that translates between the controller's
// raw `map[string]any` settings representation (as decoded from JSON) and
// terraform-plugin-framework typed values. No section converter may read or
// write a map[string]any directly; everything goes through the functions in
// this file.
//
// Two layers:
//
//   - Low-level codec* / put* functions are the typed accessors. They encode
//     the raw-data contract: present-empty is a real value (not absent/null),
//     absent or explicit JSON null is TF-null, and a wrong or fractional type
//     is a diagnostic rather than a silent normalization.
//
//   - decode* / overlay* wrappers are thin, class-free adapters over the
//     low-level codec: decode* passes prior straight through to the matching
//     codec* reader, and overlay* always writes the managed path (put*).
//     Every field in this resource is managed-by-default; the two write-only
//     secret leaves (mgmt.ssh_password, radius.secret) are handled inline in
//     their own sections rather than through a field-ownership taxonomy.
//     Section converters call ONLY this layer.

// ---------------------------------------------------------------------------
// Low-level codec: map[string]any -> types.X
// ---------------------------------------------------------------------------

// codecString reads a string field from data. Absent key or explicit JSON
// null decodes to types.StringNull() (a controller clearing a managed field
// must clear state); present-empty ("") decodes to StringValue(""), never
// collapsed to null. A present value of any other Go type is remote type
// drift: a WARNING (not error), retaining prior so a single drifted field
// never fails refresh.
func codecString(data map[string]any, key string, prior types.String) (types.String, diag.Diagnostics) {
	var diags diag.Diagnostics
	raw, ok := data[key]
	if !ok || raw == nil {
		return types.StringNull(), diags
	}
	s, ok := raw.(string)
	if !ok {
		diags.AddWarning(
			"Settings value type drift",
			fmt.Sprintf("field %q: expected string, got %T; retaining last-known value", key, raw),
		)
		return prior, diags
	}
	return types.StringValue(s), diags
}

// codecBool reads a bool field. Absent or JSON null -> Terraform null (a
// controller clearing a managed field must clear state). A present value of a
// non-bool type is remote type drift: a WARNING (not error), retaining prior
// so a single drifted field never fails refresh.
func codecBool(data map[string]any, key string, prior types.Bool) (types.Bool, diag.Diagnostics) {
	var diags diag.Diagnostics
	raw, ok := data[key]
	if !ok || raw == nil {
		return types.BoolNull(), diags
	}
	b, ok := raw.(bool)
	if !ok {
		diags.AddWarning(
			"Settings value type drift",
			fmt.Sprintf("field %q: expected bool, got %T; retaining last-known value", key, raw),
		)
		return prior, diags
	}
	return types.BoolValue(b), diags
}

// codecInt64 reads an int64 field from data. JSON numbers decode to
// float64 in Go; a fractional value (e.g. 1.9) is malformed for an int64
// field and warns-and-retains prior instead of being silently truncated.
// Absent key or explicit JSON null decodes to types.Int64Null(). A present
// value of any other Go type is remote type drift: a WARNING (not error),
// retaining prior so a single drifted field never fails refresh.
func codecInt64(data map[string]any, key string, prior types.Int64) (types.Int64, diag.Diagnostics) {
	var diags diag.Diagnostics
	raw, ok := data[key]
	if !ok || raw == nil {
		return types.Int64Null(), diags
	}
	f, ok := raw.(float64)
	if !ok {
		diags.AddWarning(
			"Settings value type drift",
			fmt.Sprintf("field %q: expected number, got %T; retaining last-known value", key, raw),
		)
		return prior, diags
	}
	if math.Trunc(f) != f {
		diags.AddWarning(
			"Settings value type drift",
			fmt.Sprintf("field %q: expected integer, got fractional value %v; retaining last-known value", key, f),
		)
		return prior, diags
	}
	return types.Int64Value(int64(f)), diags
}

// codecGoDuration reads an integer-count-of-unit field from data and
// converts it to a GoDuration value (e.g. unit=time.Second, wire value 3600
// -> "1h0m0s"). JSON numbers decode to float64 in Go; a fractional value is
// malformed for an integer-seconds field and warns-and-retains prior instead
// of being silently truncated, matching codecInt64. Absent key or explicit
// JSON null decodes to timetypes.NewGoDurationNull(). A present value of any
// other Go type is remote type drift: a WARNING (not error), retaining prior
// so a single drifted field never fails refresh.
func codecGoDuration(data map[string]any, key string, prior timetypes.GoDuration, unit time.Duration) (timetypes.GoDuration, diag.Diagnostics) {
	var diags diag.Diagnostics
	raw, ok := data[key]
	if !ok || raw == nil {
		return timetypes.NewGoDurationNull(), diags
	}
	f, ok := raw.(float64)
	if !ok {
		diags.AddWarning(
			"Settings value type drift",
			fmt.Sprintf("field %q: expected number, got %T; retaining last-known value", key, raw),
		)
		return prior, diags
	}
	if math.Trunc(f) != f {
		diags.AddWarning(
			"Settings value type drift",
			fmt.Sprintf("field %q: expected integer, got fractional value %v; retaining last-known value", key, f),
		)
		return prior, diags
	}
	return util.DurationValue(int64(f), unit), diags
}

// codecStringList reads a list-of-string field from data. Absent/null
// decodes to types.ListNull(); present-empty ([]) decodes to an empty
// ListValue, not null. A present value that is not a []any, or an element
// that is not a string, is remote type drift: a WARNING (not error),
// retaining prior so a single drifted field never fails refresh.
func codecStringList(ctx context.Context, data map[string]any, key string, prior types.List) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics
	raw, ok := data[key]
	if !ok || raw == nil {
		return types.ListNull(types.StringType), diags
	}
	items, ok := raw.([]any)
	if !ok {
		diags.AddWarning(
			"Settings value type drift",
			fmt.Sprintf("field %q: expected array, got %T; retaining last-known value", key, raw),
		)
		return prior, diags
	}
	elems := make([]attr.Value, 0, len(items))
	for i, item := range items {
		s, ok := item.(string)
		if !ok {
			diags.AddWarning(
				"Settings value type drift",
				fmt.Sprintf("field %q: element %d: expected string, got %T; retaining last-known value", key, i, item),
			)
			return prior, diags
		}
		elems = append(elems, types.StringValue(s))
	}
	list, listDiags := types.ListValue(types.StringType, elems)
	diags.Append(listDiags...)
	return list, diags
}

// codecInt64List reads a list-of-int64 field from data. Absent/null
// decodes to types.ListNull(); present-empty ([]) decodes to an empty
// ListValue, not null. A present value that is not a []any, or an element
// that is not a whole-number float64, is remote type drift: a WARNING
// (not error), retaining prior so a single drifted field never fails
// refresh.
func codecInt64List(ctx context.Context, data map[string]any, key string, prior types.List) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics
	raw, ok := data[key]
	if !ok || raw == nil {
		return types.ListNull(types.Int64Type), diags
	}
	items, ok := raw.([]any)
	if !ok {
		diags.AddWarning(
			"Settings value type drift",
			fmt.Sprintf("field %q: expected array, got %T; retaining last-known value", key, raw),
		)
		return prior, diags
	}
	elems := make([]attr.Value, 0, len(items))
	for i, item := range items {
		f, ok := item.(float64)
		if !ok {
			diags.AddWarning(
				"Settings value type drift",
				fmt.Sprintf("field %q: element %d: expected number, got %T; retaining last-known value", key, i, item),
			)
			return prior, diags
		}
		if math.Trunc(f) != f {
			diags.AddWarning(
				"Settings value type drift",
				fmt.Sprintf("field %q: element %d: expected integer, got fractional value %v; retaining last-known value", key, i, f),
			)
			return prior, diags
		}
		elems = append(elems, types.Int64Value(int64(f)))
	}
	list, listDiags := types.ListValue(types.Int64Type, elems)
	diags.Append(listDiags...)
	return list, diags
}

// putInt64List writes v into out[key] when known (including an empty
// list); skips null/unknown. Mirrors putStringList.
func putInt64List(out map[string]any, key string, v types.List) diag.Diagnostics {
	var diags diag.Diagnostics
	if v.IsNull() || v.IsUnknown() {
		return diags
	}
	elems := v.Elements()
	items := make([]any, 0, len(elems))
	for _, e := range elems {
		i, ok := e.(types.Int64)
		if !ok {
			diags.AddError(
				"Malformed settings value",
				fmt.Sprintf("field %q: element is not an int64 value: %T", key, e),
			)
			continue
		}
		items = append(items, float64(i.ValueInt64()))
	}
	out[key] = items
	return diags
}

// ---------------------------------------------------------------------------
// Low-level setters: types.X -> map[string]any
// ---------------------------------------------------------------------------

// putString writes v into out[key] when v is a known value (including
// empty), and skips the write (leaving out[key] as whatever the copied
// base snapshot already held) when v is null or unknown.
func putString(out map[string]any, key string, v types.String) {
	if v.IsNull() || v.IsUnknown() {
		return
	}
	out[key] = v.ValueString()
}

// putInt64 writes v into out[key] when known; skips null/unknown.
func putInt64(out map[string]any, key string, v types.Int64) {
	if v.IsNull() || v.IsUnknown() {
		return
	}
	out[key] = float64(v.ValueInt64())
}

// putGoDuration writes v into out[key] as an integer count of unit when
// known (including a known 0s — an intentional configured value, not a
// signal to omit the field); skips null/unknown. Mirrors putInt64.
func putGoDuration(out map[string]any, key string, v timetypes.GoDuration, unit time.Duration) {
	if v.IsNull() || v.IsUnknown() {
		return
	}
	out[key] = float64(util.DurationUnits(v, unit))
}

// putBool writes v into out[key] when known (including false); skips
// null/unknown.
func putBool(out map[string]any, key string, v types.Bool) {
	if v.IsNull() || v.IsUnknown() {
		return
	}
	out[key] = v.ValueBool()
}

// putStringList writes v into out[key] when known (including an empty
// list); skips null/unknown.
func putStringList(ctx context.Context, out map[string]any, key string, v types.List) diag.Diagnostics {
	var diags diag.Diagnostics
	if v.IsNull() || v.IsUnknown() {
		return diags
	}
	elems := v.Elements()
	items := make([]any, 0, len(elems))
	for _, e := range elems {
		s, ok := e.(types.String)
		if !ok {
			diags.AddError(
				"Malformed settings value",
				fmt.Sprintf("field %q: element is not a string value: %T", key, e),
			)
			continue
		}
		items = append(items, s.ValueString())
	}
	out[key] = items
	return diags
}

// ---------------------------------------------------------------------------
// decode/overlay layer: class-free, thin adapters over the low-level codec.
// Every field is managed by default (decode reads from data, overlay always
// writes). The two write-only secret leaves in this resource (mgmt.
// ssh_password, radius.secret) do not go through this layer at all — their
// sections read/write them inline (see setting_section_mgmt.go /
// setting_section_radius.go).
// ---------------------------------------------------------------------------

// decodeString reads key's value into state, passing prior straight through
// to codecString for its type-drift-retains-prior behavior.
func decodeString(data map[string]any, key string, prior types.String) (types.String, diag.Diagnostics) {
	return codecString(data, key, prior)
}

// decodeInt64 is the int64 analogue of decodeString.
func decodeInt64(data map[string]any, key string, prior types.Int64) (types.Int64, diag.Diagnostics) {
	return codecInt64(data, key, prior)
}

// decodeGoDuration is the GoDuration analogue of decodeInt64, for fields
// whose wire form is an integer count of unit (e.g. whole seconds).
func decodeGoDuration(data map[string]any, key string, prior timetypes.GoDuration, unit time.Duration) (timetypes.GoDuration, diag.Diagnostics) {
	return codecGoDuration(data, key, prior, unit)
}

// decodeBool is the bool analogue of decodeString.
func decodeBool(data map[string]any, key string, prior types.Bool) (types.Bool, diag.Diagnostics) {
	return codecBool(data, key, prior)
}

// decodeStringList is the string-list analogue of decodeString.
func decodeStringList(ctx context.Context, data map[string]any, key string, prior types.List) (types.List, diag.Diagnostics) {
	return codecStringList(ctx, data, key, prior)
}

// decodeInt64List is the int64-list analogue of decodeStringList.
func decodeInt64List(ctx context.Context, data map[string]any, key string, prior types.List) (types.List, diag.Diagnostics) {
	return codecInt64List(ctx, data, key, prior)
}

// overlayString writes v onto out[key]: known (including empty) writes;
// null/unknown leaves the snapshot's copied value in out untouched,
// preserving the controller value across this apply.
func overlayString(out map[string]any, key string, v types.String) {
	putString(out, key, v)
}

// overlayInt64 is the int64 analogue of overlayString.
func overlayInt64(out map[string]any, key string, v types.Int64) {
	putInt64(out, key, v)
}

// overlayGoDuration is the GoDuration analogue of overlayInt64, for fields
// whose wire form is an integer count of unit.
func overlayGoDuration(out map[string]any, key string, v timetypes.GoDuration, unit time.Duration) {
	putGoDuration(out, key, v, unit)
}

// overlayBool is the bool analogue of overlayString.
func overlayBool(out map[string]any, key string, v types.Bool) {
	putBool(out, key, v)
}

// overlayStringList is the string-list analogue of overlayString.
func overlayStringList(ctx context.Context, out map[string]any, key string, v types.List) diag.Diagnostics {
	return putStringList(ctx, out, key, v)
}

// overlayInt64List is the int64-list analogue of overlayStringList.
func overlayInt64List(ctx context.Context, out map[string]any, key string, v types.List) diag.Diagnostics {
	return putInt64List(out, key, v)
}

// ---------------------------------------------------------------------------
// Nested shapes: SingleNestedAttribute (object) and ListNestedAttribute
// (object list). Every leaf is managed by default (see the decode/overlay
// layer doc comment above); no nested leaf in this resource is a secret, so
// this layer type-dispatches each child directly with no per-field class
// lookup.
// ---------------------------------------------------------------------------

// decodeObject reads a nested object field from data, recursing into each
// child leaf. Absent key or explicit JSON null decodes to a null object of
// attrTypes. A present non-map value is remote type drift: a WARNING (not
// error), retaining the prior object wholesale rather than partially
// decoding.
func decodeObject(
	ctx context.Context,
	data map[string]any,
	key string,
	prior types.Object,
	attrTypes map[string]attr.Type,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics

	raw, ok := data[key]
	if !ok || raw == nil {
		return types.ObjectNull(attrTypes), diags
	}
	nested, ok := raw.(map[string]any)
	if !ok {
		diags.AddWarning(
			"Settings value type drift",
			fmt.Sprintf("field %q: expected object, got %T; retaining last-known value", key, raw),
		)
		return prior, diags
	}

	return decodeObjectFields(ctx, nested, key, prior, attrTypes)
}

// decodeObjectFields decodes attrTypes' children directly out of nested
// (already unwrapped from its parent key), type-dispatching each child and
// recursing into nested object-lists. diagPath is the possibly-indexed path
// used only in diagnostic messages.
func decodeObjectFields(
	ctx context.Context,
	nested map[string]any,
	diagPath string,
	prior types.Object,
	attrTypes map[string]attr.Type,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics

	priorAttrs := map[string]attr.Value{}
	if !prior.IsNull() && !prior.IsUnknown() {
		priorAttrs = prior.Attributes()
	}

	attrs := make(map[string]attr.Value, len(attrTypes))
	for childKey, childType := range attrTypes {
		childDiagPath := diagPath + "." + childKey

		switch t := childType.(type) {
		case types.ListType:
			if _, isObjElem := t.ElemType.(types.ObjectType); isObjElem {
				priorChild := types.ListNull(t.ElemType)
				if pv, ok := priorAttrs[childKey].(types.List); ok {
					priorChild = pv
				}
				childVal, childDiags := decodeObjectList(ctx, nested, childKey, priorChild, t.ElemType)
				diags.Append(childDiags...)
				attrs[childKey] = childVal
				continue
			}
		}

		switch childType {
		case types.StringType:
			priorChild := types.StringNull()
			if pv, ok := priorAttrs[childKey].(types.String); ok {
				priorChild = pv
			}
			childVal, childDiags := decodeString(nested, childKey, priorChild)
			diags.Append(childDiags...)
			attrs[childKey] = childVal
		case types.BoolType:
			priorChild := types.BoolNull()
			if pv, ok := priorAttrs[childKey].(types.Bool); ok {
				priorChild = pv
			}
			childVal, childDiags := decodeBool(nested, childKey, priorChild)
			diags.Append(childDiags...)
			attrs[childKey] = childVal
		case types.Int64Type:
			priorChild := types.Int64Null()
			if pv, ok := priorAttrs[childKey].(types.Int64); ok {
				priorChild = pv
			}
			childVal, childDiags := decodeInt64(nested, childKey, priorChild)
			diags.Append(childDiags...)
			attrs[childKey] = childVal
		default:
			if lt, ok := childType.(types.ListType); ok && lt.ElemType == types.StringType {
				priorChild := types.ListNull(types.StringType)
				if pv, ok := priorAttrs[childKey].(types.List); ok {
					priorChild = pv
				}
				childVal, childDiags := decodeStringList(ctx, nested, childKey, priorChild)
				diags.Append(childDiags...)
				attrs[childKey] = childVal
				continue
			}
			// GoDuration is deliberately NOT dispatched here: every
			// duration leaf (radius/usg) is a top-level scalar, so section
			// converters call decodeGoDuration directly. A future nested
			// duration leaf would correctly fail loudly on this guard.
			diags.AddError(
				"Unsupported nested attribute type",
				fmt.Sprintf("field %q: unsupported nested attribute type %T", childDiagPath, childType),
			)
		}
	}
	if diags.HasError() {
		return types.ObjectUnknown(attrTypes), diags
	}

	obj, objDiags := types.ObjectValue(attrTypes, attrs)
	diags.Append(objDiags...)
	return obj, diags
}

// overlayObject writes cfg's children onto out[key]'s nested map. A
// null/unknown cfg is a no-op: the snapshot's nested map is left untouched.
func overlayObject(
	ctx context.Context,
	out map[string]any,
	key string,
	cfg types.Object,
) diag.Diagnostics {
	var diags diag.Diagnostics

	if cfg.IsNull() || cfg.IsUnknown() {
		return diags
	}

	nested, ok := out[key].(map[string]any)
	if !ok {
		nested = map[string]any{}
		out[key] = nested
	}

	diags.Append(overlayObjectFields(ctx, nested, key, cfg)...)
	return diags
}

// overlayObjectFields applies cfg's children directly onto nested (already
// unwrapped from its parent key), type-dispatching each child (from cfg's
// own attribute types, so overlay dispatches structurally identically to
// decodeObjectFields) and recursing into nested object-lists. diagPath is
// the possibly-indexed path used only in diagnostics.
func overlayObjectFields(
	ctx context.Context,
	nested map[string]any,
	diagPath string,
	cfg types.Object,
) diag.Diagnostics {
	var diags diag.Diagnostics

	attrTypes := cfg.AttributeTypes(ctx)
	cfgAttrs := cfg.Attributes()

	for childKey, childType := range attrTypes {
		childDiagPath := diagPath + "." + childKey
		childAttr := cfgAttrs[childKey]

		switch t := childType.(type) {
		case types.ListType:
			if _, isObjElem := t.ElemType.(types.ObjectType); isObjElem {
				lv, ok := childAttr.(types.List)
				if !ok {
					lv = types.ListNull(t.ElemType)
				}
				childDiags := overlayObjectList(ctx, nested, childKey, lv)
				diags.Append(childDiags...)
				continue
			}
		}

		switch childType {
		case types.StringType:
			sv, ok := childAttr.(types.String)
			if !ok {
				sv = types.StringUnknown()
			}
			overlayString(nested, childKey, sv)
		case types.BoolType:
			bv, ok := childAttr.(types.Bool)
			if !ok {
				bv = types.BoolUnknown()
			}
			overlayBool(nested, childKey, bv)
		case types.Int64Type:
			iv, ok := childAttr.(types.Int64)
			if !ok {
				iv = types.Int64Unknown()
			}
			overlayInt64(nested, childKey, iv)
		default:
			if lt, ok := childType.(types.ListType); ok && lt.ElemType == types.StringType {
				lv, ok := childAttr.(types.List)
				if !ok {
					lv = types.ListUnknown(types.StringType)
				}
				listDiags := overlayStringList(ctx, nested, childKey, lv)
				diags.Append(listDiags...)
				continue
			}
			// GoDuration is deliberately NOT dispatched here: every
			// duration leaf (radius/usg) is a top-level scalar, so section
			// converters call overlayGoDuration directly. A future nested
			// duration leaf would correctly fail loudly on this guard.
			diags.AddError(
				"Unsupported nested attribute type",
				fmt.Sprintf("field %q: unsupported nested attribute type %T", childDiagPath, childType),
			)
		}
	}

	return diags
}

// decodeObjectList reads a nested list-of-object field from data, recursing
// each element's children. Element order follows the API response. Absent
// key or explicit JSON null decodes to a null list of elemType. A present
// non-array value, or a non-object element, is remote type drift: a WARNING
// (not error), retaining the prior list wholesale — never a
// partially-decoded list. An elemType that is not an ObjectType is a
// provider/schema defect (a decodeObjectList call site bug, not remote
// drift), so it stays a hard error.
func decodeObjectList(
	ctx context.Context,
	data map[string]any,
	key string,
	prior types.List,
	elemType attr.Type,
) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics

	objType, ok := elemType.(types.ObjectType)
	if !ok {
		diags.AddError(
			"Unsupported list element type",
			fmt.Sprintf("field %q: decodeObjectList requires an ObjectType element, got %T", key, elemType),
		)
		return types.ListUnknown(elemType), diags
	}

	raw, ok := data[key]
	if !ok || raw == nil {
		return types.ListNull(elemType), diags
	}
	items, ok := raw.([]any)
	if !ok {
		diags.AddWarning(
			"Settings value type drift",
			fmt.Sprintf("field %q: expected array, got %T; retaining last-known value", key, raw),
		)
		return prior, diags
	}

	var priorElems []attr.Value
	if !prior.IsNull() && !prior.IsUnknown() {
		priorElems = prior.Elements()
	}

	elems := make([]attr.Value, 0, len(items))
	for i, item := range items {
		elemMap, ok := item.(map[string]any)
		if !ok {
			diags.AddWarning(
				"Settings value type drift",
				fmt.Sprintf("field %q: element %d: expected object, got %T; retaining last-known value", key, i, item),
			)
			return prior, diags
		}
		priorElem := types.ObjectNull(objType.AttrTypes)
		if i < len(priorElems) {
			if pe, ok := priorElems[i].(types.Object); ok {
				priorElem = pe
			}
		}
		elemVal, elemDiags := decodeObjectFields(
			ctx,
			elemMap,
			fmt.Sprintf("%s[%d]", key, i),
			priorElem,
			objType.AttrTypes,
		)
		diags.Append(elemDiags...)
		elems = append(elems, elemVal)
	}
	if diags.HasError() {
		return types.ListUnknown(elemType), diags
	}

	list, listDiags := types.ListValue(elemType, elems)
	diags.Append(listDiags...)
	return list, diags
}

// overlayObjectList writes cfg's elements onto out[key] as a list of nested
// maps, applying overlayObjectFields' per-child dispatch (including
// recursion into any further-nested object-list child) to each element.
// Element order follows cfg. A null/unknown cfg is a no-op: the snapshot's
// list is left untouched.
//
// Each output element is built FRESH (starting from an empty map), never
// seeded from the base snapshot's same-index element. List position is not
// a stable identity for an element — reordering or replacing an element
// shifts every later index — so seeding elemOut from out[key][i] would
// silently re-attach whatever unmodeled, controller-owned fields lived at
// that base index onto a DIFFERENT logical element (mgmt.ssh_keys is the
// concrete case: the controller-assigned date/fingerprint would end up
// mis-attached to the wrong key on reorder). A section that needs an
// unmodeled per-element field preserved (e.g. mgmt.ssh_keys' date/
// fingerprint) must do so explicitly and deliberately in its own overlay()
// — see setting_section_mgmt.go — not rely on a codec-level positional
// default. This also means overlayObjectList never aliases or mutates any
// base element map: each elemOut is a brand-new map, so the no-source-
// mutation property holds trivially, independent of dataCopy's semantics.
func overlayObjectList(
	ctx context.Context,
	out map[string]any,
	key string,
	cfg types.List,
) diag.Diagnostics {
	var diags diag.Diagnostics

	if cfg.IsNull() || cfg.IsUnknown() {
		return diags
	}

	cfgElems := cfg.Elements()
	items := make([]any, 0, len(cfgElems))
	for i, ce := range cfgElems {
		cfgObj, ok := ce.(types.Object)
		if !ok {
			diags.AddError(
				"Unsupported list element type",
				fmt.Sprintf("field %q: element %d: expected object, got %T", key, i, ce),
			)
			continue
		}

		elemOut := map[string]any{}
		elemDiags := overlayObjectFields(ctx, elemOut, fmt.Sprintf("%s[%d]", key, i), cfgObj)
		diags.Append(elemDiags...)
		items = append(items, elemOut)
	}
	if diags.HasError() {
		return diags
	}

	out[key] = items
	return diags
}
