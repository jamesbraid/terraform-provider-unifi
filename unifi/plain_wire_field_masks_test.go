package unifi

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
)

// The PLAIN-STRUCT family, as opposed to the Network surfaces in
// wire_field_masks_test.go. These types have no per-purpose encoder: what the
// wire carries is the struct's own json tags, so the SDK side is read by
// REFLECTION rather than by parsing the module cache. A reflected type cannot
// drift from the type actually compiled against, which a regex over a
// vendored source file can.
//
// All three fetched the object before writing and then rebuilt it from the
// Terraform model, so the fetch protected nothing. That is the trap worth
// naming: a GET before a PUT reads like read-modify-write, and it is only
// read-modify-write if the FETCHED object is what goes back. Laundering it
// through the model reduces it to the schema, because a model holds nothing
// for a field the provider does not declare.
type plainMaskedSurface struct {
	name string
	file string
	// object is a zero value of the SDK type, used only for its type.
	object any
	// literal is the composite literal that builds it, e.g. "APGroup".
	literal string
	// objectVar is the variable it is bound to, for later field assignments.
	objectVar string
	declared  func() []string
	// maskedCall and wholeObjectCall are the two spellings of the write.
	maskedCall      string
	wholeObjectCall string
	// exposed names the fields this resource does NOT manage and that the
	// struct FORCE-EMITS: no omitempty, so a whole-object write sent the Go
	// zero value and reset them. These are the defect.
	exposed []string
	// managed is a positive control: a field the resource does manage, which
	// must survive in the mask or setting it would silently do nothing.
	managed string
}

// radius_profile HAS ALSO LEFT THIS TABLE, served from the kit since its mask
// became derivable from Spec.Fields. Its tls_enabled property is asserted the
// same way ap_group's for_wlanconf is: against the derivation, in
// Test_radiusProfileKit_neverWritesTLSEnabled.
// ap_group WAS HERE and is now served by the resource kit, so its mask is
// derived from Spec.Fields rather than hand-written. The property this table
// asserted for it -- that for_wlanconf is never on the wire -- did not move to
// a comment; it is asserted in Test_apGroupKit_neverWritesForWLANConf against
// the derivation itself.
func plainMaskedSurfaces() []plainMaskedSurface {
	return []plainMaskedSurface{
		// EMPTY, and that is the milestone rather than a fault: every
		// surface that carried a hand-kept mask now derives one from
		// Spec.Fields. firewall_policy was the last, and the property its row
		// held -- match_ip_sec, match_opposite_protocol and predefined stay off
		// the wire -- is asserted against the derivation in
		// Test_firewallPolicyKit_neverWritesTheExposedThree.
	}
}

// TestPlainMasksMatchTheirResource is the declare-then-derive check: the
// hand-written list must equal the set the resource actually assigns.
//
// The two failure directions are not symmetrical. A field assigned but left
// out of the mask is never written, so the practitioner sets an attribute and
// nothing happens -- silent, and the shape that took wan_ip_aliases. A field
// masked but never assigned sends a zero the resource does not own, which is
// the very clobber the mask exists to stop.
func TestPlainMasksMatchTheirResource(t *testing.T) {
	// AN EMPTY TABLE RUNS NO SUBTESTS AND REPORTS SUCCESS, which is the exact
	// shape of a check that cannot fail. It is skipped loudly instead, so
	// "everything migrated" stays distinguishable from "somebody emptied the
	// table" -- and a skip rather than a failure, because a check that breaks
	// at the moment the thing it guards starts working is one somebody deletes
	// rather than reads.
	if len(plainMaskedSurfaces()) == 0 {
		t.Skip("no surface carries a hand-kept wire mask; they all derive one from Spec.Fields")
	}

	for _, surface := range plainMaskedSurfaces() {
		t.Run(surface.name, func(t *testing.T) {
			tags, _ := wireTagsOf(surface.object)
			assigned := sdkFieldsAssignedIn(t, surface.file, surface.literal, surface.objectVar)
			if len(assigned) == 0 {
				t.Fatal("no assignments found; the parse failed and every assertion " +
					"below would pass against an empty set")
			}

			var want []string
			for _, field := range assigned {
				tag, ok := tags[field]
				if !ok {
					continue // not a wire field (embedded, or unexported)
				}
				if tag == "_id" || tag == "site_id" {
					continue // identity, not managed state
				}
				want = append(want, tag)
			}
			sort.Strings(want)
			got := append([]string(nil), surface.declared()...)
			sort.Strings(got)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("the declared mask disagrees with the resource.\n  declared: %v\n  derived:  %v",
					got, want)
			}
		})
	}
}

// TestPlainMasksExcludeWhatTheResourceDoesNotManage asserts the defect itself,
// with the control that makes the assertion mean something.
//
// Excluding a name from a mask proves nothing on its own: if the struct
// carried omitempty for that field, a whole-object write would have omitted it
// anyway and the mask changed nothing. So each exposed field is also asserted
// to FORCE-EMIT. That is the difference between the five fields fixed here and
// radius_profile's six x_client_* secrets, which are unmanaged too and were
// never at risk, because omitempty drops them.
func TestPlainMasksExcludeWhatTheResourceDoesNotManage(t *testing.T) {
	if len(plainMaskedSurfaces()) == 0 {
		t.Skip("no surface carries a hand-kept wire mask; they all derive one from Spec.Fields")
	}

	for _, surface := range plainMaskedSurfaces() {
		t.Run(surface.name, func(t *testing.T) {
			mask := surface.declared()
			tags, forceEmits := wireTagsOf(surface.object)
			assigned := sdkFieldsAssignedIn(t, surface.file, surface.literal, surface.objectVar)
			assignedTags := map[string]bool{}
			for _, field := range assigned {
				assignedTags[tags[field]] = true
			}

			for _, name := range surface.exposed {
				if slices.Contains(mask, name) {
					t.Errorf("%s is in the mask, but the resource does not manage it; "+
						"the write would keep resetting it", name)
				}
				if !forceEmits[name] {
					t.Errorf("%s carries omitempty, so a whole-object write never sent it "+
						"and excluding it from the mask proves nothing", name)
				}
				if assignedTags[name] {
					t.Errorf("%s IS assigned by the resource, so it is managed and this "+
						"entry is wrong", name)
				}
			}
			if !slices.Contains(mask, surface.managed) {
				t.Errorf("%s is missing from the mask, so a practitioner setting it would "+
					"see nothing happen", surface.managed)
			}
		})
	}
}

// TestPlainMasksNameOnlyRealWireNames catches a typo in a hand-written list.
// go-unifi's maskedBody refuses a mask naming a key the encoding does not
// carry, so a misspelling fails the apply rather than being ignored.
func TestPlainMasksNameOnlyRealWireNames(t *testing.T) {
	if len(plainMaskedSurfaces()) == 0 {
		t.Skip("no surface carries a hand-kept wire mask; they all derive one from Spec.Fields")
	}

	for _, surface := range plainMaskedSurfaces() {
		t.Run(surface.name, func(t *testing.T) {
			tags, _ := wireTagsOf(surface.object)
			known := map[string]bool{}
			for _, tag := range tags {
				known[tag] = true
			}
			for _, name := range surface.declared() {
				if !known[name] {
					t.Errorf("the mask names %q, which is not a field of the SDK type", name)
				}
			}
		})
	}
}

// TestPlainMaskedSurfacesUseTheMaskedCall is the assertion that does not
// revert quietly. Every check above still passes if the update goes back to
// the whole-object call: the list is still correct, still spelled right, still
// excludes the unmanaged fields -- it is simply no longer consulted, and Go
// does not complain about an unused package-level function.
func TestPlainMaskedSurfacesUseTheMaskedCall(t *testing.T) {
	// AN EMPTY TABLE RUNS NO SUBTESTS AND REPORTS SUCCESS, which is the exact
	// shape of a check that cannot fail. It is skipped loudly instead, so
	// "everything migrated" stays distinguishable from "somebody emptied the
	// table" -- and a skip rather than a failure, because a check that breaks
	// at the moment the thing it guards starts working is one somebody deletes
	// rather than reads.
	if len(plainMaskedSurfaces()) == 0 {
		t.Skip("no surface carries a hand-kept wire mask; they all derive one from Spec.Fields")
	}

	for _, surface := range plainMaskedSurfaces() {
		t.Run(surface.name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", surface.file))
			if err != nil {
				t.Fatalf("reading the resource: %v", err)
			}
			src := string(raw)
			if !strings.Contains(src, surface.maskedCall) {
				t.Errorf("the update does not call %s; a whole-object write resets "+
					"every field this resource does not manage", surface.maskedCall)
			}
			if strings.Contains(src, surface.wholeObjectCall) {
				t.Errorf("a whole-object %s call remains", surface.wholeObjectCall)
			}
			// The control: this really is the file the assertions are about.
			if !strings.Contains(src, "unifi."+surface.literal+"{") {
				t.Fatalf("the file read does not build a unifi.%s; the path is wrong "+
					"and the assertions above prove nothing", surface.literal)
			}
		})
	}
}

// wireTagsOf reads the json tags off an SDK struct by reflection, returning
// the Go-field-to-wire-name map and the set of wire names that are emitted
// unconditionally (no omitempty).
func wireTagsOf(object any) (map[string]string, map[string]bool) {
	typ := reflect.TypeOf(object)
	tags := make(map[string]string, typ.NumField())
	forceEmits := make(map[string]bool, typ.NumField())
	for i := range typ.NumField() {
		field := typ.Field(i)
		tag, ok := field.Tag.Lookup("json")
		if !ok || tag == "-" {
			continue
		}
		parts := strings.Split(tag, ",")
		name := parts[0]
		if name == "" {
			continue
		}
		tags[field.Name] = name
		forceEmits[name] = !slices.Contains(parts[1:], "omitempty")
	}
	return tags, forceEmits
}

// sdkFieldsAssignedIn returns the SDK fields a resource file sets: the keys of
// any composite literal of the type, plus any later assignment to the bound
// variable.
//
// It scans the WHOLE FILE rather than one named function on purpose. Scoping
// this kind of derivation to a single mapper has produced a wrong answer here
// twice, both times by missing an assignment that lived just outside it.
func sdkFieldsAssignedIn(t *testing.T, path, literal, objectVar string) []string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", path))
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	seen := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CompositeLit:
			selector, ok := node.Type.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != literal {
				return true
			}
			for _, element := range node.Elts {
				kv, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, ok := kv.Key.(*ast.Ident); ok {
					seen[key.Name] = true
				}
			}
		case *ast.AssignStmt:
			for _, lhs := range node.Lhs {
				selector, ok := lhs.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				if ident, ok := selector.X.(*ast.Ident); ok && ident.Name == objectVar {
					seen[selector.Sel.Name] = true
				}
			}
		}
		return true
	})

	fields := make([]string, 0, len(seen))
	for field := range seen {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fields
}
