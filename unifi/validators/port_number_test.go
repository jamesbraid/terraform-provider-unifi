package validators

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestPortRangeListValidator_ValidateString(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"empty is skipped", "", false},
		{"single valid port", "443", false},
		{"comma list", "80,443", false},
		{"valid range", "8000-8100", false},
		{"mixed list and range", "80,443,8000-8100", false},
		{"max valid port", "65535", false},
		{"min valid port", "1", false},
		{"zero rejected", "0", true},
		{"zero in list rejected", "80,0", true},
		{"over max rejected", "65536", true},
		{"reversed range rejected", "443-80", true},
		{"zero start in range rejected", "0-100", true},
		{"zero end in range rejected", "100-0", true},
		{"equal range endpoints allowed", "80-80", false},
		// Malformed-string syntax cases (design doc §4.5: full syntax-AND-
		// semantics validation, not just range checks on well-shaped input).
		{"non-numeric rejected", "abc", true},
		{"more than one hyphen in a segment rejected", "1-2-3", true},
		{"empty segment from doubled comma rejected", "1,,2", true},
		{"missing range start rejected", "-1", true},
		{"missing range end rejected", "1-", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := validator.StringRequest{
				Path:        path.Root("port"),
				ConfigValue: types.StringValue(c.value),
			}
			resp := &validator.StringResponse{}
			PortRangeListValidator().ValidateString(context.Background(), req, resp)
			if resp.Diagnostics.HasError() != c.wantErr {
				t.Errorf("ValidateString(%q): HasError = %v, want %v (%v)",
					c.value, resp.Diagnostics.HasError(), c.wantErr, resp.Diagnostics)
			}
		})
	}
}

func TestPortRangeListValidator_NullAndUnknownSkipped(t *testing.T) {
	for _, cv := range []types.String{types.StringNull(), types.StringUnknown()} {
		req := validator.StringRequest{Path: path.Root("port"), ConfigValue: cv}
		resp := &validator.StringResponse{}
		PortRangeListValidator().ValidateString(context.Background(), req, resp)
		if resp.Diagnostics.HasError() {
			t.Errorf("null/unknown must be skipped, got: %v", resp.Diagnostics)
		}
	}
}
