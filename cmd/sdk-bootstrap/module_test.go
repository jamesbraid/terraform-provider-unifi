package main

import "testing"

func Test_parseGoListReportsTheReplacedModule(t *testing.T) {
	pkg := []byte(`{"ImportPath":"github.com/ubiquiti-community/go-unifi/unifi","Module":{"Path":"github.com/ubiquiti-community/go-unifi","Version":"v1.103.0","Replace":{"Path":"github.com/jamesbraid/go-unifi","Version":"v1.105.1"}}}`)
	mod := []byte(`{"Path":"github.com/jamesbraid/go-unifi","Version":"v1.105.1","Origin":{"VCS":"git","Hash":"d20e126f0e2321727a66d5faeffeb434c215c455"}}`)

	got, err := parseGoList(pkg, mod)
	if err != nil {
		t.Fatal(err)
	}
	want := bootstrapSource{Repository: "github.com/jamesbraid/go-unifi", Version: "v1.105.1", Commit: "d20e126f0e2321727a66d5faeffeb434c215c455"}
	if got != want {
		t.Errorf("parseGoList() = %+v, want %+v", got, want)
	}
}

func Test_parseGoListNamesADirectoryReplaceInsteadOfInventingAVersion(t *testing.T) {
	pkg := []byte(`{"Module":{"Path":"github.com/ubiquiti-community/go-unifi","Version":"v1.103.0","Replace":{"Path":"/Users/x/go-unifi","Dir":"/Users/x/go-unifi"}}}`)

	got, err := parseGoList(pkg, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := bootstrapSource{Repository: "/Users/x/go-unifi", Version: "directory replace"}
	if got != want {
		t.Errorf("parseGoList() = %+v, want %+v -- a local replace has no version or commit and must say so", got, want)
	}
}

func Test_parseGoListWithoutAReplaceUsesTheModuleItself(t *testing.T) {
	pkg := []byte(`{"Module":{"Path":"github.com/ubiquiti-community/go-unifi","Version":"v1.103.0"}}`)
	mod := []byte(`{"Path":"github.com/ubiquiti-community/go-unifi","Version":"v1.103.0"}`)

	got, err := parseGoList(pkg, mod)
	if err != nil {
		t.Fatal(err)
	}
	if got.Repository != "github.com/ubiquiti-community/go-unifi" || got.Version != "v1.103.0" || got.Commit != "" {
		t.Errorf("parseGoList() = %+v; want the unreplaced module, version v1.103.0, no commit when the module info carries no Origin", got)
	}
}

func Test_resolveSDKModuleAgreesWithGoMod(t *testing.T) {
	got, err := resolveSDKModule("github.com/ubiquiti-community/go-unifi/unifi")
	if err != nil {
		t.Fatal(err)
	}
	if got.Repository == "" || got.Version == "" {
		t.Fatalf("resolveSDKModule() = %+v; the build list always names a module and a version (or 'directory replace')", got)
	}
	if got.Version != "directory replace" && len(got.Commit) != 40 {
		t.Errorf("resolveSDKModule() commit = %q; a tagged module resolved through the proxy carries its VCS hash", got.Commit)
	}
}
