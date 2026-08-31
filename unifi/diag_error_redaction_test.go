package unifi

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/ast/astutil"
)

// go-unifi appends the request body it just sent to every non-2xx error, and
// redacts it by matching wire names against a fixed six-substring list. The
// controller's own sensitive_metadata.json declares 64 sensitive fields; those
// six substrings match 13 of them. So a raw SDK error reaching a diagnostic
// prints whatever the list failed to predict -- unifi_setting.guest_access's
// payment keys and snmp.community were both measured printing in full against
// a real 400.
//
// resourcekit.DiagErrorText drops the payload instead of predicting field
// names. This check exists because that protection is per-call-site: it is one
// forgotten err.Error() in one new resource away from being undone, and the
// failure is silent -- a leaked secret raises no error and fails no test.
//
// A grep would pass vacuously the moment a call site wraps the error in
// fmt.Sprintf, so this walks the AST and reports the file and line of every
// offender.
func TestNoRawSDKErrorReachesADiagnostic(t *testing.T) {
	sinks := map[string]bool{
		"AddError": true, "AddAttributeError": true,
		"AddWarning": true, "AddAttributeWarning": true,
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("listing the package: %v", err)
	}

	var offenders []string
	sinksSeen := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !sinks[sel.Sel.Name] {
				return true
			}
			sinksSeen++
			for _, arg := range call.Args {
				astutil.Apply(arg, nil, func(c *astutil.Cursor) bool {
					inner, ok := c.Node().(*ast.CallExpr)
					if !ok || len(inner.Args) != 0 {
						return true
					}
					isel, ok := inner.Fun.(*ast.SelectorExpr)
					if !ok || isel.Sel.Name != "Error" {
						return true
					}
					pos := fset.Position(inner.Pos())
					offenders = append(offenders, fmt.Sprintf("%s:%d in %s",
						filepath.Base(pos.Filename), pos.Line, sel.Sel.Name))
					return true
				})
			}
			return true
		})
	}

	// Without this the check passes on a glob that matched nothing, or on a
	// sink name the framework renamed out from under it.
	if sinksSeen == 0 {
		t.Fatal("found no diagnostic sinks at all, so the scan below proved nothing")
	}

	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Errorf("%d call site(s) pass a raw error to a diagnostic, which prints the SDK's "+
			"unredacted request payload; wrap the error in resourcekit.DiagErrorText:\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}
