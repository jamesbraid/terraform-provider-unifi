// Command go-unifi-pin prints one fact about the pinned go-unifi dependency.
//
// It exists so the shell scripts that have not been rewritten yet can read the
// pin without keeping their own copy of it. They used to `source
// go-unifi-pin.sh` and read its variables; now they ask for one field at a
// time. When those scripts become Go, they import the package directly and this
// command has no callers left.
//
// One field per invocation, printed bare. The alternative -- emitting an object
// and letting the caller pick a field out of it -- puts a parser back in the
// shell, which is the thing being removed.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/gounifipin"
)

func main() {
	field := flag.String("field", "", "which pin fact to print")
	root := flag.String("repository-root", ".", "the repository whose go.mod and go.sum are read")
	flag.Parse()

	value, err := lookup(*field, *root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(value)
}

func lookup(field, root string) (string, error) {
	switch field {
	case "module-path":
		return gounifipin.ModulePath, nil
	case "module-origin":
		return gounifipin.ModuleOrigin, nil
	case "expected-commit":
		return gounifipin.ExpectedCommit(), nil
	case "expected-sum":
		return gounifipin.ExpectedSum(), nil
	case "declared-version":
		return gounifipin.DeclaredVersion(root)
	case "declared-sum":
		version, err := gounifipin.DeclaredVersion(root)
		if err != nil {
			return "", err
		}
		return gounifipin.DeclaredSum(root, version)
	case "":
		return "", fmt.Errorf("-field is required; one of %s", known())
	default:
		return "", fmt.Errorf("unknown field %q; one of %s", field, known())
	}
}

func known() string {
	fields := []string{
		"module-path", "module-origin", "expected-commit",
		"expected-sum", "declared-version", "declared-sum",
	}
	sort.Strings(fields)
	return strings.Join(fields, ", ")
}
