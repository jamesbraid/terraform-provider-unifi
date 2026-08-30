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

// settingsPackagePath is the -package value an sdk-bootstrap invocation must
// be given for newSDKConstraints to try settings.FieldConstraints at all --
// see newSDKConstraints for why that gate exists.
const settingsPackagePath = "github.com/ubiquiti-community/go-unifi/unifi/settings"

// newSDKConstraints builds production's constraintLookup for one
// invocation, gated on pkgPath (the -package flag that invocation was
// given): unifi.FieldConstraints covers the top-level package and is always
// tried; settings.FieldConstraints, tried bare then "Setting"-prefixed, is
// tried at all ONLY when pkgPath is settingsPackagePath.
//
// That gate exists because the prefixed fallback is a real source of
// ambiguity, not a theoretical one. unifi.Dashboard is a real top-level SDK
// struct (unifi/dashboard.generated.go) sitting alongside
// settings.FieldConstraints["SettingDashboard"]; an ungated invocation
// against the top-level package (-package .../unifi -struct Dashboard)
// would silently apply settings constraints to that unrelated struct's
// fields if any of its wire names happened to collide with
// SettingDashboard's. It is inert only by chance today -- unifi.Dashboard
// has no layout_preference field -- and gating on the invoked package
// removes the chance rather than relying on it.
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
func newSDKConstraints(pkgPath string) constraintLookup {
	settingsPackage := pkgPath == settingsPackagePath
	return func(goType, wire string) (unifi.FieldConstraint, bool) {
		if byWire, ok := unifi.FieldConstraints[goType]; ok {
			if constraint, ok := byWire[wire]; ok {
				return constraint, true
			}
		}
		if !settingsPackage {
			return unifi.FieldConstraint{}, false
		}
		if constraint, ok := settingsConstraint(goType, wire); ok {
			return constraint, true
		}
		if constraint, ok := settingsConstraint("Setting"+goType, wire); ok {
			return constraint, true
		}
		return unifi.FieldConstraint{}, false
	}
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
