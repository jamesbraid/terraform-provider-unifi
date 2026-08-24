package util

import (
	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	"github.com/hashicorp/terraform-plugin-framework-nettypes/iptypes"
)

// The UniFi API represents missing values as empty strings, so each helper
// below maps an empty string to the corresponding null value.
//
// There are no write-side equivalents: every nettypes value embeds
// basetypes.StringValue, so .ValueString(), .IsNull() and .IsUnknown() work
// directly on the model fields.

// MACValueOrNull returns a hwtypes.MACAddress, null when the string is empty.
func MACValueOrNull(val string) hwtypes.MACAddress {
	if val == "" {
		return hwtypes.NewMACAddressNull()
	}
	return hwtypes.NewMACAddressValue(val)
}

// IPv4ValueOrNull returns an iptypes.IPv4Address, null when the string is empty.
func IPv4ValueOrNull(val string) iptypes.IPv4Address {
	if val == "" {
		return iptypes.NewIPv4AddressNull()
	}
	return iptypes.NewIPv4AddressValue(val)
}
