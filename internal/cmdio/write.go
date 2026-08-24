// Package cmdio owns the file primitives the evidence commands share: an
// atomic write, a strict decode, a git invocation, a file digest. The bare
// call is the majority behaviour; anything else is an explicit option, so a
// divergence is something a reviewer can see and question at the call site
// rather than something buried inside a hand-rolled writer.
package cmdio

import (
	"os"
	"path/filepath"
)

// WriteAtomic writes data to path via a temporary file in the same
// directory, then renames it into place. By default it creates the parent
// directory, fsyncs before closing, and leaves the file readable only by
// its owner; every departure from that is an explicit WriteOption.
func WriteAtomic(path string, data []byte, options ...WriteOption) error {
	settings := writeSettings{
		parentDirMode: 0o755,
		fileMode:      0o600,
		sync:          true,
		prefix:        "",
	}
	for _, option := range options {
		option(&settings)
	}

	directory := filepath.Dir(path)
	// Derived from the artifact rather than the command, so a temporary
	// orphaned by a crash still names what was being written.
	if settings.prefix == "" {
		settings.prefix = derivePrefix(path)
	}
	if settings.parentDirMode != 0 {
		if err := os.MkdirAll(directory, settings.parentDirMode); err != nil {
			return err
		}
	}

	temporary, err := os.CreateTemp(directory, settings.prefix+"*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()

	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if settings.sync {
		if err := temporary.Sync(); err != nil {
			_ = temporary.Close()
			return err
		}
	}
	if err := temporary.Chmod(settings.fileMode); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

// A WriteOption records a deliberate departure from the majority behaviour.
type WriteOption func(*writeSettings)

type writeSettings struct {
	parentDirMode os.FileMode
	fileMode      os.FileMode
	sync          bool
	prefix        string
}

// Mode sets the mode of the written file, for a call site that needs
// something other than the default 0o600.
func Mode(mode os.FileMode) WriteOption {
	return func(s *writeSettings) { s.fileMode = mode }
}

// NoParentDir skips creating the parent directory: without it, writing into
// a directory that does not exist fails rather than creating it.
func NoParentDir() WriteOption {
	return func(s *writeSettings) { s.parentDirMode = 0 }
}

// SkipSync writes without a durability barrier before the rename.
//
// Nothing calls this today; it's kept as an affordance rather than deleted.
// If it acquires a caller, that call site must say why.
func SkipSync() WriteOption {
	return func(s *writeSettings) { s.sync = false }
}

// derivePrefix names a temporary after the artifact it will become.
func derivePrefix(path string) string {
	return "." + filepath.Base(path) + "-"
}
