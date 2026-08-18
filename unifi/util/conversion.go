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

func Ptr[T any](in T) *T {
	return &in
}
