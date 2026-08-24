package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_device "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_device"
)

// A listed device with no overrides must still say what its overrides are
// a set of: the list path starts from a zero model (no prior state to
// inherit a type from), and port_override is not a Field, so nothing on
// that path gave the null its element type. The framework then refused
// the whole list result: "types.SetType[!!! MISSING TYPE !!!]".
func TestDeviceListNullPortOverrideCarriesItsType(t *testing.T) {
	ctx := context.Background()
	var model deviceKitModel
	var prior deviceKitModel
	hook := deviceKitAfterReceive()
	if diags := hook(ctx, &ui.Device{}, &model, prior, nil); diags.HasError() {
		t.Fatalf("afterReceive on a zero model: %v", diags)
	}
	elem := model.PortOverride.ElementType(ctx)
	if elem == nil {
		t.Fatal("a listed device with no overrides carries an untyped null, which the " +
			"framework refuses to place into the typed schema")
	}
	block := resource_device.DeviceResourceSchema(ctx).Blocks["port_override"]
	if block == nil {
		t.Fatal("the served schema no longer declares a port_override block")
	}
	if got := (types.SetType{ElemType: elem}); !got.Equal(block.Type()) {
		t.Fatalf("the typed null disagrees with the served schema: got %v, want %v", got, block.Type())
	}
}
