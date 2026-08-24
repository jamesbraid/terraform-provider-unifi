package resourcekit

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr/xattr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/defaults"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
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
// generated schema: a Required attribute must keep whatever the API returned
// (nulling it would disagree with config), an Optional one may treat a zero
// as unset, and Computed-only follows Required.
//
// The Terraform name is recovered by reflection over the model, not from the
// mapping: WireName is the SDK's name, which can't index the schema, and this
// keeps the check working even for surfaces with no mapping file (unifi_setting
// is one).
func ElideProblems[M any, S any](spec Spec[M, S], built schema.Schema) []string {
	var model M
	offsets := tfsdkOffsets(&model)

	var problems []string
	for _, field := range spec.Fields {
		// A read-only field forwards ToModel, so its Elide still governs how
		// an API zero reaches the model; reach through the wrapper rather
		// than exempting it, or every computed field goes unchecked.
		if wrapper, ok := field.(interface{ Unwrap() Field[M, S] }); ok {
			field = wrapper.Unwrap()
		}
		value := reflect.ValueOf(field)
		kind := value.Type().Name()
		if i := strings.IndexByte(kind, '['); i > 0 {
			kind = kind[:i] // strip the generic instantiation
		}
		elide := value.FieldByName("Elide")
		if !elide.IsValid() {
			// A field kind with no Elide makes no claim, so there is nothing
			// to check -- but the exemption has to name what it means, or an
			// omission looks identical to a deliberate exemption.
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
				spec.TypeName,
				field.WireName(),
			))
			continue
		}
		attribute, ok := built.Attributes[name]
		if !ok {
			// A block is implicitly optional and can never be Computed, so the
			// rule that applies is the Optional-and-not-Computed one: NullZero,
			// since an absent block is an absence, not a configured empty.
			if _, isBlock := built.Blocks[name]; isBlock {
				if elide.Kind() == reflect.Bool && elide.Bool() != bool(NullZero) {
					problems = append(problems, fmt.Sprintf(
						"%s.%s is KeepZero but it is a block, which is optional and never "+
							"computed, so an absent one is an absence and wants NullZero",
						spec.TypeName, name))
				}
				continue
			}
			problems = append(problems, fmt.Sprintf(
				"%s: field %q maps to attribute %q, which the schema does not declare, "+
					"as either an attribute or a block",
				spec.TypeName, field.WireName(), name))
			continue
		}
		// A ReadDefault replaces the elision rather than joining it, so what
		// has to hold is the pair of flags that make the substitution legal:
		// Computed (the value is provider-invented) and Optional (a Required
		// attribute is always in config, so the empty case can't happen).
		if def := value.FieldByName(
			"ReadDefault",
		); def.IsValid() && def.Kind() == reflect.String &&
			def.String() != "" {
			if !attribute.IsOptional() || !attribute.IsComputed() {
				problems = append(problems, fmt.Sprintf(
					"%s.%s substitutes %q on an empty read but the schema declares it %s; "+
						"a provider-supplied value needs Optional+Computed",
					spec.TypeName, name, def.String(), requiredness(attribute)))
			}
			continue
		}
		// Required and Computed-only keep the zero (config always supplies
		// it, or it must round-trip as given). Optional-not-Computed nulls
		// it. Optional+Computed keeps it unless the zero is a value none of
		// the attribute's own validators would accept -- unless a schema
		// default is itself that zero, since a default outranks the
		// validator that rejects it (validators run against config, defaults
		// land in the plan, and the two can disagree).
		//
		// elidesTheEmptyString is field-kind-specific: string kinds elide the
		// literal "", but DurationPtrField elides the number 0 (a value
		// GoDuration holds happily as "0s"), so asking its type about ""
		// would ask the wrong question.
		elidesTheEmptyString := kind == "StringField" || kind == "StringLikeField"
		want := KeepZero
		switch {
		case attribute.IsRequired():
			want = KeepZero
		case attribute.IsOptional() && !attribute.IsComputed():
			want = NullZero
		case attribute.IsOptional() && attribute.IsComputed() &&
			zeroIsRejected(attribute, elidesTheEmptyString) &&
			!zeroIsTheDefault(attribute):
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

// requiredness names the exact flag combination: collapsing Optional+Computed
// into "Optional" would hide the distinction the rule keys on.
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

// zeroIsRejected runs the attribute's validators against "" rather than
// inspecting them: OneOf's permitted set is an unexported field of another
// module, but ValidateString is the one part of a validator promised to keep
// working, and running it also catches LengthAtLeast and a regex, not just
// OneOf.
func zeroIsRejected(attribute schema.Attribute, elidedZeroIsTheEmptyString bool) bool {
	stringAttribute, ok := attribute.(schema.StringAttribute)
	if !ok {
		return false
	}
	// A custom type is asked about its own "" (ValidateAttribute), not the
	// attribute's validators: elide only ever elides the literal string "",
	// never a semantic zero, so the type's own opinion of "" is the right
	// question, not a probe of numeric-looking validators.
	if stringAttribute.CustomType != nil {
		return elidedZeroIsTheEmptyString && customTypeRejectsEmpty(stringAttribute.CustomType)
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

// zeroIsTheDefault reports whether the attribute's schema default IS the zero
// value. It asks the default for its value rather than inspecting its type,
// for the same reason zeroIsRejected runs validators: the default's own type
// is another module's unexported struct.
func zeroIsTheDefault(attribute schema.Attribute) bool {
	stringAttribute, ok := attribute.(schema.StringAttribute)
	if !ok || stringAttribute.Default == nil {
		return false
	}
	response := &defaults.StringResponse{}
	stringAttribute.Default.DefaultString(
		context.Background(), defaults.StringRequest{Path: path.Root("probe")}, response)
	return !response.PlanValue.IsNull() && !response.PlanValue.IsUnknown() &&
		response.PlanValue.ValueString() == ""
}

// customTypeRejectsEmpty asks a custom string type whether it accepts "" as a
// value, by constructing it and running the type's own validation --
// authoritative in a way the attribute's config validators aren't, since
// ToModel writes the constructed value into state and a type that refuses it
// fails every read carrying it.
func customTypeRejectsEmpty(customType basetypes.StringTypable) bool {
	ctx := context.Background()
	value, diags := customType.ValueFromString(ctx, basetypes.NewStringValue(""))
	if diags.HasError() {
		return false
	}
	validatable, ok := value.(xattr.ValidateableAttribute)
	if !ok {
		return false
	}
	response := &xattr.ValidateAttributeResponse{}
	validatable.ValidateAttribute(ctx, xattr.ValidateAttributeRequest{
		Path: path.Root("probe"),
	}, response)
	return response.Diagnostics.HasError()
}
