package main

import (
	unifi "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// fieldConstraint mirrors unifi.FieldConstraint in the bootstrap's own wire
// shape (snake_case, omitempty throughout) -- copied verbatim from the SDK's
// table, never recomputed here.
type fieldConstraint struct {
	Values      []string `json:"values,omitempty"`
	Int64Values []int64  `json:"int64_values,omitempty"`
	Pattern     string   `json:"pattern,omitempty"`
	Min         int64    `json:"min,omitempty"`
	Max         int64    `json:"max,omitempty"`
	HasBounds   bool     `json:"has_bounds,omitempty"`
	MinLength   int64    `json:"min_length,omitempty"`
	MaxLength   int64    `json:"max_length,omitempty"`
	HasLength   bool     `json:"has_length,omitempty"`
}

// constraintLookup finds the SDK's validation facts for one field, keyed by
// its declaring struct's Go type name and its wire (JSON) name. Injected so
// tests can stub it without the real generated tables.
type constraintLookup func(goType, wire string) (unifi.FieldConstraint, bool)

// sdkConstraints is production's constraintLookup: unifi.FieldConstraints
// covers the top-level package, settings.FieldConstraints covers settings
// types. A Go type name lives in at most one of the two, so trying both is
// enough -- there's no ambiguity to resolve.
func sdkConstraints(goType, wire string) (unifi.FieldConstraint, bool) {
	if byWire, ok := unifi.FieldConstraints[goType]; ok {
		if constraint, ok := byWire[wire]; ok {
			return constraint, true
		}
	}
	if byWire, ok := settings.FieldConstraints[goType]; ok {
		if constraint, ok := byWire[wire]; ok {
			return unifi.FieldConstraint(constraint), true
		}
	}
	return unifi.FieldConstraint{}, false
}

// constraintFromSDK copies one FieldConstraint verbatim into the bootstrap's
// wire shape.
func constraintFromSDK(c unifi.FieldConstraint) *fieldConstraint {
	return &fieldConstraint{
		Values:      c.Values,
		Int64Values: c.Int64Values,
		Pattern:     c.Pattern,
		Min:         c.Min,
		Max:         c.Max,
		HasBounds:   c.HasBounds,
		MinLength:   c.MinLength,
		MaxLength:   c.MaxLength,
		HasLength:   c.HasLength,
	}
}
