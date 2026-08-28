package sdkshape

import (
	"os"
	"testing"

	"golang.org/x/mod/modfile"
)

// A directory replace is how the SDK is developed against before it is
// tagged; it must never be committed to a trunk, because CI resolves the
// SDK from the public proxy and would build a different provider.
func TestGoModReplacesNameAVersionNotADirectory(t *testing.T) {
	data, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	file, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, replace := range file.Replace {
		if replace.New.Version == "" {
			t.Errorf("go.mod replaces %s with the directory %q; point it at a tagged version before this branch is reviewed", replace.Old.Path, replace.New.Path)
		}
	}
}
