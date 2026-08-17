// Package evidencebundle prepares and archives the evidence directory a
// qualification run writes its artifacts into.
//
// It replaces .woodpecker/scripts/m1-evidence-lib.sh, which was sourced by five
// callers across three separate rewrites -- so it could not travel with any one
// of them without stranding the others.
//
// EVERY CHECK HERE IS ABOUT NOT SHIPPING SOMETHING THAT IS NOT THE EVIDENCE.
// The directory must be empty and outside the repository, so a run cannot
// silently include files it did not produce or overwrite the tree it measured.
// The archive must name exactly the members the checksum manifest names,
// neither more nor fewer, with every digest verified -- an archive holding one
// extra file is an archive whose contents nobody vouched for.
package evidencebundle

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// checksumManifest is the file naming what the bundle is supposed to contain.
const checksumManifest = "SHA256SUMS"

// Prepare resolves the evidence directory and refuses one that could contaminate
// the run, returning the physical path the caller should write into.
//
// It returns the RESOLVED path rather than the requested one because every
// later check -- containment, and the archive not living inside the bundle --
// is meaningless against a path that still contains symlinks.
func Prepare(requested, repositoryRoot string) (string, error) {
	// Lstat, not Stat: a symlink to a valid directory passes every other check
	// here while writing somewhere nobody named. The shell rejected the
	// terminal symlink and this keeps that exactly.
	if info, err := os.Lstat(requested); err == nil {
		if info.Mode()&fs.ModeSymlink != 0 {
			return "", fmt.Errorf("evidence output directory %s is a symlink; "+
				"the run would write somewhere its own path does not name", requested)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("evidence output %s exists and is not a directory", requested)
		}
	}

	if err := os.MkdirAll(requested, 0o750); err != nil {
		return "", fmt.Errorf("create evidence output directory: %w", err)
	}
	resolvedDirectory, err := filepath.EvalSymlinks(requested)
	if err != nil {
		return "", fmt.Errorf("resolve evidence output directory: %w", err)
	}
	resolvedRepository, err := filepath.EvalSymlinks(repositoryRoot)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}

	if resolvedDirectory == resolvedRepository ||
		strings.HasPrefix(resolvedDirectory, resolvedRepository+string(os.PathSeparator)) {
		return "", fmt.Errorf("evidence output %s is inside the provider repository %s. "+
			"Evidence written into the tree it describes changes that tree, and every "+
			"downstream check that compares the tree against a receipt then compares it "+
			"against itself", resolvedDirectory, resolvedRepository)
	}

	entries, err := os.ReadDir(resolvedDirectory)
	if err != nil {
		return "", fmt.Errorf("read evidence output directory: %w", err)
	}
	if len(entries) > 0 {
		return "", fmt.Errorf("evidence output directory %s is not empty (%d entr(y/ies)). "+
			"A leftover file from an earlier run would be archived as though this run "+
			"produced it", resolvedDirectory, len(entries))
	}
	return resolvedDirectory, nil
}

// Archive verifies the bundle against its own checksum manifest and writes it
// out as a gzipped tar.
//
// The archive is removed if anything fails, so a rejected bundle never leaves a
// partial artifact behind for someone to find and trust.
func Archive(sourceDirectory, archivePath string) error {
	resolvedDirectory, err := filepath.EvalSymlinks(sourceDirectory)
	if err != nil {
		return fmt.Errorf("resolve evidence directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o750); err != nil {
		return fmt.Errorf("create archive directory: %w", err)
	}
	archiveParent, err := filepath.EvalSymlinks(filepath.Dir(archivePath))
	if err != nil {
		return fmt.Errorf("resolve archive directory: %w", err)
	}
	resolvedArchive := filepath.Join(archiveParent, filepath.Base(archivePath))

	if strings.HasPrefix(resolvedArchive, resolvedDirectory+string(os.PathSeparator)) {
		return fmt.Errorf("archive %s is inside the evidence directory %s, so it would be "+
			"a member of itself", resolvedArchive, resolvedDirectory)
	}
	// Lstat so a dangling symlink counts as present. Refusing to overwrite is
	// what keeps a failed run from being mistaken for the previous good one.
	if _, err := os.Lstat(resolvedArchive); err == nil {
		return fmt.Errorf("archive %s already exists", resolvedArchive)
	}

	expected, digests, err := readManifest(resolvedDirectory)
	if err != nil {
		return err
	}
	actual, err := walkMembers(resolvedDirectory)
	if err != nil {
		return err
	}
	if err := sameMembers(expected, actual); err != nil {
		return err
	}
	if err := verifyDigests(resolvedDirectory, digests); err != nil {
		return err
	}

	if err := writeArchive(resolvedDirectory, resolvedArchive, expected); err != nil {
		_ = os.Remove(resolvedArchive)
		return err
	}
	// Read back what was just written. The members are known here, so this is
	// not asking what the archive contains -- it is asking whether the file on
	// disk is a readable archive of them, which a truncated or half-written one
	// is not.
	written, err := archiveMembers(resolvedArchive)
	if err != nil {
		_ = os.Remove(resolvedArchive)
		return err
	}
	if err := sameMembers(expected, written); err != nil {
		_ = os.Remove(resolvedArchive)
		return fmt.Errorf("the archive that was written does not hold what it was given: %w", err)
	}
	return nil
}

// readManifest returns the member names the manifest declares, including the
// manifest itself, and each member's expected digest.
//
// Parsing is strict. The shell stripped a digest prefix with sed and left any
// line that did not match untouched, so a malformed line silently became a
// member name -- which then failed a later check under a message about
// something else.
func readManifest(root string) ([]string, map[string]string, error) {
	path := filepath.Join(root, checksumManifest)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, fmt.Errorf("evidence checksum manifest %s is missing: %w", checksumManifest, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("evidence checksum manifest %s is not a regular file", checksumManifest)
	}
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, nil, err
	}

	digests := map[string]string{}
	members := []string{checksumManifest}
	for number, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if line == "" {
			continue
		}
		if len(line) < 67 {
			return nil, nil, fmt.Errorf("%s line %d is too short to be a checksum entry", checksumManifest, number+1)
		}
		digest, separator, name := line[:64], line[64:66], line[66:]
		if _, err := hex.DecodeString(digest); err != nil {
			return nil, nil, fmt.Errorf("%s line %d does not start with a sha256: %w", checksumManifest, number+1, err)
		}
		if separator != "  " && separator != " *" {
			return nil, nil, fmt.Errorf("%s line %d has no sha256sum separator after the digest", checksumManifest, number+1)
		}
		if err := safeMemberName(name); err != nil {
			return nil, nil, fmt.Errorf("%s line %d: %w", checksumManifest, number+1, err)
		}
		digests[name] = digest
		members = append(members, name)
	}
	sort.Strings(members)
	return dedupe(members), digests, nil
}

// safeMemberName rejects names that would make the archive extract somewhere
// other than where the extractor expects.
func safeMemberName(name string) error {
	if name == "" {
		return fmt.Errorf("empty member name")
	}
	if strings.HasPrefix(name, "/") {
		return fmt.Errorf("member %q is an absolute path", name)
	}
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("member %q begins with a dash and would be read as an option", name)
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return fmt.Errorf("member %q contains a control character", name)
		}
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return fmt.Errorf("member %q traverses out of the evidence directory", name)
		}
	}
	return nil
}

// walkMembers lists every non-directory file under root, and refuses a symlink
// anywhere -- including a symlinked directory, which would otherwise pull in
// files from outside the bundle while every name still looked local.
func walkMembers(root string) ([]string, error) {
	var members []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("evidence directory contains a symlink at %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		members = append(members, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(members)
	return members, nil
}

// sameMembers is SET EQUALITY in both directions, which is the whole point.
//
// A file present and unmanifested is something nobody vouched for; a file
// manifested and absent is a claim about evidence that does not exist. Checking
// only one direction would let one of those through, and which one depends on
// which way round the check was written.
func sameMembers(expected, actual []string) error {
	present := map[string]bool{}
	for _, name := range actual {
		present[name] = true
	}
	declared := map[string]bool{}
	for _, name := range expected {
		declared[name] = true
	}
	var unmanifested, missing []string
	for _, name := range actual {
		if !declared[name] {
			unmanifested = append(unmanifested, name)
		}
	}
	for _, name := range expected {
		if !present[name] {
			missing = append(missing, name)
		}
	}
	if len(unmanifested) == 0 && len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("evidence directory and %s disagree: %d file(s) present and unmanifested %v, "+
		"%d manifested and absent %v", checksumManifest, len(unmanifested), unmanifested,
		len(missing), missing)
}

func verifyDigests(root string, digests map[string]string) error {
	var wrong []string
	for name, want := range digests {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return err
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != want {
			wrong = append(wrong, name)
		}
	}
	if len(wrong) == 0 {
		return nil
	}
	sort.Strings(wrong)
	return fmt.Errorf("%d evidence file(s) do not match their recorded digest: %v", len(wrong), wrong)
}

func writeArchive(root, destination string, members []string) error {
	handle, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer handle.Close()

	compressor := gzip.NewWriter(handle)
	writer := tar.NewWriter(compressor)
	for _, name := range members {
		path := filepath.Join(root, filepath.FromSlash(name))
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = name
		// The names come from the manifest, not from the filesystem walk, so
		// the archive records exactly what was vouched for.
		header.Uid, header.Gid, header.Uname, header.Gname = 0, 0, "", ""
		if err := writer.WriteHeader(header); err != nil {
			return err
		}
		source, err := os.Open(filepath.Clean(path))
		if err != nil {
			return err
		}
		if _, err := io.Copy(writer, source); err != nil {
			source.Close()
			return err
		}
		if err := source.Close(); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	if err := compressor.Close(); err != nil {
		return err
	}
	return handle.Close()
}

func archiveMembers(path string) ([]string, error) {
	handle, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	decompressor, err := gzip.NewReader(handle)
	if err != nil {
		return nil, fmt.Errorf("read back %s: %w", path, err)
	}
	defer decompressor.Close()

	var members []string
	reader := tar.NewReader(decompressor)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read back %s: %w", path, err)
		}
		members = append(members, header.Name)
	}
	sort.Strings(members)
	return members, nil
}

func dedupe(sorted []string) []string {
	out := sorted[:0]
	for i, name := range sorted {
		if i == 0 || name != sorted[i-1] {
			out = append(out, name)
		}
	}
	return out
}
