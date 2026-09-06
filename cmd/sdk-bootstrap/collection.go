package main

import (
	"fmt"
	"os"
	"regexp"

	unifi "github.com/ubiquiti-community/go-unifi/unifi"
)

// restCollectionPattern is the one shape every generated rest client method
// addresses its collection with. The stat/ paths beside it name read-only
// projections, not the collection the controller's sensitive metadata is
// keyed by, so they are deliberately not matched.
var restCollectionPattern = regexp.MustCompile(`api/s/%s/rest/([a-z0-9_]+)`)

// structCollection reports the controller collection name a struct's records
// live in -- the key unifi.SensitiveFieldsByCollection uses -- derived from
// the same file the struct's declaration was read from, where the generated
// client methods spell out the endpoint. A struct whose file addresses no
// rest collection (the v2 API surfaces, report-only types) has none, and
// returns "". Two distinct collections in one file would make the answer a
// guess, so that refuses instead.
//
// The settings package is the one place the rule does not hold: a settings
// struct has no client file of its own -- every section reads and writes
// through the shared setting endpoint -- so the package itself names the
// collection. Gated on pkgPath exactly as newSDKConstraints gates its
// settings fallback, and for the same reason: only an invocation that asked
// for the settings package gets settings semantics.
func structCollection(pkgPath, filename string) (string, error) {
	if pkgPath == settingsPackagePath {
		return "setting", nil
	}
	source, err := os.ReadFile(filename) // #nosec G304 -- filename comes from resolving the -package build-time flag through go/importer, not user input
	if err != nil {
		return "", fmt.Errorf("read %s: %w", filename, err)
	}
	var collection string
	for _, match := range restCollectionPattern.FindAllSubmatch(source, -1) {
		found := string(match[1])
		if collection == "" {
			collection = found
			continue
		}
		if collection != found {
			return "", fmt.Errorf(
				"%s addresses two rest collections, %q and %q; which one the struct's records "+
					"live in cannot be derived",
				filename, collection, found)
		}
	}
	return collection, nil
}

// sensitiveLeaves is the controller's own sensitivity declaration for one
// collection, as a set of leaf wire names -- a nested declaration like
// auth_servers.x_secret is recorded by the SDK as x_secret, the key a
// consumer sees on the object that holds it, so the set applies at every
// nesting depth of the walk. Nil for a struct with no collection, and for a
// collection the controller declares no secrets in.
func sensitiveLeaves(collection string) map[string]bool {
	if collection == "" {
		return nil
	}
	names := unifi.SensitiveFieldsByCollection[collection]
	if len(names) == 0 {
		return nil
	}
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}
