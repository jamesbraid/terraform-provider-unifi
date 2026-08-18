package resourcekit

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

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
		value := reflect.ValueOf(field)
		elide := value.FieldByName("Elide")
		if !elide.IsValid() {
			continue // BoolField and friends elide nothing; there is no claim to check.
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
		want := NullZero
		if attribute.IsRequired() || (attribute.IsComputed() && !attribute.IsOptional()) {
			want = KeepZero
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

func requiredness(a schema.Attribute) string {
	switch {
	case a.IsRequired():
		return "Required"
	case a.IsComputed() && !a.IsOptional():
		return "Computed-only"
	case a.IsOptional():
		return "Optional"
	}
	return "neither required nor optional"
}
