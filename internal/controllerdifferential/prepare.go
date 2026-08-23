package controllerdifferential

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/releasedtree"
)

// Layer is one addition to the released tree, and the vet that follows it.
//
// THE RELEASED SIDE IS A COMPOSITE of three sources -- the released tag, the
// candidate's test harness, and the candidate's scenario files -- with three
// different owners. Vetting once at the end reports that something is broken;
// vetting per layer reports WHICH owner broke it. Each pass costs a couple of
// seconds against a suite measured in tens of minutes.
type Layer struct {
	// Label is for the operator.
	Label string
	// Advice is what to look at when this layer is the one that broke, and it
	// is different for each: the tag failing alone is not the graft, and the
	// graft failing is not the tag.
	Advice string
}

// PrepareResult is what preparing the released tree produced.
type PrepareResult struct {
	Root          string
	GraftedOwners []string
	Layers        []Layer
}

// PrepareReleasedTree extracts the released tag, grafts the candidate's harness
// and its lendable scenario files onto it, and vets after each.
//
// It needs git and a Go toolchain and nothing else -- no controller, no docker,
// no Linux. The shell split the script at exactly this point for the same
// reason: keeping preparation and execution together meant the tree could not
// be built anywhere the controllers cannot run, so these vets were unreachable
// from any self-test and had to be verified by hand.
//
// A COMPILE FAILURE HERE IS THE CHEAPEST FACT IN THE PIPELINE AND WAS BEING
// PAID FOR AT THE MOST EXPENSIVE MOMENT: it used to surface when the released
// suite ran, which is after the candidate suite had already spent its hour.
// THE TAG AND THE GRAFT COME FROM DIFFERENT PARAMETERS even though production
// passes the same directory for both. They are different sources -- git history
// versus the candidate's working files -- and the composite this builds is only
// describable if they can be named apart. It is also the only way a test can
// stage a deliberately broken scenario owner without rewriting the repository
// it is running in.
func PrepareReleasedTree(repository, candidateRoot, tag, root string, sharedOwners []string) (PrepareResult, error) {
	result := PrepareResult{Root: root}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return result, err
	}
	if err := releasedtree.Extract(repository, tag, root); err != nil {
		return result, err
	}

	extracted := Layer{
		Label: "extracting " + tag,
		Advice: "Nothing of the candidate's has been copied in yet, so this is the released tag " +
			"failing to build on its own. Do not look for it in the graft.",
	}
	result.Layers = append(result.Layers, extracted)
	if err := vetReleasedTree(root, extracted); err != nil {
		return result, err
	}

	// The harness is campaign infrastructure, not provider runtime, and it goes
	// onto both sides unconditionally so the two are measured by the same
	// digest-aware fixture.
	if err := os.RemoveAll(filepath.Join(root, "internal", "controllertest")); err != nil {
		return result, err
	}
	if err := copyTree(filepath.Join(candidateRoot, "internal", "controllertest"),
		filepath.Join(root, "internal", "controllertest")); err != nil {
		return result, fmt.Errorf("graft the controllertest harness: %w", err)
	}
	// acctestenv travels WITH the harness, because it is part of the same
	// contract: it names the environment variables the fleet publishes, and
	// both the harness and the scenario owners grafted below read them.
	//
	// It is a separate package precisely so an ordinary `go test ./unifi` does
	// not import the harness to read two strings -- that edge was worth 119
	// third-party modules. Splitting it created this obligation: a package the
	// grafted files import has to exist on the released side too, and the
	// released tag predates it.
	//
	// COPIED ONLY IF THE CANDIDATE HAS IT. A candidate predating the split has
	// no such package and needs none, and a graft that insisted would turn a
	// missing optional file into a failure that names the wrong thing -- which
	// is what TestAScenarioOwnerThatDoesNotCompileIsRefused caught when this
	// was written unconditionally.
	acctestenvSource := filepath.Join(candidateRoot, "internal", "acctestenv")
	if _, err := os.Stat(acctestenvSource); err == nil {
		if err := os.RemoveAll(filepath.Join(root, "internal", "acctestenv")); err != nil {
			return result, err
		}
		if err := copyTree(acctestenvSource,
			filepath.Join(root, "internal", "acctestenv")); err != nil {
			return result, fmt.Errorf("graft the acctestenv names: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return result, fmt.Errorf("graft the acctestenv names: %w", err)
	}
	if err := copyFile(filepath.Join(candidateRoot, "docker-compose.yaml"),
		filepath.Join(root, "docker-compose.yaml")); err != nil {
		return result, fmt.Errorf("graft docker-compose.yaml: %w", err)
	}
	harness := Layer{
		Label: "grafting the candidate's internal/controllertest harness",
		Advice: "The harness is copied onto both sides unconditionally and no scenario owner has " +
			"been copied yet. The break is between the candidate's harness and the released provider.",
	}
	result.Layers = append(result.Layers, harness)
	if err := vetReleasedTree(root, harness); err != nil {
		return result, err
	}

	for _, owner := range sharedOwners {
		if err := copyFile(filepath.Join(candidateRoot, owner), filepath.Join(root, owner)); err != nil {
			return result, fmt.Errorf("graft scenario owner %s: %w", owner, err)
		}
		result.GraftedOwners = append(result.GraftedOwners, owner)
	}
	owners := Layer{
		Label: fmt.Sprintf("grafting %d scenario owner(s)", len(result.GraftedOwners)),
		Advice: "The released tag and the harness both vetted clean before these files were " +
			"copied, so the break is in one of them:\n    " +
			strings.Join(result.GraftedOwners, "\n    ") +
			"\n  A scenario owner that cannot compile against the released provider must not be " +
			"lent to it. Withdraw the exception or split the file -- do NOT skip it and continue, " +
			"because a skipped owner produces a receipt that looks complete for a comparison that " +
			"never happened.",
	}
	result.Layers = append(result.Layers, owners)
	if err := vetReleasedTree(root, owners); err != nil {
		return result, err
	}
	return result, nil
}

// vetReleasedTree compiles the released tree and says which layer broke it.
//
// THE OUTPUT IS HELD IN MEMORY, and that is the point rather than a detail. The
// shell wrote it to a file whose name it built from the layer LABEL -- and one
// label reads "grafting the candidate's internal/controllertest harness", so
// the redirect got spaces and a slash, failed, and the subshell returned
// non-zero before go vet ran at all. The function then reported that the
// released tree does not compile. It cost pipeline 188. A check that cannot
// tell "the thing failed" from "I could not run the check" is the defect this
// campaign exists to find, and here the seam that produced it does not exist:
// there is no filename, so there is nothing to build one from.
func vetReleasedTree(root string, layer Layer) error {
	command := exec.Command("go", "vet", "./unifi/")
	command.Dir = root
	// The AMBIENT build cache, not a scratch one. The shell gave each run a
	// fresh GOCACHE, which means every vet recompiles the whole dependency
	// graph -- three times per run, for a fact that is meant to be the cheapest
	// in the pipeline. Go's cache is content-addressed, so sharing it cannot
	// make a stale object look fresh; it only lets the released tag's objects
	// survive between runs, which is the difference between a minute and a few
	// seconds.
	command.Env = append(os.Environ(),
		"GOPROXY=off", "GOSUMDB=off", "GOVCS=*:off", "GIT_TERMINAL_PROMPT=0",
		"GOTOOLCHAIN=local")
	out, err := command.CombinedOutput()
	if err == nil {
		return nil
	}
	return fmt.Errorf("the released tree does not compile after: %s\n%s\n  %s",
		layer.Label, trimTo(string(out), 40), layer.Advice)
}

func trimTo(text string, lines int) string {
	split := strings.Split(text, "\n")
	if len(split) <= lines {
		return text
	}
	return strings.Join(split[:lines], "\n")
}

func copyFile(source, destination string) error {
	raw, err := os.ReadFile(filepath.Clean(source))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
		return err
	}
	return os.WriteFile(destination, raw, 0o600)
}

// copyTree copies a directory, refusing symlinks rather than following them. A
// symlink in the harness would pull the candidate's tree in under a released
// path, and every later comparison would be against files nobody grafted.
func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symlink; grafting it would copy something the released "+
				"tree does not name", path)
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		return copyFile(path, target)
	})
}
