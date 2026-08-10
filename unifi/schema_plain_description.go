package unifi

import (
	"reflect"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// plainDescriptions rewrites a generated schema so its descriptions project as
// plain text rather than Markdown.
//
// A provider code specification carries one description per attribute, and the
// generator writes it to both Description and MarkdownDescription. The
// framework reports description_kind markdown whenever MarkdownDescription is
// set, so a generated schema always projects as markdown. The specification has
// no way to ask for anything else -- the same gap that makes write_only
// ungeneratable, and the same remedy: graft the fact the specification cannot
// carry.
//
// Four released surfaces need it. ap_group, device, firewall_group and
// port_profile were written with Description alone and project as plain; the
// other twenty-four already project as markdown on every attribute a generator
// emits, which is why the first six migrations never met this. Preserving it is
// not cosmetic: catalog-build-schema.sh compares the candidate projection
// against the released provider's byte for byte, so a surface that flips
// description_kind fails the build rather than merely reading differently.
//
// Call this before grafting timeouts, whose own description is already plain
// and is not the generator's to restate.
func plainDescriptions(s *schema.Schema) {
	if s.Description == "" {
		s.Description = s.MarkdownDescription
	}
	s.MarkdownDescription = ""
	plainAttributeDescriptions(s.Attributes)
	plainBlockDescriptions(s.Blocks)
}

func plainAttributeDescriptions(attributes map[string]schema.Attribute) {
	for name, attribute := range attributes {
		if plain, ok := plainDescriptionOf(attribute).(schema.Attribute); ok {
			attributes[name] = plain
		}
	}
}

func plainBlockDescriptions(blocks map[string]schema.Block) {
	for name, block := range blocks {
		if plain, ok := plainDescriptionOf(block).(schema.Block); ok {
			blocks[name] = plain
		}
	}
}

// plainDescriptionOf returns a copy of one attribute or block that carries its
// description as plain text, recursing into a nested object's own members.
//
// It works by reflection rather than a switch over the concrete attribute
// types, for the reason the behaviour inventory does: a switch silently ignores
// a type added later, and silently ignoring one here would reintroduce exactly
// the drift this exists to prevent.
func plainDescriptionOf(value any) any {
	source := reflect.ValueOf(value)
	if source.Kind() != reflect.Struct {
		return value
	}

	pointer := reflect.New(source.Type())
	pointer.Elem().Set(source)
	copied := pointer.Elem()

	description := copied.FieldByName("Description")
	markdown := copied.FieldByName("MarkdownDescription")
	if description.IsValid() && markdown.IsValid() &&
		description.Kind() == reflect.String && markdown.Kind() == reflect.String &&
		description.CanSet() && markdown.CanSet() {
		if description.String() == "" {
			description.SetString(markdown.String())
		}
		markdown.SetString("")
	}

	// Members hang off one of two shapes. The list, set and map nested kinds
	// carry a NestedObject; the single nested kinds carry Attributes and Blocks
	// directly. Both are walked, because covering only NestedObject silently
	// skips every child of a SingleNestedAttribute.
	//
	// The maps are shared with the original, so rewriting their entries reaches
	// the schema whether or not the copy above is used.
	plainMembersOf(copied)
	if nested := copied.FieldByName("NestedObject"); nested.IsValid() &&
		nested.Kind() == reflect.Struct {
		plainMembersOf(nested)
	}

	return copied.Interface()
}

// plainMembersOf rewrites the attributes and blocks hanging directly off value,
// if it carries either.
func plainMembersOf(value reflect.Value) {
	if attributes, ok := fieldAs[map[string]schema.Attribute](value, "Attributes"); ok {
		plainAttributeDescriptions(attributes)
	}
	if blocks, ok := fieldAs[map[string]schema.Block](value, "Blocks"); ok {
		plainBlockDescriptions(blocks)
	}
}

func fieldAs[T any](value reflect.Value, name string) (T, bool) {
	var zero T
	field := value.FieldByName(name)
	if !field.IsValid() || !field.CanInterface() {
		return zero, false
	}
	typed, ok := field.Interface().(T)
	return typed, ok
}
