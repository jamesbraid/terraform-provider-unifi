package dependencypin

import (
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/releasequalification"
)

// BuildReceipt assembles the publishability receipt from what the run measured.
//
// FOUR OF ITS FIELDS WERE LITERALS IN THE JQ TEMPLATE, and each is a claim the
// consumers check against the same constant that produced it, so none of the
// checks could fail:
//
//	result:            "pass"            the script exited before reaching the template
//	replace_present:   false             measured on line 55 and then discarded
//	resolution_runner: "remote_ci"       nothing measured where the run happened
//	network_boundary:  "remote_ci_only"  nothing measured the network boundary
//
// replace_present is the sharpest, because the measurement existed. `go list -m
// -json` was already asked whether a Replace key was present, the answer was
// compared, and then the literal false was written into the receipt anyway.
// Deleting that comparison would have left the receipt still claiming false and
// all three consumers still passing.
//
// resolution_runner is the one field here that cannot be measured from inside
// the process, so it is an input rather than a constant: a run that cannot show
// it was CI records something else and is REJECTED downstream, which is the
// right way round. A wrong value that blocks is recoverable; a wrong value that
// passes is what this comment is about.
func BuildReceipt(pin Pin, declared Declared, resolved Resolved, providerCommit, resolutionRunner string) releasequalification.DependencyPublishabilityReceipt {
	result := "pass"
	if len(Check(pin, declared, resolved)) > 0 {
		result = "blocked"
	}
	return releasequalification.DependencyPublishabilityReceipt{
		FormatVersion:    1,
		Gate:             "go-unifi-dependency-publishability",
		Result:           result,
		ProviderCommit:   providerCommit,
		ModulePath:       resolved.Path,
		ModuleVersion:    resolved.Version,
		ModuleCommit:     resolved.OriginCommit,
		ModuleZipSHA256:  resolved.ArchiveSHA256,
		ModuleDirSHA256:  resolved.TreeSHA256,
		ReplacePresent:   resolved.ReplacePresent,
		ResolutionRunner: resolutionRunner,
		NetworkBoundary:  NetworkBoundary(resolved.ProxyDisabled),
	}
}
