// Package cmdio owns the file primitives the evidence commands share.
//
// IT EXISTS BECAUSE THIRTY-FIVE COPIES OF FIVE PRIMITIVES HELD SIXTEEN
// DIFFERENT BEHAVIOURS. writeAtomic alone was written nine times in three
// variants: five create the parent directory and four do not, six chmod 0o600
// and three chmod 0o644, and one does not fsync at all. Nobody chose those
// differences; they accumulated, and nine hand-rolled writers are nine things
// nobody compares.
//
// SO THE POINT IS NOT THE LINES. It is that a divergence costs a token here.
// The majority behaviour is the bare call, and anything else has to say so --
// SkipSync() is a thing a reviewer can question, where a missing .Sync() inside
// the eighth copy of a twenty-line function is not.
//
// THIS PACKAGE OWNS THESE PRIMITIVES. If you need an atomic write, a strict
// decode, a git invocation or a file digest in cmd/, take it from here rather
// than writing a tenth one.
package cmdio

import (
	"os"
	"path/filepath"
)

// WriteAtomic writes data to path via a temporary file in the same directory,
// then renames it into place.
//
// THE BARE CALL IS THE MAJORITY BEHAVIOUR, which five of the nine original
// writers had: create the parent directory, fsync before closing, and leave the
// file readable only by its owner. Every departure is an explicit option, so the
// call site records the choice instead of burying it.
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
	// Derived from the artifact rather than the command, so a temporary orphaned
	// by a crash still names what was being written. Each original writer
	// hardcoded its own command name here; deriving it costs no option at the
	// call site and identifies the artifact instead.
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

// Mode sets the mode of the written file. Three of the nine original writers
// used 0o644 rather than 0o600; those call sites say so.
func Mode(mode os.FileMode) WriteOption {
	return func(s *writeSettings) { s.fileMode = mode }
}

// NoParentDir skips creating the parent directory, which four of the nine
// original writers did not do. It is a real difference: without it, writing
// into a directory that does not exist fails rather than creating it.
func NoParentDir() WriteOption {
	return func(s *writeSettings) { s.parentDirMode = 0 }
}

// SkipSync writes without a durability barrier before the rename.
//
// EXACTLY ONE CALLER DOES THIS and it is preserved rather than corrected,
// because "the other eight fsync" is not a decision about whether this one
// should. A call site using this must say why; if the answer turns out to be
// that a copy lost a line, that is a defect to fix deliberately and not inside
// a consolidation.
func SkipSync() WriteOption {
	return func(s *writeSettings) { s.sync = false }
}

// derivePrefix names a temporary after the artifact it will become.
func derivePrefix(path string) string {
	return "." + filepath.Base(path) + "-"
}
