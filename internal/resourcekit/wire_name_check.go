package resourcekit

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// WireNameProblems reports every descriptor field whose Wire disagrees with the
// SDK struct's own json tag.
//
// WHY THIS EXISTS. Wire is the name the field-masked update puts on the wire,
// and it is hand-transcribed from the SDK struct -- where it differs from the
// Terraform name often enough that transcribing is not optional: dns_record's
// `name` is `key`, firewall_rule's `icmp_v6_typename` is `icmpv6_typename`, and
// its `src_mac` is `src_mac_address`.
//
// Nothing checked it. Two mutations against a fully cut-over surface -- one
// renaming src_mac_address to src_mac, one reverting icmpv6_typename to the
// Terraform spelling -- left the whole provider suite green. A wrong name is
// not a compile error and not a test failure; it is a field mask naming an
// attribute the controller does not have, so the write is accepted and the
// value never changes. That is the same silent write-drop the masked update was
// introduced to prevent, arriving through the mask itself.
//
// THE ANSWER IS IN THE SDK STRUCT, so this reads it there rather than from a
// mapping file: calling a field's SDK accessor against a zero struct yields a
// pointer into it, and the struct field at that address carries the json tag.
// Same mechanism ElideProblems uses on the model side, and it works for
// surfaces with no mapping file at all.
func WireNameProblems[M any, S any](spec Spec[M, S]) []string {
	var sdk S
	tags := jsonOffsets(&sdk)

	var problems []string
	for _, field := range spec.Fields {
		if wrapper, ok := field.(interface{ Unwrap() Field[M, S] }); ok {
			field = wrapper.Unwrap()
		}
		value := reflect.ValueOf(field)
		accessor := value.FieldByName("SDK")
		if !accessor.IsValid() || accessor.Kind() != reflect.Func {
			problems = append(problems, fmt.Sprintf(
				"%s: field %q has no SDK accessor the check could follow, so its wire name is unverifiable",
				spec.TypeName, field.WireName()))
			continue
		}
		results := accessor.Call([]reflect.Value{reflect.ValueOf(&sdk)})
		if len(results) != 1 || results[0].Kind() != reflect.Ptr {
			problems = append(problems, fmt.Sprintf(
				"%s: field %q has an SDK accessor returning %s rather than a pointer",
				spec.TypeName, field.WireName(), results[0].Kind()))
			continue
		}
		tag, ok := tags[results[0].Pointer()]
		if !ok {
			problems = append(problems, fmt.Sprintf(
				"%s: field %q reaches a struct field carrying no json tag, so nothing names it on the wire",
				spec.TypeName, field.WireName()))
			continue
		}
		if tag != field.WireName() {
			problems = append(problems, fmt.Sprintf(
				"%s: field %q is %q on the wire; a mask naming the wrong attribute is accepted "+
					"and changes nothing",
				spec.TypeName, field.WireName(), tag))
		}
	}
	// The names a hook contributes are checked the same way. A typo here is
	// invisible: the mask simply lacks the field and the write does nothing.
	known := map[string]struct{}{}
	for _, tag := range tags {
		known[tag] = struct{}{}
	}
	for _, name := range spec.AlwaysWire {
		if _, ok := known[name]; !ok {
			problems = append(problems, fmt.Sprintf(
				"%s: AlwaysWire names %q, which is not a json field of the SDK struct; "+
					"the mask would silently omit it",
				spec.TypeName, name))
		}
	}

	sort.Strings(problems)
	return problems
}

// jsonOffsets maps each SDK struct field's address to its json name.
//
// Embedded and unexported fields are skipped rather than reported: a descriptor
// cannot reach them, so they cannot be a field's target.
func jsonOffsets(sdk any) map[uintptr]string {
	out := map[uintptr]string{}
	value := reflect.ValueOf(sdk).Elem()
	typ := value.Type()
	for i := range typ.NumField() {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		if comma := strings.IndexByte(tag, ','); comma >= 0 {
			tag = tag[:comma]
		}
		if tag == "" {
			continue
		}
		out[value.Field(i).Addr().Pointer()] = tag
	}
	return out
}
