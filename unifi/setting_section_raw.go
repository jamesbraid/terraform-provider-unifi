package unifi

import (
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Helpers for sections that convert to/from the raw rest/setting JSON
// document (settings.RawSetting.Data, a map[string]any decoded by
// encoding/json — numbers arrive as float64).

// setRawString writes the value only if it is user-set; null/unknown leaves
// the remote value untouched.
func setRawString(data map[string]any, key string, v types.String) {
	if !v.IsNull() && !v.IsUnknown() {
		data[key] = v.ValueString()
	}
}

func setRawBool(data map[string]any, key string, v types.Bool) {
	if !v.IsNull() && !v.IsUnknown() {
		data[key] = v.ValueBool()
	}
}

func setRawInt(data map[string]any, key string, v types.Int64) {
	if !v.IsNull() && !v.IsUnknown() {
		data[key] = v.ValueInt64()
	}
}

// rawString reads a string field; absent or empty maps to null, mirroring
// util.StringValueOrNull on the typed read path.
func rawString(data map[string]any, key string) types.String {
	if v, ok := data[key].(string); ok && v != "" {
		return types.StringValue(v)
	}
	return types.StringNull()
}

// rawBool reads a bool field; absent maps to null.
func rawBool(data map[string]any, key string) types.Bool {
	if v, ok := data[key].(bool); ok {
		return types.BoolValue(v)
	}
	return types.BoolNull()
}

// rawInt reads a numeric field, tolerating the representations UniFi
// controllers use interchangeably (JSON number → float64, or numeric
// string); absent or non-numeric maps to null.
func rawInt(data map[string]any, key string) types.Int64 {
	switch v := data[key].(type) {
	case float64:
		return types.Int64Value(int64(v))
	case int:
		return types.Int64Value(int64(v))
	case int64:
		return types.Int64Value(v)
	case string:
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return types.Int64Value(n)
		}
	}
	return types.Int64Null()
}

// rawStringList reads a []any-of-strings field; absent maps to null.
func rawStringList(data map[string]any, key string) types.List {
	raw, ok := data[key].([]any)
	if !ok {
		return types.ListNull(types.StringType)
	}
	elems := make([]attr.Value, 0, len(raw))
	for _, e := range raw {
		if s, ok := e.(string); ok {
			elems = append(elems, types.StringValue(s))
		}
	}
	// All elements are types.String; construction cannot fail.
	return types.ListValueMust(types.StringType, elems)
}

// anyRawKey reports whether any of the keys exists in the raw document —
// used to decide whether a nested block materializes or stays null.
func anyRawKey(data map[string]any, keys ...string) bool {
	for _, k := range keys {
		if _, ok := data[k]; ok {
			return true
		}
	}
	return false
}
