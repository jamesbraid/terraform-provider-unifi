// Package exportgate answers one question: if this tree were published, what
// would go with it?
//
// It operates on a tar archive -- `git archive` output -- rather than the
// working tree, because the working tree contains files git would not ship and
// omits nothing it would. Judging the wrong artifact is how a gate passes and a
// publication still leaks.
//
// It emits findings and no receipt. A receipt would be a claim about a
// publication that has not happened, and the previous attempt at this gate
// ended by writing four literal `true` values, two of which corresponded to no
// code at all. This returns what it found, the caller exits 0 or 1, and there
// is nothing to mistake for evidence.
package exportgate

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Rule names. Findings carry one so a failure says which rule caught it and the
// self-test can assert per rule rather than "something failed".
const (
	RuleDeniedPath    = "denied-path"
	RuleDeniedTerm    = "denied-term"
	RuleSymlink       = "symlink"
	RuleBinaryFile    = "binary-file"
	RuleCommitMessage = "commit-message"
)

// Finding is one violation. Detail never contains the matched term for a hashed
// entry -- see HashedTerm.
type Finding struct {
	Rule   string
	Path   string
	Detail string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s: %s: %s", f.Rule, f.Path, f.Detail)
}

// Term is a literal string that must not appear in a published tree.
//
// PermittedPaths are globs where the term may legitimately appear, so one
// justified occurrence does not force the whole term to be dropped from the
// list. That is the difference between a denylist that survives contact with a
// real tree and one that gets weakened until it means nothing.
type Term struct {
	Value          string   `json:"value"`
	Why            string   `json:"why"`
	PermittedPaths []string `json:"permitted_paths,omitempty"`
}

// HashedTerm is a term the DENYLIST ITSELF MUST NOT SPELL.
//
// This gate guards a repository that is mirrored publicly. A denylist naming
// the private hostnames it protects would introduce those strings into the very
// tree it is checking, and into its permanent history -- the guard becoming the
// leak. Measured before this was written: the tracked tree and every commit
// message contain none of them today, so committing them in plaintext would not
// be tightening a rule, it would be creating the exposure.
//
// So sensitive terms are stored as sha256 of their lowercased form and matched
// by hashing TOKENS out of the content. That works for the shapes that actually
// matter -- hostnames, usernames, email addresses, bucket names -- because they
// are tokens. It cannot match an arbitrary substring, and that limit is real
// and stated rather than hidden: use Term for patterns that are safe to name
// and HashedTerm for the ones that are not.
//
// Label is a non-identifying description ("private forge hostname") so a
// failure is actionable without printing the secret into a CI log.
type HashedTerm struct {
	SHA256         string   `json:"sha256"`
	Label          string   `json:"label"`
	PermittedPaths []string `json:"permitted_paths,omitempty"`
}

// Policy is a DECLARATION, not a measurement. It is a human statement about
// what must never ship, and deriving it from the tree it checks would make it
// vacuous by construction -- the tree would define its own acceptability. When
// it goes stale a human re-declares it.
type Policy struct {
	DeniedPaths []string     `json:"denied_paths"`
	Terms       []Term       `json:"terms"`
	HashedTerms []HashedTerm `json:"hashed_terms"`
}

// Validate refuses a policy that would let everything through. A gate loaded
// with an empty denylist reports success on any tree, which is the outcome this
// whole exercise exists to make impossible.
func (p Policy) Validate() error {
	if len(p.DeniedPaths) == 0 {
		return fmt.Errorf("policy declares no denied paths; a gate that denies nothing " +
			"passes every tree and is decoration")
	}
	if len(p.Terms) == 0 && len(p.HashedTerms) == 0 {
		return fmt.Errorf("policy declares no terms; the path rules alone cannot see a private " +
			"identifier pasted into a shipped file")
	}
	for i, term := range p.HashedTerms {
		if len(term.SHA256) != 64 {
			return fmt.Errorf("hashed_terms[%d] (%s) has a %d-character digest, want 64",
				i, term.Label, len(term.SHA256))
		}
		if term.Label == "" {
			return fmt.Errorf("hashed_terms[%d] has no label; a failure naming only a digest "+
				"tells the reader nothing they can act on", i)
		}
	}
	for i, term := range p.Terms {
		if term.Value == "" {
			return fmt.Errorf("terms[%d] is empty, which matches every file", i)
		}
	}
	return nil
}

// HashToken returns the digest form used by HashedTerm.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(token)))
	return hex.EncodeToString(sum[:])
}

// tokenPattern extracts the shapes a hashed term can be: hostnames, usernames,
// emails, paths segments. Deliberately generous at the edges -- a token that is
// split too finely simply fails to match, and a false negative here is the
// failure mode this design already accepts and documents.
var tokenPattern = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9._@:/-]{2,}`)

// Inspect applies every archive rule and returns what it found.
//
// THE FLOOR COMES FIRST AND IS AN ERROR, NOT A FINDING. An archive with no
// regular files produces no findings, and "no findings" is what success looks
// like -- so a gate handed an empty or unreadable archive would report a clean
// tree. That exact shape has already happened in this repository more than once
// tonight, including in an instrument built to prove something else.
func Inspect(archive io.Reader, policy Policy) ([]Finding, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}

	var findings []Finding
	files := 0
	reader := tar.NewReader(archive)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading archive: %w", err)
		}
		name := path.Clean(header.Name)

		if header.Typeflag == tar.TypeSymlink || header.Typeflag == tar.TypeLink {
			findings = append(findings, Finding{RuleSymlink, name, fmt.Sprintf(
				"archive contains a link to %q; a link can point outside the published tree and "+
					"is followed by whoever unpacks it", header.Linkname)})
			continue
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		files++

		if denied, rule := deniedPath(name, policy.DeniedPaths); denied {
			findings = append(findings, Finding{RuleDeniedPath, name, fmt.Sprintf(
				"matches denied path %q", rule)})
			// Still scanned for terms below: a denied path that also leaks a
			// term should say both, so removing the path is not mistaken for
			// having handled the term.
		}

		content, err := io.ReadAll(reader)
		if err != nil {
			return nil, fmt.Errorf("reading %s from archive: %w", name, err)
		}
		findings = append(findings, inspectContent(name, content, policy)...)
	}

	if files == 0 {
		return nil, fmt.Errorf("archive contains no regular files, so every rule below would " +
			"pass having examined nothing; refusing to report a clean tree")
	}
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		return findings[i].Rule < findings[j].Rule
	})
	return findings, nil
}

func inspectContent(name string, content []byte, policy Policy) []Finding {
	var findings []Finding

	if executableMagic(content) && !anyGlobMatches(name, policy.binaryPermitted()) {
		findings = append(findings, Finding{RuleBinaryFile, name,
			"content begins with an executable header; a compiled artifact renamed to look like " +
				"source ships a binary nobody reviewed"})
	}

	lowered := strings.ToLower(string(content))
	for _, term := range policy.Terms {
		if !strings.Contains(lowered, strings.ToLower(term.Value)) {
			continue
		}
		if anyGlobMatches(name, term.PermittedPaths) {
			continue
		}
		findings = append(findings, Finding{RuleDeniedTerm, name, fmt.Sprintf(
			"contains denied term %q (%s)", term.Value, term.Why)})
	}

	if len(policy.HashedTerms) > 0 {
		for _, token := range tokenPattern.FindAllString(string(content), -1) {
			digest := HashToken(token)
			for _, term := range policy.HashedTerms {
				if digest != term.SHA256 || anyGlobMatches(name, term.PermittedPaths) {
					continue
				}
				// The matched token is NOT printed. The whole point of a hashed
				// term is that the secret is absent from the repository; echoing
				// it into a CI log would undo that at the moment of detection.
				findings = append(findings, Finding{RuleDeniedTerm, name, fmt.Sprintf(
					"contains a token matching the denied identifier %q (matched by digest; the "+
						"value is deliberately not printed)", term.Label)})
			}
		}
	}
	return findings
}

// binaryPermitted collects permitted globs for compiled content from the
// policy's terms, so fixtures that must contain binaries can be declared once.
func (p Policy) binaryPermitted() []string {
	var globs []string
	for _, term := range p.Terms {
		if term.Value == "!binary" {
			globs = append(globs, term.PermittedPaths...)
		}
	}
	return globs
}

func executableMagic(content []byte) bool {
	switch {
	case len(content) >= 4 && string(content[:4]) == "\x7fELF":
		return true
	case len(content) >= 2 && string(content[:2]) == "MZ":
		return true
	case len(content) >= 4 && (string(content[:4]) == "\xcf\xfa\xed\xfe" ||
		string(content[:4]) == "\xce\xfa\xed\xfe" ||
		string(content[:4]) == "\xca\xfe\xba\xbe"):
		return true
	}
	return false
}

func deniedPath(name string, denied []string) (bool, string) {
	for _, rule := range denied {
		if globMatches(name, rule) {
			return true, rule
		}
	}
	return false, ""
}

// globMatches treats a trailing "/" or a bare directory name as a prefix rule,
// because "deny .woodpecker/" should deny everything under it and path.Match
// does not cross separators.
func globMatches(name, rule string) bool {
	rule = strings.TrimSuffix(rule, "/")
	if name == rule || strings.HasPrefix(name, rule+"/") {
		return true
	}
	if ok, err := path.Match(rule, name); err == nil && ok {
		return true
	}
	// Allow "**/x" style by matching the base rule against any suffix segment.
	if strings.HasPrefix(rule, "**/") {
		if ok, err := path.Match(strings.TrimPrefix(rule, "**/"), path.Base(name)); err == nil && ok {
			return true
		}
	}
	return false
}

func anyGlobMatches(name string, rules []string) bool {
	for _, rule := range rules {
		if globMatches(name, rule) {
			return true
		}
	}
	return false
}

// InspectCommitMessages applies the term rules to commit messages.
//
// A tree can be clean while the history is not: a hostname removed from a file
// in a later commit is still in the message of the commit that removed it, and
// publishing a repository publishes both.
func InspectCommitMessages(messages map[string]string, policy Policy) ([]Finding, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, fmt.Errorf("no commit messages were supplied, so this rule would pass having " +
			"read nothing; the published range must resolve to at least one commit")
	}

	var findings []Finding
	for commit, message := range messages {
		lowered := strings.ToLower(message)
		for _, term := range policy.Terms {
			if strings.Contains(lowered, strings.ToLower(term.Value)) {
				findings = append(findings, Finding{RuleCommitMessage, commit, fmt.Sprintf(
					"commit message contains denied term %q (%s)", term.Value, term.Why)})
			}
		}
		for _, token := range tokenPattern.FindAllString(message, -1) {
			digest := HashToken(token)
			for _, term := range policy.HashedTerms {
				if digest == term.SHA256 {
					findings = append(findings, Finding{RuleCommitMessage, commit, fmt.Sprintf(
						"commit message contains a token matching the denied identifier %q "+
							"(matched by digest; the value is deliberately not printed)", term.Label)})
				}
			}
		}
	}
	sort.SliceStable(findings, func(i, j int) bool { return findings[i].Path < findings[j].Path })
	return findings, nil
}
