package resourcekit

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// elideExempt names the field kinds that deliberately make no elision claim.
// A kind absent from here and lacking an Elide is reported rather than skipped,
// because "no claim" and "nobody wrote one" look identical from the outside.
var elideExempt = map[string]struct{}{
	"BoolField":      {}, // a false is a value; see the type's own comment
	"BoolPtrField":   {}, // a pointer bool already distinguishes unset from false
	"StringPtrField": {}, // likewise: a *string separates unset from empty
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
		// NULLZERO IS THE NARROW CASE, NOT THE BROAD ONE. Only an attribute that
		// is Optional and NOT Computed may treat a zero as an absence: there,
		// a zero from the API against a config that never mentioned the
		// attribute is genuinely "unset", and writing it as a value produces a
		// permanent diff.
		//
		// EVERYTHING ELSE KEEPS ITS ZERO, and Optional+Computed is the case
		// that forced this. The practitioner may set an explicit empty value
		// there -- ap_group's acceptance config says `device_macs = []` and
		// the schema description spells the distinction out: "May be empty --
		// the controller accepts a group with no members. Omit it to leave the
		// membership as the controller has it." Nulling an explicit empty
		// makes state disagree with config, which is the inconsistent-result
		// failure this rule exists to prevent, arriving from the other side.
		want := KeepZero
		if attribute.IsOptional() && !attribute.IsComputed() && !attribute.IsRequired() {
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
