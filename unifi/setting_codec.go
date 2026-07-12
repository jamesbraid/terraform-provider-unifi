package unifi

import (
	"context"
	"fmt"
	"math"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// setting_codec.go is the ONE place that translates between the controller's
// raw `map[string]any` settings representation (as decoded from JSON) and
// terraform-plugin-framework typed values. No section converter may read or
// write a map[string]any directly; everything goes through the functions in
// this file.
//
// Two layers:
//
//   - Low-level codec* / put* functions are ownership-agnostic typed
//     accessors. They encode the raw-data contract: present-empty is a real
//     value (not absent/null), absent or explicit JSON null is TF-null, and a
//     wrong or fractional type is a diagnostic rather than a silent
//     normalization.
//
//   - Ownership-aware decode* / overlay* wrappers apply the C1 field-ownership
//     policy (unifi/setting_ownership.go) on top of the low-level codec by
//     branching on ownershipClass. Section converters call ONLY this layer.

// ---------------------------------------------------------------------------
// Low-level codec: map[string]any -> types.X
// ---------------------------------------------------------------------------

// codecString reads a string field from data. Absent key or explicit JSON
// null decodes to types.StringNull(); present-empty ("") decodes to
// StringValue(""), never collapsed to null. A present value of any other Go
// type is a diagnostic, not a silent conversion.
func codecString(data map[string]any, key string) (types.String, diag.Diagnostics) {
	var diags diag.Diagnostics
	raw, ok := data[key]
	if !ok || raw == nil {
		return types.StringNull(), diags
	}
	s, ok := raw.(string)
	if !ok {
		diags.AddError(
			"Malformed settings value",
			fmt.Sprintf("field %q: expected string, got %T", key, raw),
		)
		return types.StringUnknown(), diags
	}
	return types.StringValue(s), diags
}

// codecBool reads a bool field from data. Absent/null decodes to
// types.BoolNull(); a present value of any other Go type is a diagnostic.
func codecBool(data map[string]any, key string) (types.Bool, diag.Diagnostics) {
	var diags diag.Diagnostics
	raw, ok := data[key]
	if !ok || raw == nil {
		return types.BoolNull(), diags
	}
	b, ok := raw.(bool)
	if !ok {
		diags.AddError(
			"Malformed settings value",
			fmt.Sprintf("field %q: expected bool, got %T", key, raw),
		)
		return types.BoolUnknown(), diags
	}
	return types.BoolValue(b), diags
}

// codecInt64 reads an int64 field from data. JSON numbers decode to
// float64 in Go; a fractional value (e.g. 1.9) is malformed for an int64
// field and raises a diagnostic instead of being silently truncated. Absent
// key or explicit JSON null decodes to types.Int64Null().
func codecInt64(data map[string]any, key string) (types.Int64, diag.Diagnostics) {
	var diags diag.Diagnostics
	raw, ok := data[key]
	if !ok || raw == nil {
		return types.Int64Null(), diags
	}
	f, ok := raw.(float64)
	if !ok {
		diags.AddError(
			"Malformed settings value",
			fmt.Sprintf("field %q: expected number, got %T", key, raw),
		)
		return types.Int64Unknown(), diags
	}
	if math.Trunc(f) != f {
		diags.AddError(
			"Malformed settings value",
			fmt.Sprintf("field %q: expected integer, got fractional value %v", key, f),
		)
		return types.Int64Unknown(), diags
	}
	return types.Int64Value(int64(f)), diags
}

// codecStringList reads a list-of-string field from data. Absent/null
// decodes to types.ListNull(); present-empty ([]) decodes to an empty
// ListValue, not null. A present value that is not a []any, or an element
// that is not a string, is a diagnostic.
func codecStringList(ctx context.Context, data map[string]any, key string) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics
	raw, ok := data[key]
	if !ok || raw == nil {
		return types.ListNull(types.StringType), diags
	}
	items, ok := raw.([]any)
	if !ok {
		diags.AddError(
			"Malformed settings value",
			fmt.Sprintf("field %q: expected array, got %T", key, raw),
		)
		return types.ListUnknown(types.StringType), diags
	}
	elems := make([]attr.Value, 0, len(items))
	for i, item := range items {
		s, ok := item.(string)
		if !ok {
			diags.AddError(
				"Malformed settings value",
				fmt.Sprintf("field %q: element %d: expected string, got %T", key, i, item),
			)
			return types.ListUnknown(types.StringType), diags
		}
		elems = append(elems, types.StringValue(s))
	}
	list, listDiags := types.ListValue(types.StringType, elems)
	diags.Append(listDiags...)
	return list, diags
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
// Ownership-aware layer: decode (data -> state) and overlay (config -> PUT
// body), branching on ownershipClass per the C1 policy in
// unifi/setting_ownership.go.
// ---------------------------------------------------------------------------

// decodeString reads key's value into state per class. A field that does
// not read from the API (ownerWriteOnlySecret) preserves prior state
// unconditionally and never inspects data — the controller does not return
// secret values, so anything present in data for that key is a mask, not
// truth.
func decodeString(data map[string]any, key string, class ownershipClass, prior types.String) (types.String, diag.Diagnostics) {
	if !class.readsFromAPI() {
		return prior, nil
	}
	return codecString(data, key)
}

// decodeInt64 is the int64 analogue of decodeString.
func decodeInt64(data map[string]any, key string, class ownershipClass, prior types.Int64) (types.Int64, diag.Diagnostics) {
	if !class.readsFromAPI() {
		return prior, nil
	}
	return codecInt64(data, key)
}

// decodeBool is the bool analogue of decodeString.
func decodeBool(data map[string]any, key string, class ownershipClass, prior types.Bool) (types.Bool, diag.Diagnostics) {
	if !class.readsFromAPI() {
		return prior, nil
	}
	return codecBool(data, key)
}

// decodeStringList is the string-list analogue of decodeString.
func decodeStringList(ctx context.Context, data map[string]any, key string, class ownershipClass, prior types.List) (types.List, diag.Diagnostics) {
	if !class.readsFromAPI() {
		return prior, nil
	}
	return codecStringList(ctx, data, key)
}

// overlayString applies v onto out[key] per the C1 write policy:
//
//   - write-only secret (class == ownerWriteOnlySecret): a null or unknown
//     config value means "not being changed this apply" — but the copied
//     base snapshot may hold a masked/stale secret from the read-back
//     (e.g. "******"), and that must NEVER be re-sent to the controller.
//     So the key is deleted from out entirely, omitting it from the PUT
//     body (the controller keeps its stored value for an omitted key). A
//     configured value, including an explicit empty string, IS written —
//     that is an intentional clear/rotate-to-empty.
//   - managed/co-managed (writesToPUT() true, not the secret class): write
//     v when present (including empty); a null/unknown config leaves the
//     snapshot's copied value in out untouched, preserving the controller
//     value across this apply.
//   - computed/generated-secret/preserved (writesToPUT() false): no write,
//     no delete — the snapshot value already in out is the truth for these
//     controller-owned, read-back fields.
func overlayString(out map[string]any, key string, class ownershipClass, v types.String) {
	if class == ownerWriteOnlySecret {
		if v.IsNull() || v.IsUnknown() {
			delete(out, key)
			return
		}
		putString(out, key, v)
		return
	}
	if class.writesToPUT() {
		putString(out, key, v)
	}
}

// overlayInt64 is the int64 analogue of overlayString.
func overlayInt64(out map[string]any, key string, class ownershipClass, v types.Int64) {
	if class == ownerWriteOnlySecret {
		if v.IsNull() || v.IsUnknown() {
			delete(out, key)
			return
		}
		putInt64(out, key, v)
		return
	}
	if class.writesToPUT() {
		putInt64(out, key, v)
	}
}

// overlayBool is the bool analogue of overlayString.
func overlayBool(out map[string]any, key string, class ownershipClass, v types.Bool) {
	if class == ownerWriteOnlySecret {
		if v.IsNull() || v.IsUnknown() {
			delete(out, key)
			return
		}
		putBool(out, key, v)
		return
	}
	if class.writesToPUT() {
		putBool(out, key, v)
	}
}

// overlayStringList is the string-list analogue of overlayString.
func overlayStringList(ctx context.Context, out map[string]any, key string, class ownershipClass, v types.List) diag.Diagnostics {
	if class == ownerWriteOnlySecret {
		if v.IsNull() || v.IsUnknown() {
			delete(out, key)
			return nil
		}
		return putStringList(ctx, out, key, v)
	}
	if class.writesToPUT() {
		return putStringList(ctx, out, key, v)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Nested shapes: SingleNestedAttribute (object) and ListNestedAttribute
// (object list), each child/leaf keyed by its own ownershipClass, looked up
// by its FULL dotted path in the section's ownership() map.
//
// Design (Task 16b, codex-validated "Design 3"): a settingSection's
// ownership() map is already keyed by the schema's full dotted leaf path
// (see setting_section_test.go's leafPaths / the gate-10 coverage test) —
// e.g. "custom_servers.enabled" or "suppression_alerts.tracking.direction".
// List elements share ONE path with no "[i]" index, because ownership is a
// property of the schema shape, not of any one API response's element
// count. So instead of a caller pre-stripping a per-call child-only
// ownership map, these helpers take the section's FULL ownership map (own)
// plus ownPrefix: the dotted, index-free path of the object/list currently
// being decoded/overlaid within that map. A leaf child's lookup path is
// computed as ownPrefix+"."+child (or just child at the section's
// top-level, where ownPrefix is the top-level attribute name already).
//
// THE CRITICAL SPLIT this design depends on: ownPrefix (index-free, used
// ONLY for own[...] lookups) is a different string from diagPath (may
// include "[i]", used ONLY in diagnostic messages). Conflating them would
// make every list-element lookup miss (own has no "custom_servers[0]..."
// entry) and silently fall through to the zero-value ownershipClass
// (ownerManaged) if the missing-entry guard below were skipped — which is
// exactly why that guard is comma-ok and fails loud instead of defaulting.
// ---------------------------------------------------------------------------

// ownershipFor looks up path in own using the mandatory comma-ok form: a
// missing entry is a diagnostic, NEVER a silent fall-through to Go's
// zero-value ownershipClass (ownerManaged, iota 0). diagPath is used only
// in the error message so a caller can surface the indexed ("foo[2].bar")
// form to a human while path (index-free) is what was actually looked up.
func ownershipFor(own map[string]ownershipClass, path, diagPath string) (ownershipClass, diag.Diagnostics) {
	var diags diag.Diagnostics
	class, ok := own[path]
	if !ok {
		diags.AddError(
			"Missing ownership entry",
			fmt.Sprintf("field %q has no ownership() entry (looked up as %q)", diagPath, path),
		)
		return ownerManaged, diags
	}
	return class, diags
}

// leafPath joins prefix and child into a dotted ownership-lookup path,
// matching leafPaths' convention in setting_section_test.go: a top-level
// leaf's path is its own name; a nested leaf is "prefix.child".
func leafPath(prefix, child string) string {
	if prefix == "" {
		return child
	}
	return prefix + "." + child
}

// decodeObject reads a nested object field from data, recursing into each
// child leaf per its ownershipClass looked up from own by full dotted path
// (ownPrefix+"."+child). A child that does not read from the API (e.g. a
// nested write-only secret) is preserved from the corresponding attribute
// of prior. Absent key or explicit JSON null decodes to a null object of
// attrTypes. ownPrefix is this object's own dotted path within own (e.g.
// "dns_verification"); at a section's top level that is the attribute name.
func decodeObject(
	ctx context.Context,
	data map[string]any,
	key string,
	own map[string]ownershipClass,
	ownPrefix string,
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
		diags.AddError(
			"Malformed settings value",
			fmt.Sprintf("field %q: expected object, got %T", key, raw),
		)
		return types.ObjectUnknown(attrTypes), diags
	}

	return decodeObjectFields(ctx, nested, ownPrefix, key, own, prior, attrTypes)
}

// decodeObjectFields decodes attrTypes' children directly out of nested
// (already unwrapped from its parent key), type-dispatching each child and
// recursing into nested object-lists. ownPrefix is the index-free dotted
// path of this object within own (used for ownership lookups only);
// diagPath is the possibly-indexed path used only in diagnostic messages —
// see the package-level doc comment above for why these must not be
// conflated.
func decodeObjectFields(
	ctx context.Context,
	nested map[string]any,
	ownPrefix string,
	diagPath string,
	own map[string]ownershipClass,
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
		path := leafPath(ownPrefix, childKey)
		childDiagPath := leafPath(diagPath, childKey)

		switch t := childType.(type) {
		case types.ListType:
			if _, isObjElem := t.ElemType.(types.ObjectType); isObjElem {
				priorChild := types.ListNull(t.ElemType)
				if pv, ok := priorAttrs[childKey].(types.List); ok {
					priorChild = pv
				}
				childVal, childDiags := decodeObjectList(ctx, nested, childKey, own, path, priorChild, t.ElemType)
				diags.Append(childDiags...)
				attrs[childKey] = childVal
				continue
			}
		}

		class, classDiags := ownershipFor(own, path, childDiagPath)
		diags.Append(classDiags...)
		if classDiags.HasError() {
			continue
		}

		switch childType {
		case types.StringType:
			priorChild := types.StringNull()
			if pv, ok := priorAttrs[childKey].(types.String); ok {
				priorChild = pv
			}
			childVal, childDiags := decodeString(nested, childKey, class, priorChild)
			diags.Append(childDiags...)
			attrs[childKey] = childVal
		case types.BoolType:
			priorChild := types.BoolNull()
			if pv, ok := priorAttrs[childKey].(types.Bool); ok {
				priorChild = pv
			}
			childVal, childDiags := decodeBool(nested, childKey, class, priorChild)
			diags.Append(childDiags...)
			attrs[childKey] = childVal
		case types.Int64Type:
			priorChild := types.Int64Null()
			if pv, ok := priorAttrs[childKey].(types.Int64); ok {
				priorChild = pv
			}
			childVal, childDiags := decodeInt64(nested, childKey, class, priorChild)
			diags.Append(childDiags...)
			attrs[childKey] = childVal
		default:
			if lt, ok := childType.(types.ListType); ok && lt.ElemType == types.StringType {
				priorChild := types.ListNull(types.StringType)
				if pv, ok := priorAttrs[childKey].(types.List); ok {
					priorChild = pv
				}
				childVal, childDiags := decodeStringList(ctx, nested, childKey, class, priorChild)
				diags.Append(childDiags...)
				attrs[childKey] = childVal
				continue
			}
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

// overlayObject writes cfg's children onto out[key]'s nested map per each
// child's ownershipClass looked up from own by full dotted path, following
// the same per-class branching as overlayString (including delete-on-null
// for a nested write-only-secret leaf). A null/unknown cfg is a no-op: the
// snapshot's nested map is left untouched. ownPrefix is this object's own
// dotted path within own, matching decodeObject's.
func overlayObject(
	ctx context.Context,
	out map[string]any,
	key string,
	own map[string]ownershipClass,
	ownPrefix string,
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

	diags.Append(overlayObjectFields(ctx, nested, ownPrefix, key, own, cfg)...)
	return diags
}

// overlayObjectFields applies cfg's children directly onto nested (already
// unwrapped from its parent key), type-dispatching each child (from cfg's
// own attribute types, so overlay dispatches structurally identically to
// decodeObjectFields) and recursing into nested object-lists. ownPrefix is
// the index-free dotted path of this object within own (ownership lookups
// only); diagPath is the possibly-indexed path used only in diagnostics.
func overlayObjectFields(
	ctx context.Context,
	nested map[string]any,
	ownPrefix string,
	diagPath string,
	own map[string]ownershipClass,
	cfg types.Object,
) diag.Diagnostics {
	var diags diag.Diagnostics

	attrTypes := cfg.AttributeTypes(ctx)
	cfgAttrs := cfg.Attributes()

	for childKey, childType := range attrTypes {
		path := leafPath(ownPrefix, childKey)
		childDiagPath := leafPath(diagPath, childKey)
		childAttr := cfgAttrs[childKey]

		switch t := childType.(type) {
		case types.ListType:
			if _, isObjElem := t.ElemType.(types.ObjectType); isObjElem {
				lv, ok := childAttr.(types.List)
				if !ok {
					lv = types.ListNull(t.ElemType)
				}
				childDiags := overlayObjectList(ctx, nested, childKey, own, path, lv)
				diags.Append(childDiags...)
				continue
			}
		}

		class, classDiags := ownershipFor(own, path, childDiagPath)
		diags.Append(classDiags...)
		if classDiags.HasError() {
			continue
		}

		switch childType {
		case types.StringType:
			sv, ok := childAttr.(types.String)
			if !ok {
				sv = types.StringUnknown()
			}
			overlayString(nested, childKey, class, sv)
		case types.BoolType:
			bv, ok := childAttr.(types.Bool)
			if !ok {
				bv = types.BoolUnknown()
			}
			overlayBool(nested, childKey, class, bv)
		case types.Int64Type:
			iv, ok := childAttr.(types.Int64)
			if !ok {
				iv = types.Int64Unknown()
			}
			overlayInt64(nested, childKey, class, iv)
		default:
			if lt, ok := childType.(types.ListType); ok && lt.ElemType == types.StringType {
				lv, ok := childAttr.(types.List)
				if !ok {
					lv = types.ListUnknown(types.StringType)
				}
				listDiags := overlayStringList(ctx, nested, childKey, class, lv)
				diags.Append(listDiags...)
				continue
			}
			diags.AddError(
				"Unsupported nested attribute type",
				fmt.Sprintf("field %q: unsupported nested attribute type %T", childDiagPath, childType),
			)
		}
	}

	return diags
}

// decodeObjectList reads a nested list-of-object field from data,
// recursing each element's children per own (looked up by ownPrefix, the
// list's own index-free dotted path — every element shares this same
// prefix, matching leafPaths' convention that list elements have no "[i]"
// in their ownership key). Element order follows the API response. A
// per-element write-only-secret leaf is preserved from the matching
// (same-index) element of prior; if prior has no element at that index,
// the leaf decodes via decodeString's normal preserve-from-null-prior
// behavior (StringNull()). Absent key or explicit JSON null decodes to a
// null list of elemType.
func decodeObjectList(
	ctx context.Context,
	data map[string]any,
	key string,
	own map[string]ownershipClass,
	ownPrefix string,
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
		diags.AddError(
			"Malformed settings value",
			fmt.Sprintf("field %q: expected array, got %T", key, raw),
		)
		return types.ListUnknown(elemType), diags
	}

	var priorElems []attr.Value
	if !prior.IsNull() && !prior.IsUnknown() {
		priorElems = prior.Elements()
	}

	elems := make([]attr.Value, 0, len(items))
	for i, item := range items {
		elemMap, ok := item.(map[string]any)
		if !ok {
			diags.AddError(
				"Malformed settings value",
				fmt.Sprintf("field %q: element %d: expected object, got %T", key, i, item),
			)
			continue
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
			ownPrefix,
			fmt.Sprintf("%s[%d]", key, i),
			own,
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

// overlayObjectList writes cfg's elements onto out[key] as a list of
// nested maps, applying overlayObjectFields' per-child branching (including
// delete-on-null for a per-element write-only-secret leaf, and recursion
// into any further-nested object-list child) to each element, all sharing
// ownPrefix (the list's own index-free dotted path in own). Element order
// follows cfg. Each element starts from the snapshot's same-index element
// (if any) so that a leaf omitted from cfg's element retains the
// snapshot's value for that element, exactly as overlayObject preserves
// untouched leaves within a single object. A null/unknown cfg is a no-op:
// the snapshot's list is left untouched.
//
// elemOut is seeded from the base snapshot's same-index element map
// (elemOut = m) rather than a copy of it: this is safe only because every
// caller's out originates from a rawSettings.dataCopy(...), which deep-
// copies the whole section tree up front, so mutating elemOut in place
// mutates that already-independent copy, never the snapshot itself. Do not
// call this with an out that isn't already a deep, private copy.
func overlayObjectList(
	ctx context.Context,
	out map[string]any,
	key string,
	own map[string]ownershipClass,
	ownPrefix string,
	cfg types.List,
) diag.Diagnostics {
	var diags diag.Diagnostics

	if cfg.IsNull() || cfg.IsUnknown() {
		return diags
	}

	var baseElems []any
	if existing, ok := out[key].([]any); ok {
		baseElems = existing
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
		if i < len(baseElems) {
			if m, ok := baseElems[i].(map[string]any); ok {
				elemOut = m
			}
		}

		elemDiags := overlayObjectFields(ctx, elemOut, ownPrefix, fmt.Sprintf("%s[%d]", key, i), own, cfgObj)
		diags.Append(elemDiags...)
		items = append(items, elemOut)
	}
	if diags.HasError() {
		return diags
	}

	out[key] = items
	return diags
}
