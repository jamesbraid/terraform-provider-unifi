package util

import (
	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	"github.com/hashicorp/terraform-plugin-framework-nettypes/iptypes"
)

// This file provides read-side conversion helpers that mirror StringValueOrNull
// for the custom string types from terraform-plugin-framework-nettypes. The
// UniFi API represents missing values as empty strings, so every helper maps an
// empty string to the corresponding null value.
//
// Write-side conversion does not need dedicated helpers: every nettypes value
// embeds basetypes.StringValue, so .ValueString(), .IsNull() and .IsUnknown()
// are available directly on the model fields.

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

// IPValueOrNull returns an iptypes.IPAddress (IPv4 or IPv6), null when empty.
func IPValueOrNull(val string) iptypes.IPAddress {
	if val == "" {
		return iptypes.NewIPAddressNull()
	}
	return iptypes.NewIPAddressValue(val)
}

// Pointer variants for APIs that expose *string fields. A nil or empty-string
// pointer maps to the corresponding null value.

// IPv4PtrValueOrNull returns an iptypes.IPv4Address from a *string.
func IPv4PtrValueOrNull(val *string) iptypes.IPv4Address {
	if val == nil {
		return iptypes.NewIPv4AddressNull()
	}
	return IPv4ValueOrNull(*val)
}
