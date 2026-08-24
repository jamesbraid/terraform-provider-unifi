package unifi

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
)

func resourceClient(data any, diags *diag.Diagnostics) (*Client, bool) {
	return providerClient(data, "Resource", diags)
}

func dataSourceClient(data any, diags *diag.Diagnostics) (*Client, bool) {
	return providerClient(data, "Data Source", diags)
}

// dataSourceWithClient is embedded by every data source: none of them wire
// anything surface-specific in Configure (unlike a kit resource's Backend),
// so the twelve copies of this method were byte-identical -- one embedded
// implementation replaces all twelve.
type dataSourceWithClient struct {
	client *Client
}

func (d *dataSourceWithClient) Configure(
	_ context.Context,
	req datasource.ConfigureRequest,
	resp *datasource.ConfigureResponse,
) {
	client, ok := dataSourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	d.client = client
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
