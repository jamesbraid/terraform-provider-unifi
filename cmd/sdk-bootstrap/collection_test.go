package main

import (
	"go/token"
	"go/types"
	"strings"
	"testing"
)

// Test_structCollectionReadsTheRestPathFromTheDeclaringFile pins the shape
// every generated rest client file has: several methods, one collection.
func Test_structCollectionReadsTheRestPathFromTheDeclaringFile(t *testing.T) {
	path := writeFixture(t, `package fixture

type Widget struct{}

func listPath(site string) string { return fmt.Sprintf("api/s/%s/rest/widgetconf", site) }
func getPath(site, id string) string { return fmt.Sprintf("api/s/%s/rest/widgetconf/%s", site, id) }
`)
	got, err := structCollection("github.com/ubiquiti-community/go-unifi/unifi", path)
	if err != nil {
		t.Fatalf("structCollection() error = %v", err)
	}
	if got != "widgetconf" {
		t.Errorf("structCollection() = %q, want %q", got, "widgetconf")
	}
}

// Test_structCollectionIsEmptyForAFileWithNoRestPath covers the v2 API
// surfaces, whose files address no rest collection at all: no collection is
// an answer, not an error.
func Test_structCollectionIsEmptyForAFileWithNoRestPath(t *testing.T) {
	path := writeFixture(t, `package fixture

type Widget struct{}

func listPath(site string) string { return fmt.Sprintf("v2/api/site/%s/widgets", site) }
`)
	got, err := structCollection("github.com/ubiquiti-community/go-unifi/unifi", path)
	if err != nil {
		t.Fatalf("structCollection() error = %v", err)
	}
	if got != "" {
		t.Errorf("structCollection() = %q, want empty", got)
	}
}

// Test_structCollectionRefusesTwoDistinctCollections: picking either one
// would be a guess, and a guess here decides which fields get masked.
func Test_structCollectionRefusesTwoDistinctCollections(t *testing.T) {
	path := writeFixture(t, `package fixture

func a(site string) string { return fmt.Sprintf("api/s/%s/rest/widgetconf", site) }
func b(site string) string { return fmt.Sprintf("api/s/%s/rest/gadgetconf", site) }
`)
	_, err := structCollection("github.com/ubiquiti-community/go-unifi/unifi", path)
	if err == nil {
		t.Fatal("structCollection() accepted a file addressing two rest collections")
	}
	for _, want := range []string{"widgetconf", "gadgetconf"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("structCollection() error = %v, want it to name %q", err, want)
		}
	}
}

// Test_structCollectionIsSettingForTheSettingsPackage: a settings struct has
// no client file of its own, so the package names the collection -- and the
// file is never read, exactly as newSDKConstraints gates on the same path.
func Test_structCollectionIsSettingForTheSettingsPackage(t *testing.T) {
	got, err := structCollection(settingsPackagePath, "/does/not/exist.go")
	if err != nil {
		t.Fatalf("structCollection() error = %v", err)
	}
	if got != "setting" {
		t.Errorf("structCollection() = %q, want %q", got, "setting")
	}
}

// Test_sensitiveLeavesCarriesTheSDKDeclaration is the positive control for
// the whole derivation: if the SDK's exported map is renamed, emptied or
// re-keyed, every bootstrap would silently stop marking anything, so pin one
// entry that must exist -- the controller has always declared the RADIUS
// profile shared secret.
func Test_sensitiveLeavesCarriesTheSDKDeclaration(t *testing.T) {
	set := sensitiveLeaves("radiusprofile")
	if !set["x_secret"] {
		t.Fatalf("sensitiveLeaves(%q) = %v, want it to carry %q: the SDK's "+
			"SensitiveFieldsByCollection no longer says what this derivation is built on",
			"radiusprofile", set, "x_secret")
	}
	if sensitiveLeaves("") != nil {
		t.Error("sensitiveLeaves(\"\") != nil; a struct with no collection must mark nothing")
	}
}

// Test_walkMarksDeclaredSensitiveLeavesAtEveryDepth: the SDK records a nested
// declaration (auth_servers.x_secret) by its leaf name, so the set applies at
// every depth of the walk, and only to names it carries.
func Test_walkMarksDeclaredSensitiveLeavesAtEveryDepth(t *testing.T) {
	server := types.NewStruct(
		[]*types.Var{
			tagged("IP", types.String),
			tagged("XSecret", types.String),
		},
		[]string{`json:"ip"`, `json:"x_secret"`},
	)
	outer := types.NewStruct(
		[]*types.Var{
			tagged("Name", types.String),
			tagged("XSecret", types.String),
			types.NewField(token.NoPos, nil, "AuthServers", types.NewSlice(named("Server", server)), false),
		},
		[]string{`json:"name"`, `json:"x_secret"`, `json:"auth_servers"`},
	)

	got := walk(outer, "Outer", nil, map[string]bool{"x_secret": true})

	byName := map[string]field{}
	for _, f := range got {
		byName[f.Name] = f
	}
	if !byName["x_secret"].Sensitive {
		t.Error("top-level x_secret not marked sensitive")
	}
	if byName["name"].Sensitive {
		t.Error("name marked sensitive; the set names only x_secret")
	}
	var nested field
	for _, f := range byName["auth_servers"].Fields {
		if f.Name == "x_secret" {
			nested = f
		}
	}
	if !nested.Sensitive {
		t.Errorf("auth_servers.x_secret not marked sensitive; leaf names must apply at depth (got %+v)", byName["auth_servers"].Fields)
	}
}
