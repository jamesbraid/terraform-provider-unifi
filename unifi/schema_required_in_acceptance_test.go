package unifi

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// TestAcceptanceConfigsSetEveryRequiredAttribute guards the shape of defect
// unifi_wlan had: an acceptance fixture that declares a data source it
// needs but never wires into a Required attribute, so the resource relies
// on a default the schema says does not exist.
//
// Scope, kept deliberately narrow rather than exhaustive:
//   - Only a resource's TOP-LEVEL Required attributes are checked. A
//     Required attribute nested inside a block or object is not.
//   - A resource is only checked if this package declares at least one
//     zero-argument `testAcc*Config*() string` function whose body is a
//     single `return` of a raw string literal -- the shape essentially
//     every acceptance config here uses. The handful that take arguments
//     (an interpolated fixture) can't be evaluated without calling them
//     with made-up values, so they're skipped rather than misreported. A
//     resource with no such function -- no acceptance test at all, or only
//     a parameterized one -- is out of scope: there's nothing to compare
//     its schema against.
//   - Within a covered resource, an attribute needs to appear in only ONE
//     of its config functions to satisfy the guard. This catches "no
//     acceptance config for this resource ever sets it", not "every single
//     step's fixture sets it".
func TestAcceptanceConfigsSetEveryRequiredAttribute(t *testing.T) {
	bodies, err := acceptanceConfigBodiesByResourceType(".")
	if err != nil {
		t.Fatal(err)
	}
	if len(bodies) == 0 {
		t.Fatal("found no testAcc*Config*() string function; this guard would check nothing")
	}

	ctx := context.Background()
	provider := &unifiProvider{}
	checked := 0
	for _, newResource := range provider.Resources(ctx) {
		res := newResource()
		var meta resource.MetadataResponse
		res.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "unifi"}, &meta)

		configs, ok := bodies[meta.TypeName]
		if !ok {
			continue // out of scope: no parseable acceptance config references this resource
		}
		checked++

		var got resource.SchemaResponse
		res.Schema(ctx, resource.SchemaRequest{}, &got)
		for _, name := range requiredTopLevelAttributes(got.Schema.Attributes) {
			if !anyConfigSetsAttribute(configs, meta.TypeName, name) {
				t.Errorf("%s.%s is Required but no acceptance config for %s ever sets it",
					meta.TypeName, name, meta.TypeName)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no resource's type name matched any parsed acceptance config; this guard would check nothing")
	}
}

func requiredTopLevelAttributes(attrs map[string]rschema.Attribute) []string {
	var names []string
	for name, a := range attrs {
		if a.IsRequired() {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// testAccConfigFuncName matches the naming convention every acceptance
// config function in this package follows.
var testAccConfigFuncName = regexp.MustCompile(`^testAcc.*Config.*$`)

// acceptanceConfigBodiesByResourceType parses every *_test.go file in dir
// and returns, for each terraform resource type a config string declares, the
// raw config strings that declare it.
func acceptanceConfigBodiesByResourceType(dir string) (map[string][]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	bodies := map[string][]string{}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !testAccConfigFuncName.MatchString(fn.Name.Name) {
				continue
			}
			config, ok := singleRawStringReturn(fn)
			if !ok {
				continue // parameterized or computed config; not parseable generically
			}
			for _, resourceType := range resourceTypesIn(config) {
				bodies[resourceType] = append(bodies[resourceType], config)
			}
		}
	}
	return bodies, nil
}

// singleRawStringReturn reports the function's return value when its whole
// body is one `return "<literal>"` statement over zero parameters -- the
// shape a config function must have for its value to be known without
// calling it with arguments this test would have to invent.
func singleRawStringReturn(fn *ast.FuncDecl) (string, bool) {
	if fn.Type.Params != nil && len(fn.Type.Params.List) > 0 {
		return "", false
	}
	if fn.Body == nil || len(fn.Body.List) != 1 {
		return "", false
	}
	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return "", false
	}
	lit, ok := ret.Results[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

// resourceBlockPattern finds a `resource "unifi_x" "label" {` block's
// opening brace; the body is then taken by counting braces from there, so a
// nested block (mac_filter = { ... }) doesn't truncate the match early.
var resourceBlockPattern = regexp.MustCompile(`resource\s+"(unifi_[a-z0-9_]+)"\s+"[a-zA-Z0-9_-]+"\s*\{`)

// resourceTypesIn names every resource type a config string declares.
func resourceTypesIn(config string) []string {
	seen := map[string]bool{}
	var types []string
	for _, match := range resourceBlockPattern.FindAllStringSubmatch(config, -1) {
		if !seen[match[1]] {
			seen[match[1]] = true
			types = append(types, match[1])
		}
	}
	return types
}

// resourceBlockBodies extracts the brace-balanced body of every
// `resource "resourceType" ...` block in config.
func resourceBlockBodies(config, resourceType string) []string {
	var bodies []string
	for _, match := range resourceBlockPattern.FindAllStringSubmatchIndex(config, -1) {
		if config[match[2]:match[3]] != resourceType {
			continue
		}
		open := match[1] - 1 // index of the '{' the pattern consumed
		depth := 0
		for i := open; i < len(config); i++ {
			switch config[i] {
			case '{':
				depth++
			case '}':
				depth--
			}
			if depth == 0 {
				bodies = append(bodies, config[open+1:i])
				break
			}
		}
	}
	return bodies
}

// anyConfigSetsAttribute reports whether name is assigned inside any of
// resourceType's own blocks across configs. The search is scoped to the
// extracted block body, not the whole config string, so a data source or a
// different resource sharing the same config text can't produce a false pass.
func anyConfigSetsAttribute(configs []string, resourceType, name string) bool {
	assignment := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(name) + `\s*=`)
	for _, config := range configs {
		for _, body := range resourceBlockBodies(config, resourceType) {
			if assignment.MatchString(body) {
				return true
			}
		}
	}
	return false
}
