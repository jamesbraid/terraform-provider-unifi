package unifi

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// FORTY Configure METHODS READ THE PROVIDER'S CLIENT BY HAND, and the nil check
// in front of the type assertion is the part that matters: the framework calls
// Configure once before the provider itself is configured, with ProviderData
// nil, and a surface that reports an error there fails every plan. Every one of
// the forty got it right, which is exactly why it should be written once --
// forty chances to get it wrong is the cost, not forty wrong answers.
//
// THREE FUNCTIONS RATHER THAN ONE WITH A KIND ARGUMENT. The summary line
// differs by surface kind and reaches the practitioner, so it is pinned by the
// function name instead of by a string a caller passes in. A wrapper that took
// "Resource" would let a data source produce a resource's diagnostic, and
// nothing would report it.

func resourceClient(data any, diags *diag.Diagnostics) (*Client, bool) {
	return providerClient(data, "Resource", diags)
}

func dataSourceClient(data any, diags *diag.Diagnostics) (*Client, bool) {
	return providerClient(data, "Data Source", diags)
}

func actionClient(data any, diags *diag.Diagnostics) (*Client, bool) {
	return providerClient(data, "Action", diags)
}

// providerClient returns false for a nil ProviderData WITHOUT a diagnostic,
// which is the pre-configure call and not an error. Every other failure is.
func providerClient(data any, kind string, diags *diag.Diagnostics) (*Client, bool) {
	if data == nil {
		return nil, false
	}
	client, ok := data.(*Client)
	if !ok {
		diags.AddError(
			"Unexpected "+kind+" Configure Type",
			fmt.Sprintf(
				"Expected *Client, got: %T. Please report this issue to the provider developers.",
				data,
			),
		)
		return nil, false
	}
	return client, true
}
