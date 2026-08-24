package resourcekit

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/attr/xattr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// ZeroReadProblems runs every field's ToModel against a zero SDK object and
// reports what a real read could not survive. A zero SDK object is the one
// input every surface must tolerate: the controller omits what is unset, the
// SDK decodes that as a Go zero, and ToModel turns it into a state value --
// so a field that can't take this trip breaks the read of any object that
// leaves it blank.
//
// This check catches two shapes of failure ElideProblems can't: a panic (a
// field with a nil closure, as device's mac once had) and a value the
// field's own type refuses (client's fixed_ip read back as ""). ElideProblems
// judges the declaration; this judges the behavior, which also covers a new
// field kind's own ToModel arithmetic before anyone teaches the rule about it.
//
// A Required attribute is exempt from the value judgment (a real read always
// carries it) but not the panic judgment, which needs no controller to fire.
func ZeroReadProblems[M any, S any](spec Spec[M, S], built schema.Schema) []string {
	ctx := context.Background()
	var problems []string
	var model M
	sdk := new(S)
	for _, field := range spec.Fields {
		problems = append(problems, zeroReadFieldProblems(ctx, spec.TypeName, field, sdk, &model)...)
	}

	offsets := tfsdkOffsets(&model)
	value := reflect.ValueOf(&model).Elem()
	for i := range value.NumField() {
		modelField := value.Field(i)
		name, ok := offsets[modelField.Addr().Pointer()]
		if !ok {
			continue
		}
		validatable, ok := modelField.Interface().(xattr.ValidateableAttribute)
		if !ok {
			continue
		}
		attribute, ok := built.Attributes[name]
		if !ok || attribute.IsRequired() {
			continue
		}
		response := &xattr.ValidateAttributeResponse{}
		validatable.ValidateAttribute(ctx, xattr.ValidateAttributeRequest{
			Path: path.Root(name),
		}, response)
		for _, diagnostic := range response.Diagnostics.Errors() {
			problems = append(problems, fmt.Sprintf(
				"%s.%s: a zero read produces a value its own type refuses (%s); an object "+
					"that leaves this blank cannot be read back",
				spec.TypeName, name, diagnostic.Summary()))
		}
	}
	sort.Strings(problems)
	return problems
}

func zeroReadFieldProblems[M any, S any](
	ctx context.Context,
	typeName string,
	field Field[M, S],
	sdk *S,
	model *M,
) (problems []string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			problems = append(problems, fmt.Sprintf(
				"%s.%s: reading a zero object panics (%v); no read or list of this surface "+
					"can survive it",
				typeName, field.WireName(), recovered))
		}
	}()
	field.ToModel(ctx, sdk, model)
	return problems
}

// ZeroReadProblems on the resource binds the spec to its own served schema, so
// a caller holding only a resource.Resource can ask the question through one
// non-generic interface -- which is how the provider-wide test reaches every
// kit surface without naming any.
func (r *Resource[M, S]) ZeroReadProblems(ctx context.Context) []string {
	return ZeroReadProblems(r.Spec, r.SchemaSpec.Resource(ctx))
}
