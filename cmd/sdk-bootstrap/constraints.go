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
//
// settings.FieldConstraints is keyed by the SDK generator's canonical
// resource name, which for every settings type carries a "Setting" prefix
// (SettingGlobalSwitch, SettingAutoSpeedtest). The generator strips that
// prefix ONLY when emitting a top-level settings resource's own Go type
// (go-unifi's cmd/fields/main.go, ResourceInfo.CleanStructName:
// strings.TrimPrefix(structName, "Setting")); a nested settings struct's Go
// type keeps the full canonical name, so it already matches the table key.
// That is why nested lookups (e.g. "SettingDashboardWidgets") have always
// worked and top-level ones (e.g. "GlobalSwitch") never did: the bare Go
// type name is missing exactly the prefix the table key carries.
//
// CleanStructName lives in an unexported main package in the SDK repo, so
// there is no importable rule to call instead -- this re-derives the same
// convention rather than reusing the generator's own code. That is a second
// copy of a fact this package doesn't own; if the SDK ever changes how it
// strips the prefix, this must change with it.
func sdkConstraints(goType, wire string) (unifi.FieldConstraint, bool) {
	if byWire, ok := unifi.FieldConstraints[goType]; ok {
		if constraint, ok := byWire[wire]; ok {
			return constraint, true
		}
	}
	if constraint, ok := settingsConstraint(goType, wire); ok {
		return constraint, true
	}
	if constraint, ok := settingsConstraint("Setting"+goType, wire); ok {
		return constraint, true
	}
	return unifi.FieldConstraint{}, false
}

// settingsConstraint looks goType up in settings.FieldConstraints exactly as
// given -- no prefix logic here, so a caller can try both the bare and the
// "Setting"-prefixed form without this function knowing which is which.
func settingsConstraint(goType, wire string) (unifi.FieldConstraint, bool) {
	byWire, ok := settings.FieldConstraints[goType]
	if !ok {
		return unifi.FieldConstraint{}, false
	}
	constraint, ok := byWire[wire]
	return unifi.FieldConstraint(constraint), ok
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
