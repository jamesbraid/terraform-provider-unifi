package resourcekit

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// elideExempt names the field kinds that deliberately make no elision claim.
// A kind absent from here and lacking an Elide is reported rather than skipped,
// because "no claim" and "nobody wrote one" look identical from the outside.
var elideExempt = map[string]struct{}{
	"BoolField":    {}, // a false is a value; see the type's own comment
	"BoolPtrField": {}, // a pointer bool already distinguishes unset from false
	// A pointer string that never emits "" has no third state to elide: nil
	// and a pointer to the empty string both mean absent, and ToSDK produces
	// only the first.
	"StringLikePtrField": {},
}

// ElideProblems reports every descriptor field whose Elide disagrees with the
// generated schema.
//
// WHY THIS EXISTS. Elide is a value nothing verified. Flipping all seven of
// dns_record's left the entire unifi suite green, which means the descriptor
// could be mass-produced across every surface with the value wrong and no test
// would say so. That is the shape this lane keeps finding: a field asserted
// nowhere reads as agreed rather than as unchecked.
//
// THE RULE. Elide answers "is a zero from the API an absence?", and the schema
// already knows: a Required attribute must hold whatever the API returned,
// including an empty string, because nulling it produces a state that does not
// match the config -- an inconsistent-result-after-apply. An Optional one may
// treat a zero as unset. Computed-only follows Required, since the practitioner
// supplied nothing and the value must round-trip as given.
//
// THE TERRAFORM NAME IS RECOVERED BY REFLECTION, NOT FROM THE MAPPING. A
// Field's WireName is the SDK's name -- dns_record's `name` is the controller's
// `key` -- so it cannot index the schema. Calling the field's Model accessor
// against a zero model yields a pointer into that struct, and the struct field
// holding that address carries the tfsdk tag. This keeps the check working for
// surfaces that have no mapping file at all, and unifi_setting is one: 62
// mapping files cover 30 surface names while the provider source derives 29,
// and the two overlap on only 27.
func ElideProblems[M any, S any](spec Spec[M, S], built schema.Schema) []string {
	var model M
	offsets := tfsdkOffsets(&model)

	var problems []string
	for _, field := range spec.Fields {
		// A read-only field forwards ToModel, so its Elide still governs how an
		// API zero reaches the model. Reach through the wrapper rather than
		// exempting it, or every computed field goes unchecked.
		if wrapper, ok := field.(interface{ Unwrap() Field[M, S] }); ok {
			field = wrapper.Unwrap()
		}
		value := reflect.ValueOf(field)
		elide := value.FieldByName("Elide")
		if !elide.IsValid() {
			// A field kind with no Elide makes no claim, so there is nothing to
			// check -- but the exemption has to name what it means. BoolField
			// is deliberate and documented: a false is a value, and nulling it
			// would turn "the controller did not say" into "the practitioner
			// said false". Anything else reaching here is an omission wearing
			// the same shape, which is how the collection types went unchecked
			// until a surface needed one.
			kind := value.Type().Name()
			if i := strings.IndexByte(kind, '['); i > 0 {
				kind = kind[:i] // strip the generic instantiation
			}
			if _, deliberate := elideExempt[kind]; !deliberate {
				problems = append(problems, fmt.Sprintf(
					"%s: field %q is a %s, which carries no Elide; either it should, or "+
						"add it to elideExempt with the reason",
					spec.TypeName, field.WireName(), kind))
			}
			continue
		}
		name, ok := terraformName(&model, value, offsets)
		if !ok {
			problems = append(problems, fmt.Sprintf(
				"%s: field %q has no model accessor the check could follow, so its Elide is unverifiable",
				spec.TypeName, field.WireName()))
			continue
		}
		attribute, ok := built.Attributes[name]
		if !ok {
			problems = append(problems, fmt.Sprintf(
				"%s: field %q maps to attribute %q, which the schema does not declare",
				spec.TypeName, field.WireName(), name))
			continue
		}
		// A READ DEFAULT REPLACES THE ELISION RATHER THAN JOINING IT, so the
		// rule below would be checking a value that cannot be reached. What
		// has to hold instead is the pair of flags that make the substitution
		// legal at all.
		//
		// COMPUTED, because the value is one the provider invents: the config
		// never mentioned the attribute, so a state carrying "default" against
		// a null config is an inconsistent result after apply unless Terraform
		// has been told the provider may supply it.
		//
		// OPTIONAL, because a Required attribute is always in the config, so
		// the empty read the default exists to catch cannot happen -- a
		// default there is dead code claiming to be a behaviour.
		if def := value.FieldByName("ReadDefault"); def.IsValid() && def.Kind() == reflect.String && def.String() != "" {
			if !attribute.IsOptional() || !attribute.IsComputed() {
				problems = append(problems, fmt.Sprintf(
					"%s.%s substitutes %q on an empty read but the schema declares it %s; "+
						"a provider-supplied value needs Optional+Computed",
					spec.TypeName, name, def.String(), requiredness(attribute)))
			}
			continue
		}
		// THE RULE, IN FOUR CASES.
		//
		// Required keeps its zero. The config always supplies the attribute, so
		// state must hold whatever came back; nulling it makes state disagree
		// with config, which is an inconsistent-result-after-apply.
		//
		// Optional and not Computed nulls its zero. A zero from the API against
		// a config that never mentioned the attribute is genuinely unset, and
		// writing it as a value produces a permanent diff.
		//
		// Optional AND Computed splits on whether the zero is a legal value,
		// and getting that split right took three attempts. Treating them all
		// as KeepZero was written against ap_group's device_macs, where
		// `device_macs = []` is a real configuration meaning a group with no
		// members -- but the same answer for firewall_rule's setting_preference
		// puts "" in state for an attribute whose own schema says
		// OneOf("auto","manual"), a value the provider would reject if a
		// practitioner wrote it. Splitting on whether the attribute declares a
		// schema Default is refuted by the example the rule came from: neither
		// device_macs nor setting_preference has one. What separates them is
		// whether an empty is something the practitioner could have written,
		// and the schema already knows because its validators are what would
		// reject it.
		//
		// Computed-only follows Required: the practitioner supplied nothing and
		// the value must round-trip as given.
		//
		// THE SPLIT DOES NOT REACH REQUIRED, deliberately. Three Required
		// attributes across the shipped descriptors carry a OneOf excluding the
		// empty string -- dns_record's record_type, firewall_group's type and
		// static_route's type -- and applying the split to them would demand
		// NullZero on all three.
		want := KeepZero
		switch {
		case attribute.IsRequired():
			want = KeepZero
		case attribute.IsOptional() && !attribute.IsComputed():
			want = NullZero
		case attribute.IsOptional() && attribute.IsComputed() && zeroIsRejected(attribute):
			want = NullZero
		}
		if got := ElideZero(elide.Bool()); got != want {
			problems = append(problems, fmt.Sprintf(
				"%s.%s is %s but the schema declares it %s, which wants %s",
				spec.TypeName, name, elideName(got), requiredness(attribute), elideName(want)))
		}
	}
	sort.Strings(problems)
	return problems
}

// tfsdkOffsets maps each model field's address to its tfsdk tag.
func tfsdkOffsets(model any) map[uintptr]string {
	out := map[uintptr]string{}
	value := reflect.ValueOf(model).Elem()
	typ := value.Type()
	for i := range typ.NumField() {
		tag := typ.Field(i).Tag.Get("tfsdk")
		if tag == "" {
			continue
		}
		out[value.Field(i).Addr().Pointer()] = tag
	}
	return out
}

// terraformName calls the field's Model accessor and identifies which model
// field it returned a pointer to.
func terraformName(model any, field reflect.Value, offsets map[uintptr]string) (string, bool) {
	accessor := field.FieldByName("Model")
	if !accessor.IsValid() || accessor.Kind() != reflect.Func {
		return "", false
	}
	results := accessor.Call([]reflect.Value{reflect.ValueOf(model)})
	if len(results) != 1 || results[0].Kind() != reflect.Ptr {
		return "", false
	}
	name, ok := offsets[results[0].Pointer()]
	return name, ok
}

func elideName(e ElideZero) string {
	if e == KeepZero {
		return "KeepZero"
	}
	return "NullZero"
}

// requiredness names the exact flag combination, because "Optional" alone read
// as the whole story once already: Optional+Computed is the majority case in
// this provider and the one the rule originally got wrong, so a message that
// collapses it into "Optional" hides the very distinction being asserted.
func requiredness(a schema.Attribute) string {
	switch {
	case a.IsRequired():
		return "Required"
	case a.IsOptional() && a.IsComputed():
		return "Optional+Computed"
	case a.IsComputed():
		return "Computed-only"
	case a.IsOptional():
		return "Optional-only"
	}
	return "neither required nor optional"
}

// zeroIsRejected asks the attribute's own validators whether an empty string is
// a legal value for it.
//
// IT RUNS THEM RATHER THAN INSPECTING THEM. stringvalidator.OneOf returns an
// unexported type holding an unexported slice, so reading its permitted set
// means reflection into another module's internals or parsing the English of
// its Description. Both break when that module changes something it never
// promised to keep. Calling ValidateString with "" uses the only part of a
// validator that IS promised, and it answers the question directly instead of
// by proxy -- which also means it catches LengthAtLeast and a regex that
// excludes the empty string, not only OneOf.
func zeroIsRejected(attribute schema.Attribute) bool {
	stringAttribute, ok := attribute.(schema.StringAttribute)
	if !ok {
		return false
	}
	// A CUSTOM TYPE IS NOT PROBED, because "" is not its zero value.
	//
	// port_profile's dot1x_idle_timeout is a timetypes.GoDuration whose
	// validators are GoDurationBetween(0, 65535s) and GoDurationMultipleOf.
	// Feeding them "" gets a rejection -- but for being unparseable as a
	// duration, not for being an illegal value: "0s" is inside the range and
	// is exactly what the hand-written mapper produces for a pointer to zero.
	// So the probe answered a different question from the one asked, and the
	// answer it gave would have nulled a legitimate zero duration.
	//
	// Skipping leaves these on KeepZero, which is the answer the rule gave
	// before the refinement and the one that matches the code being replaced.
	// Probing a custom type properly would mean asking it for its own zero,
	// which basetypes.StringTypable does not offer.
	if stringAttribute.CustomType != nil {
		return false
	}
	ctx := context.Background()
	for _, v := range stringAttribute.Validators {
		response := &validator.StringResponse{}
		v.ValidateString(ctx, validator.StringRequest{
			Path:        path.Root("probe"),
			ConfigValue: types.StringValue(""),
		}, response)
		if response.Diagnostics.HasError() {
			return true
		}
	}
	return false
}
