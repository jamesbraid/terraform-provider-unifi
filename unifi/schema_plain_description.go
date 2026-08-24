package unifi

import (
	"reflect"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// plainDescriptions rewrites a generated schema so its descriptions project as
// plain text rather than Markdown: the framework reports description_kind
// markdown whenever MarkdownDescription is set, and the generator always sets it.
//
// Call this before grafting timeouts, whose description is already plain.
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
// This uses reflection rather than a switch over concrete attribute types: a
// switch would silently ignore any type added later.
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

	// Both NestedObject (list/set/map) and direct Attributes/Blocks (single
	// nested) must be walked, or a whole class of nested attribute is skipped.
	//
	// The maps are shared with the original, so rewriting them reaches the
	// schema even without using the copy above.
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
