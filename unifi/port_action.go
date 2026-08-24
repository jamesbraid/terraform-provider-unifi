package unifi

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/action/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/action_port"
)

// Ensure the implementation satisfies framework interfaces.
var (
	_ action.Action              = &portAction{}
	_ action.ActionWithConfigure = &portAction{}
)

// NewPortAction returns a new instance of the port action.
func NewPortAction() action.Action {
	return &portAction{}
}

type portAction struct {
	client *Client
}

// portActionModel describes the action request and response data model.
type portActionModel struct {
	DeviceMAC  hwtypes.MACAddress `tfsdk:"device_mac"`
	PortNumber types.Int64        `tfsdk:"port_number"`
	PoeMode    types.String       `tfsdk:"poe_mode"`
	Timeouts   timeouts.Value     `tfsdk:"timeouts"`
}

func mergePortOverride(
	existing []ui.DevicePortOverrides,
	portNumber int64,
	poeMode string,
) []ui.DevicePortOverrides {
	merged := slices.Clone(existing)
	for i := range merged {
		if merged[i].PortIDX != nil && *merged[i].PortIDX == portNumber {
			merged[i].PoeMode = poeMode
			return merged
		}
	}
	port := portNumber
	return append(merged, ui.DevicePortOverrides{PortIDX: &port, PoeMode: poeMode})
}

func (a *portAction) Metadata(
	ctx context.Context,
	req action.MetadataRequest,
	resp *action.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_port"
}

func (a *portAction) Schema(
	ctx context.Context,
	req action.SchemaRequest,
	resp *action.SchemaResponse,
) {
	resp.Schema = action_port.PortActionSchema(ctx)
	// timeouts is provider-owned and declared "generated": false, exactly as on
	// every managed surface: it comes from the framework's timeouts package
	// rather than from the specification, so the kernel grafts it here.
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(ctx)
}

func (a *portAction) Configure(
	ctx context.Context,
	req action.ConfigureRequest,
	resp *action.ConfigureResponse,
) {
	client, ok := actionClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}

	a.client = client
}

func (a *portAction) Invoke(
	ctx context.Context,
	req action.InvokeRequest,
	resp *action.InvokeResponse,
) {
	var config portActionModel

	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	invokeTimeout, timeoutDiags := config.Timeouts.Invoke(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, invokeTimeout)
	defer cancel()

	poeMode := config.PoeMode.ValueString()
	validPoeModes := []string{"auto", "pasv24", "passthrough", "off"}
	isValidPoeMode := slices.Contains(validPoeModes, poeMode)
	if !isValidPoeMode {
		resp.Diagnostics.AddError(
			"Invalid PoE Mode",
			fmt.Sprintf(
				"PoE mode must be one of: auto, pasv24, passthrough, off. Got: %s",
				poeMode,
			),
		)
		return
	}

	deviceMAC := config.DeviceMAC.ValueString()
	portNumber := config.PortNumber.ValueInt64()

	device, err := a.client.GetDeviceByMAC(ctx, a.client.Site, deviceMAC)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Finding Device",
			fmt.Sprintf("Could not find device with MAC address %s: %s", deviceMAC, err.Error()),
		)
		return
	}

	existingOverrides := device.PortOverrides
	if existingOverrides == nil {
		existingOverrides = []ui.DevicePortOverrides{}
	}

	device.PortOverrides = mergePortOverride(existingOverrides, portNumber, poeMode)
	_, err = a.client.UpdateDevice(ctx, a.client.Site, device)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Device Port",
			fmt.Sprintf(
				"Could not update port %d on device %s: %s",
				portNumber,
				deviceMAC,
				err.Error(),
			),
		)
		return
	}
}
