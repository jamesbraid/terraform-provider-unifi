package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

// goListPackage is the slice of `go list -json <pkg>` this tool reads: which
// module provides the package once go.mod's replace directives apply.
type goListPackage struct {
	Module struct {
		Path    string
		Version string
		Replace *struct {
			Path    string
			Version string
			Dir     string
		}
	}
}

// goListModule is the slice of `go list -m -json <path>@<version>` this tool
// reads: the VCS hash the module proxy recorded for the version.
type goListModule struct {
	Origin *struct{ Hash string }
}

// resolveSDKModule asks the go tool which module actually provides pkgPath in
// this build, so the bootstrap records the SDK the schema was derived from
// rather than a commit someone typed into a generate directive.
func resolveSDKModule(pkgPath string) (bootstrapSource, error) {
	pkgJSON, err := exec.Command("go", "list", "-json", pkgPath).Output() // #nosec G204 -- pkgPath comes from the -package build-time flag
	if err != nil {
		return bootstrapSource{}, fmt.Errorf("go list %s: %w", pkgPath, err)
	}
	var pkg goListPackage
	if err := json.Unmarshal(pkgJSON, &pkg); err != nil {
		return bootstrapSource{}, fmt.Errorf("decode go list output: %w", err)
	}
	var modJSON []byte
	if repo, version := moduleIdentity(pkg); version != "" {
		modJSON, err = exec.Command("go", "list", "-m", "-json", repo+"@"+version).Output() // #nosec G204 -- repo and version come from parsing 'go list -json' output for the -package build-time flag
		if err != nil {
			return bootstrapSource{}, fmt.Errorf("go list -m %s@%s: %w", repo, version, err)
		}
	}
	return parseGoList(pkgJSON, modJSON)
}

// moduleIdentity returns the module path and version after any replace. A
// directory replace has no version.
func moduleIdentity(pkg goListPackage) (repo, version string) {
	if pkg.Module.Replace != nil {
		return pkg.Module.Replace.Path, pkg.Module.Replace.Version
	}
	return pkg.Module.Path, pkg.Module.Version
}

// parseGoList turns the two go list documents into the bootstrap's source
// record. modJSON may be nil when there is no version to look up.
func parseGoList(pkgJSON, modJSON []byte) (bootstrapSource, error) {
	var pkg goListPackage
	if err := json.Unmarshal(pkgJSON, &pkg); err != nil {
		return bootstrapSource{}, fmt.Errorf("decode package: %w", err)
	}
	repo, version := moduleIdentity(pkg)
	if repo == "" {
		return bootstrapSource{}, fmt.Errorf("go list named no module for the package")
	}
	source := bootstrapSource{Repository: repo, Version: version}
	if version == "" {
		// A local checkout stands in for the module: there is nothing to
		// version or hash, and saying so beats inventing either.
		source.Version = "directory replace"
		return source, nil
	}
	if len(modJSON) == 0 {
		return source, nil
	}
	var mod goListModule
	if err := json.Unmarshal(modJSON, &mod); err != nil {
		return bootstrapSource{}, fmt.Errorf("decode module: %w", err)
	}
	if mod.Origin != nil {
		source.Commit = mod.Origin.Hash
	}
	return source, nil
}
