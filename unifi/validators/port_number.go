package validators

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// PortRangeListValidator validates a comma-separated list of ports and/or
// port ranges (e.g. "80,443" or "8000-8100"). Each port must be in 1-65535
// (0 and values above 65535 are rejected); each range's start must not
// exceed its end. Null, unknown, and empty values are skipped — this
// validator only checks semantic range validity, not presence.
func PortRangeListValidator() validator.String {
	return &portRangeListValidator{}
}

type portRangeListValidator struct{}

func (v portRangeListValidator) Description(context.Context) string {
	return "each port must be 1-65535 and each range's start must not exceed its end"
}

func (v portRangeListValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v portRangeListValidator) ValidateString(
	ctx context.Context,
	req validator.StringRequest,
	resp *validator.StringResponse,
) {
	if req.ConfigValue.IsUnknown() || req.ConfigValue.IsNull() {
		return
	}
	value := req.ConfigValue.ValueString()
	if value == "" {
		return
	}

	for _, segment := range strings.Split(value, ",") {
		start, end, isRange := strings.Cut(segment, "-")
		startVal, err := strconv.ParseUint(start, 10, 16)
		if err != nil || startVal == 0 {
			resp.Diagnostics.AddAttributeError(
				req.Path,
				"Invalid Port",
				fmt.Sprintf("Value %q in %q is not a valid port (must be 1-65535).", start, value),
			)
			continue
		}
		if !isRange {
			continue
		}
		endVal, err := strconv.ParseUint(end, 10, 16)
		if err != nil || endVal == 0 {
			resp.Diagnostics.AddAttributeError(
				req.Path,
				"Invalid Port",
				fmt.Sprintf("Value %q in %q is not a valid port (must be 1-65535).", end, value),
			)
			continue
		}
		if startVal > endVal {
			resp.Diagnostics.AddAttributeError(
				req.Path,
				"Invalid Port Range",
				fmt.Sprintf("Range %q in %q has start (%d) greater than end (%d).",
					segment, value, startVal, endVal),
			)
		}
	}
}
