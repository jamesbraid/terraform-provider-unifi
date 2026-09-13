package util

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// StringValueOrNull returns types.StringValue if not empty, otherwise types.StringNull.
func StringValueOrNull(val string) types.String {
	if val == "" {
		return types.StringNull()
	}
	return types.StringValue(val)
}

// OmitZeroInt64Pointer returns nil for a null, unknown, or zero value, and a
// pointer to the value otherwise -- the OmitZero rule of resourcekit's
// Int64PtrField, spelled out for a nested block member encoded by hand. A plain
// ValueInt64Pointer() returns nil only for null and hands back a pointer to zero
// for unknown (its internal value is the Go zero), so an unset Optional+Computed
// number reaches the controller as a literal 0. Where the field's constraint
// rejects 0 (a port, a channel width) that write fails with api.err.InvalidValue;
// dropping the zero lets the field's omitempty tag omit it so the controller
// keeps what it holds.
func OmitZeroInt64Pointer(val types.Int64) *int64 {
	if val.IsNull() || val.IsUnknown() || val.ValueInt64() == 0 {
		return nil
	}
	return Ptr(val.ValueInt64())
}

func Ptr[T any](in T) *T {
	return &in
}
