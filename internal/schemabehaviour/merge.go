package schemabehaviour

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// MergeIntoPolicy writes a surface's derived behaviour into a policy in place
// and returns a report of what it did.
//
// Matching is by the terraform_name the policy already declares, never by a
// name this package works out for itself. A migration renames fields -- the
// rename is a judgement made against the resource's own conversion code, and
// two of wlan's nine are unrecoverable by inspection -- so the policy is the
// authority on which attribute is which and this only fills in what each one
// does.
//
// That choice makes the merge a rename check as well. A policy attribute the
// schema does not have is reported: either the rename is wrong, or the policy
// invented an attribute the released provider never served. Both are worth
// stopping for, and a merge that silently matched nothing would report neither.
//
// Behaviour already present in the policy is left alone and reported, so
// running this over a partly hand-written policy adds what is missing instead
// of overwriting a considered decision.
func MergeIntoPolicy(path string, surface Surface) (string, error) {
	if surface.Delegated {
		return "", fmt.Errorf(
			"%s serves a generated schema (%s), so there is no hand-written schema to derive from",
			surface.TypeName, surface.DelegatedTo)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}

	// Decoded into a generic document rather than the compiler's types: this
	// writes back every member it did not touch, and a struct round trip would
	// quietly drop anything the struct does not model.
	var document map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}

	byPath := map[string][]Behaviour{}
	for _, behaviour := range surface.Behaviours {
		byPath[behaviour.Path] = append(byPath[behaviour.Path], behaviour)
	}

	declared := make(map[string]bool, len(surface.Attributes))
	for _, attribute := range surface.Attributes {
		declared[attribute] = true
	}

	merge := &merger{
		available:     byPath,
		declared:      declared,
		applied:       map[string]bool{},
		providerOwned: map[string]bool{},
	}
	merge.walkFields(document, "fields", "")
	merge.walkFields(document, "groupings", "")
	merge.walkFields(document, "flattenings", "")
	merge.walkProviderOwned(document)

	for path := range byPath {
		if merge.applied[path] {
			continue
		}
		if root, _, nested := strings.Cut(path, "."); nested && merge.providerOwned[root] {
			merge.providerOwnedNested = append(merge.providerOwnedNested, path)
			continue
		}
		merge.unplaced = append(merge.unplaced, path)
	}

	if len(merge.written) > 0 {
		encoded, err := json.MarshalIndent(document, "", "  ")
		if err != nil {
			return "", fmt.Errorf("encode: %w", err)
		}
		encoded = append(encoded, '\n')
		if err := os.WriteFile(path, encoded, 0o644); err != nil {
			return "", fmt.Errorf("write: %w", err)
		}
	}

	return merge.report(surface, path), nil
}

type merger struct {
	available map[string][]Behaviour
	declared  map[string]bool
	applied   map[string]bool
	// providerOwned records which top-level names the policy owns rather than
	// derives, so behaviour under one can be told apart from behaviour with no
	// home at all. The two need different answers and read identically without
	// this.
	providerOwned       map[string]bool
	written             []string
	kept                []string
	unmatched           []string
	unplaced            []string
	providerOwnedNested []string
}

// walkProviderOwned fills the provider-owned half of a policy.
//
// These attributes have no structural field to derive from -- site is a request
// parameter, timeouts is grafted, and bgp's asn, router_id and peers are
// configuration the controller never stores -- but they are still attributes of
// the released schema, and they still carry validators, defaults and plan
// modifiers. Reading only the derived half left every one of those to be typed
// out by hand, which is the thing deriving the policy exists to stop.
//
// It reports them as unplaced too, under a message saying the policy omitted
// them, which was false: the policy had them in the other half. Following that
// message meant moving a provider-owned attribute into fields, where it has no
// structural_name to give.
func (m *merger) walkProviderOwned(node map[string]any) {
	entries, ok := node["provider_owned"].([]any)
	if !ok {
		return
	}
	for _, entry := range entries {
		seam, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		name, _ := seam["terraform_name"].(string)
		if name == "" {
			continue
		}
		m.providerOwned[name] = true
		m.apply(seam, name)
	}
}

// walkFields descends the policy's field lists. Groupings, flattenings and
// nested members all carry attributes, and a merge that read only the top-level
// list would silently do nothing for a surface whose behaviour is mostly nested
// -- which is most of them.
func (m *merger) walkFields(node map[string]any, key, prefix string) {
	raw, ok := node[key]
	if !ok {
		return
	}
	entries, ok := raw.([]any)
	if !ok {
		return
	}
	for _, entry := range entries {
		field, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		name, _ := field["terraform_name"].(string)
		path := name
		if prefix != "" && name != "" {
			path = prefix + "." + name
		}
		if name != "" {
			m.apply(field, path)
		}
		for _, nested := range []string{"members", "fields"} {
			m.walkFields(field, nested, path)
		}
	}
}

// apply fills one policy field's attribute with the behaviour derived for it.
func (m *merger) apply(field map[string]any, path string) {
	behaviours, ok := m.available[path]
	if !ok {
		// A managed attribute the schema has no behaviour for is ordinary --
		// most attributes carry none. An attribute the SCHEMA does not have at
		// all is not, and is the shape a wrong rename takes. The schema's own
		// attribute list is what separates them; without it every plain
		// attribute read as a suspect rename.
		if disposition, _ := field["disposition"].(string); disposition == "managed" &&
			!m.declared[path] {
			m.unmatched = append(m.unmatched, path)
		}
		return
	}
	m.applied[path] = true

	attribute, ok := field["attribute"].(map[string]any)
	if !ok {
		// Nothing to hang behaviour on. Reported rather than invented: a field
		// with behaviour and no attribute is a policy that has not decided
		// what the attribute is yet.
		m.unmatched = append(m.unmatched, path+" (no attribute to carry it)")
		return
	}

	for _, group := range []struct {
		kind, member string
	}{
		{KindValidator, "validators"},
		{KindPlanModifier, "plan_modifiers"},
	} {
		var custom []any
		for _, behaviour := range behaviours {
			if behaviour.Kind != group.kind {
				continue
			}
			custom = append(custom, customEntry(behaviour))
		}
		if len(custom) == 0 {
			continue
		}
		if _, exists := attribute[group.member]; exists {
			m.kept = append(m.kept, fmt.Sprintf("%s %s", path, group.member))
			continue
		}
		attribute[group.member] = custom
		m.written = append(m.written, fmt.Sprintf("%s %s (%d)", path, group.member, len(custom)))
	}

	for _, behaviour := range behaviours {
		switch behaviour.Kind {
		case KindDefault:
			if _, exists := attribute["default"]; exists {
				m.kept = append(m.kept, path+" default")
				continue
			}
			value, ok := staticValue(behaviour)
			if !ok {
				// A default that is not a literal cannot be written as one.
				// Left for a human WITH the expression, rather than guessed.
				m.unmatched = append(m.unmatched, fmt.Sprintf(
					"%s default is %s, which is not a static value; write it as a custom default",
					path, behaviour.Expression))
				continue
			}
			attribute["default"] = map[string]any{"static": value}
			m.written = append(m.written, path+" default")
		case KindCustomType:
			if _, exists := attribute["custom_type"]; exists {
				m.kept = append(m.kept, path+" custom_type")
				continue
			}
			if behaviour.ValueType == "" || len(behaviour.Imports) != 1 {
				m.unmatched = append(m.unmatched, fmt.Sprintf(
					"%s custom_type is %s, whose value type does not follow from its name; "+
						"write it by hand", path, behaviour.Expression))
				continue
			}
			attribute["custom_type"] = map[string]any{
				"import":     map[string]any{"path": behaviour.Imports[0]},
				"type":       behaviour.Expression,
				"value_type": behaviour.ValueType,
			}
			m.written = append(m.written, path+" custom_type")
		}
	}
}

func customEntry(behaviour Behaviour) any {
	imports := make([]any, 0, len(behaviour.Imports))
	for _, path := range behaviour.Imports {
		imports = append(imports, map[string]any{"path": path})
	}
	custom := map[string]any{"schema_definition": behaviour.Expression}
	if len(imports) > 0 {
		custom["imports"] = imports
	}
	return map[string]any{"custom": custom}
}

// staticValue converts a derived static default into the JSON value the
// specification carries. An int64 is emitted as json.Number so that a whole
// number does not come back as 3600.0 after a round trip.
func staticValue(behaviour Behaviour) (any, bool) {
	static := behaviour.Static
	switch {
	case static == nil:
		return nil, false
	case static.Bool != nil:
		return *static.Bool, true
	case static.String != nil:
		return *static.String, true
	case static.Int64 != nil:
		return json.Number(fmt.Sprintf("%d", *static.Int64)), true
	case static.Float64 != nil:
		return json.Number(fmt.Sprintf("%v", *static.Float64)), true
	}
	return nil, false
}

func (m *merger) report(surface Surface, path string) string {
	sort.Strings(m.written)
	sort.Strings(m.kept)
	sort.Strings(m.unmatched)
	sort.Strings(m.unplaced)
	sort.Strings(m.providerOwnedNested)

	var out strings.Builder
	fmt.Fprintf(&out, "%s: %d behaviour(s) derived from %s\n",
		surface.TypeName, len(surface.Behaviours), surface.File)
	section(&out, "written into "+path, m.written)
	section(&out, "already present, left alone", m.kept)
	section(&out, "POLICY ATTRIBUTES THE SCHEMA DOES NOT HAVE — check the rename", m.unmatched)
	section(&out, "SCHEMA BEHAVIOUR WITH NOWHERE TO GO — the policy omits these attributes", m.unplaced)
	// A provider-owned attribute's members are written in specification form --
	// {"name": x, "<type>": {...}} -- not in the policy's own field form, so the
	// walk above cannot reach them. Saying which attribute owns them and why
	// they are out of reach is the whole answer; a second walker over a second
	// shape, for the one attribute in the estate that has this, would be more
	// mechanism than the problem has.
	section(&out,
		"NESTED INSIDE A PROVIDER-OWNED ATTRIBUTE — transcribe these by hand, "+
			"their members are written in specification form and are not walked",
		m.providerOwnedNested)
	if len(surface.Opaque) > 0 {
		var lines []string
		for _, unread := range surface.Opaque {
			lines = append(lines, unread.Path+": "+unread.Reason)
		}
		section(&out, "NOT READABLE FROM SOURCE — transcribe these by hand", lines)
	}
	return out.String()
}

func section(out *strings.Builder, title string, lines []string) {
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(out, "\n  %s (%d):\n", title, len(lines))
	for _, line := range lines {
		fmt.Fprintf(out, "    %s\n", line)
	}
}
