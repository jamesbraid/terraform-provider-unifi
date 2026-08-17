package evidencebundle

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Member is one file to place in an evidence directory.
type Member struct {
	// Name is the name inside the directory. It must be a plain relative name:
	// the manifest is read back by Archive, which refuses anything absolute,
	// dash-leading, control-bearing or traversing.
	Name string
	// Source is where to copy it from.
	Source string
	// Executable decides the mode. The provider binary is 0755 because a
	// reviewer runs it; everything else is 0600.
	Executable bool
}

// Publish copies the members into a prepared evidence directory and writes the
// SHA256SUMS the archiver checks.
//
// IT WRITES THE MANIFEST RATHER THAN LEAVING IT TO THE CALLER, because Archive
// requires the manifest to name exactly the files present -- so a producer that
// copies files and forgets one line has built a bundle that cannot be archived,
// and finds out at the end of a run rather than at the copy.
//
// The digest lines are sha256sum's format, two spaces between hash and name,
// because that is what readManifest parses and what a reviewer will check with
// sha256sum -c.
func Publish(directory string, members []Member) error {
	if len(members) == 0 {
		return fmt.Errorf("no members: an evidence directory with only a manifest describes " +
			"nothing, and an empty bundle reads as a run that produced no evidence rather than " +
			"as a producer that was not asked for any")
	}
	ordered := append([]Member(nil), members...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })

	var manifest []byte
	for _, member := range ordered {
		if err := safeMemberName(member.Name); err != nil {
			return err
		}
		raw, err := os.ReadFile(filepath.Clean(member.Source))
		if err != nil {
			return fmt.Errorf("evidence member %s: %w", member.Name, err)
		}
		mode := os.FileMode(0o600)
		if member.Executable {
			mode = 0o755
		}
		destination := filepath.Join(directory, filepath.FromSlash(member.Name))
		if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(destination, raw, mode); err != nil {
			return err
		}
		sum := sha256.Sum256(raw)
		manifest = append(manifest, []byte(hex.EncodeToString(sum[:])+"  "+member.Name+"\n")...)
	}
	return os.WriteFile(filepath.Join(directory, checksumManifest), manifest, 0o600)
}
