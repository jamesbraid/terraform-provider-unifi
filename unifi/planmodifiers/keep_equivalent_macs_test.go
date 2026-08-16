package planmodifiers

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// TestMACSetsEqual guards the AP group refresh path. device_macs keeps whatever
// representation the practitioner wrote. A Set identifies elements by their
// string value, so the element type's semantic equality never reaches the set
// itself: without this comparison the read replaces "AA-BB-.." with the
// controller's "aa:bb:.." and leaves a diff no apply can settle.
func TestMACSetsEqual(t *testing.T) {
	ctx := context.Background()

	set := func(macs ...string) types.Set {
		// A nil slice reflects into a null Set, which is a different case; keep
		// the no-argument form meaning "empty".
		if macs == nil {
			macs = []string{}
		}
		v, d := types.SetValueFrom(ctx, hwtypes.MACAddressType{}, macs)
		if d.HasError() {
			t.Fatalf("building set: %v", d)
		}
		return v
	}

	tests := []struct {
		name    string
		current types.Set
		api     []string
		want    bool
	}{
		{
			name:    "same addresses, different separator and case",
			current: set("76-5A-86-93-5D-A4"),
			api:     []string{"76:5a:86:93:5d:a4"},
			want:    true,
		},
		{
			name:    "same addresses, different order",
			current: set("aa:bb:cc:dd:ee:ff", "11:22:33:44:55:66"),
			api:     []string{"11:22:33:44:55:66", "aa:bb:cc:dd:ee:ff"},
			want:    true,
		},
		{
			name:    "identical",
			current: set("aa:bb:cc:dd:ee:ff"),
			api:     []string{"aa:bb:cc:dd:ee:ff"},
			want:    true,
		},
		{
			name:    "different address",
			current: set("aa:bb:cc:dd:ee:ff"),
			api:     []string{"11:22:33:44:55:66"},
			want:    false,
		},
		{
			name:    "member added remotely",
			current: set("aa:bb:cc:dd:ee:ff"),
			api:     []string{"aa:bb:cc:dd:ee:ff", "11:22:33:44:55:66"},
			want:    false,
		},
		{
			name:    "member removed remotely",
			current: set("aa:bb:cc:dd:ee:ff", "11:22:33:44:55:66"),
			api:     []string{"aa:bb:cc:dd:ee:ff"},
			want:    false,
		},
		{
			name:    "both empty",
			current: set(),
			api:     []string{},
			want:    true,
		},
		{
			name:    "null state takes the controller value",
			current: types.SetNull(hwtypes.MACAddressType{}),
			api:     []string{"aa:bb:cc:dd:ee:ff"},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MACSetsEqual(ctx, tt.current, tt.api); got != tt.want {
				t.Errorf("MACSetsEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}
