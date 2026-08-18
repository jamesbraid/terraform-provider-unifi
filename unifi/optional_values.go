package unifi

// Shared helpers for optional pointer-valued API fields.
//
// THEY LIVED IN site_to_site_vpn_resource.go, which is where they were first
// needed and not where they belong: network_resource.go uses optStr too, and
// cutting site_to_site_vpn over to the kit deleted the file out from under it.
// A package-level helper sitting in one surface's file is a coupling nothing
// declares and the compiler only reports once the file moves.
//
// optInt64 and stringPtrOrNull went with that cutover: their only remaining
// callers were tests of mapper functions the descriptor replaced. optInt64's
// rule -- nil for zero, not just for null -- did not go with them; it is
// Int64PtrField's Elide in the descriptor now.
// optStr returns the string pointer for an optional attribute, or nil when the
// value is null, unknown, or empty — so the marshaler omits it rather than
// sending "" (which the controller rejects for IP/enum fields).
// optStr accepts any framework string-backed value (types.String or the custom
// nettypes values, which all embed basetypes.StringValue) and returns nil for
// null/unknown/empty.
func optStr(s interface {
	IsNull() bool
	IsUnknown() bool
	ValueString() string
},
) *string {
	if s.IsNull() || s.IsUnknown() || s.ValueString() == "" {
		return nil
	}
	v := s.ValueString()
	return &v
}
