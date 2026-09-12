package resourcekit

import (
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// DerivedElide is the Elide the generated schema implies for one attribute:
// the single source ElideProblems judges hand descriptors against, exported
// so a generator can emit the value the check would demand instead of
// re-deriving it and drifting. The second result is false when the schema
// declares neither an attribute nor a block of this name, in which case the
// first is meaningless.
//
// elidesTheEmptyString is field-kind-specific: string kinds elide the
// literal "", but DurationPtrField elides the number 0 (a value GoDuration
// holds happily as "0s"), so asking a validator about "" would ask the
// wrong question for it.
func DerivedElide(built schema.Schema, name string, elidesTheEmptyString bool) (ElideZero, bool) {
	attribute, ok := built.Attributes[name]
	if !ok {
		// A block is implicitly optional and can never be Computed, so the
		// rule that applies is the Optional-and-not-Computed one: NullZero,
		// since an absent block is an absence, not a configured empty.
		if _, isBlock := built.Blocks[name]; isBlock {
			return NullZero, true
		}
		return KeepZero, false
	}
	// Required and Computed-only keep the zero (config always supplies it,
	// or it must round-trip as given). Optional-not-Computed nulls it.
	// Optional+Computed keeps it unless the zero is a value none of the
	// attribute's own validators would accept -- unless a schema default is
	// itself that zero, since a default outranks the validator that rejects
	// it (validators run against config, defaults land in the plan, and the
	// two can disagree).
	switch {
	case attribute.IsRequired():
		return KeepZero, true
	case attribute.IsOptional() && !attribute.IsComputed():
		return NullZero, true
	case attribute.IsOptional() && attribute.IsComputed() &&
		zeroIsRejected(built, attribute, elidesTheEmptyString) &&
		!zeroIsTheDefault(attribute):
		return NullZero, true
	}
	return KeepZero, true
}
