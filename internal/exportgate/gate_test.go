package exportgate

import (
	"archive/tar"
	"bytes"
	"strings"
	"testing"
)

// The self-test IS the deliverable. A gate nobody has watched fail is a gate
// nobody can distinguish from a decoration, and the previous attempt at this
// one emitted four literal `true` values, two backed by no code at all.
//
// So every rule below gets a tree containing EXACTLY ONE violation of it, and
// is asserted to fail BY RULE NAME rather than by "something failed". Asserting
// only that a bad tree fails would pass for a gate that rejects every tree,
// which is why TestCleanTreeProducesNoFindings exists and why each case also
// checks that no OTHER rule fired.

func archiveOf(t *testing.T, entries ...tar.Header) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	for _, header := range entries {
		body := []byte(header.Uname) // carrier for content; see file()
		header.Uname = ""
		header.Size = int64(len(body))
		if header.Typeflag == 0 {
			header.Typeflag = tar.TypeReg
		}
		if header.Typeflag == tar.TypeSymlink {
			header.Size = 0
			body = nil
		}
		if err := writer.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		if len(body) > 0 {
			if _, err := writer.Write(body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// file builds a regular entry. Content rides in Uname purely so the table below
// stays readable; archiveOf moves it into the body.
func file(name, content string) tar.Header {
	return tar.Header{Name: name, Uname: content, Mode: 0o644, Typeflag: tar.TypeReg}
}

func symlink(name, target string) tar.Header {
	return tar.Header{Name: name, Linkname: target, Mode: 0o777, Typeflag: tar.TypeSymlink}
}

const secretHost = "private-forge.example.invalid"

func testPolicy() Policy {
	return Policy{
		DeniedPaths: []string{".woodpecker/", "build/", "provider-codegen/bootstrap/"},
		Terms: []Term{
			{Value: "op://", Why: "1Password reference", PermittedPaths: []string{"docs/secrets.md"}},
			{Value: "!binary", Why: "permitted-binary carrier", PermittedPaths: []string{"testdata/*.bin"}},
		},
		HashedTerms: []HashedTerm{
			{SHA256: HashToken(secretHost), Label: "private forge hostname"},
		},
	}
}

// cleanTree is the baseline every case below mutates by exactly one thing.
func cleanTree(t *testing.T) []byte {
	return archiveOf(t,
		file("main.go", "package main\n"),
		file("README.md", "# provider\n"),
		file("unifi/network_resource.go", "package unifi\n"),
	)
}

// TestCleanTreeProducesNoFindings is the control that makes every other case in
// this file mean something. Without it, a gate that flagged every file would
// satisfy all the must-fail assertions below.
func TestCleanTreeProducesNoFindings(t *testing.T) {
	findings, err := Inspect(bytes.NewReader(cleanTree(t)), testPolicy())
	if err != nil {
		t.Fatalf("clean tree: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("clean tree produced %d finding(s), so every must-fail case below would pass "+
			"for the wrong reason: %v", len(findings), findings)
	}
}

func TestEachRuleFailsOnExactlyItsOwnViolation(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		entries   []tar.Header
		wantRule  string
		wantPath  string
		wantInMsg string
	}{
		{
			name: "denied path",
			entries: []tar.Header{
				file("main.go", "package main\n"),
				file(".woodpecker/catalog-controller-differential.yml", "steps: []\n"),
			},
			wantRule:  RuleDeniedPath,
			wantPath:  ".woodpecker/catalog-controller-differential.yml",
			wantInMsg: "matches denied path",
		},
		{
			name: "denied term in a shipped file",
			entries: []tar.Header{
				file("main.go", "package main\n"),
				file("docs/setup.md", "read the token from op://Vault/item/field\n"),
			},
			wantRule:  RuleDeniedTerm,
			wantPath:  "docs/setup.md",
			wantInMsg: `denied term "op://"`,
		},
		{
			name: "hashed identifier in a shipped file",
			entries: []tar.Header{
				file("main.go", "package main\n"),
				file("docs/ci.md", "the runner lives at "+secretHost+" today\n"),
			},
			wantRule:  RuleDeniedTerm,
			wantPath:  "docs/ci.md",
			wantInMsg: "private forge hostname",
		},
		{
			name: "symlink",
			entries: []tar.Header{
				file("main.go", "package main\n"),
				symlink("config.json", "../../../etc/passwd"),
			},
			wantRule:  RuleSymlink,
			wantPath:  "config.json",
			wantInMsg: "link to",
		},
		{
			name: "compiled artifact renamed as source",
			entries: []tar.Header{
				file("main.go", "package main\n"),
				file("unifi/helper.go", "\x7fELF\x02\x01\x01\x00 not actually go source"),
			},
			wantRule:  RuleBinaryFile,
			wantPath:  "unifi/helper.go",
			wantInMsg: "executable header",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			findings, err := Inspect(bytes.NewReader(archiveOf(t, testCase.entries...)), testPolicy())
			if err != nil {
				t.Fatalf("inspect: %v", err)
			}
			if len(findings) == 0 {
				t.Fatalf("a tree containing exactly one %s violation produced no findings",
					testCase.wantRule)
			}

			var matched bool
			for _, finding := range findings {
				if finding.Rule == testCase.wantRule && finding.Path == testCase.wantPath {
					matched = true
					if !strings.Contains(finding.Detail, testCase.wantInMsg) {
						t.Errorf("detail %q does not explain the violation (want %q)",
							finding.Detail, testCase.wantInMsg)
					}
				}
			}
			if !matched {
				t.Fatalf("no %s finding for %s; got %v",
					testCase.wantRule, testCase.wantPath, findings)
			}
			// EXACTLY its own violation: a rule that fires on unrelated files
			// would satisfy the assertion above while being useless.
			for _, finding := range findings {
				if finding.Path == "main.go" {
					t.Errorf("rule %s also flagged the innocent file main.go: %v",
						testCase.wantRule, finding)
				}
			}
		})
	}
}

// TestPermittedPathsAreHonoured is the other half of a usable denylist: a term
// that legitimately appears somewhere must be declarable there, or the term
// gets deleted from the list entirely and stops protecting everywhere else.
func TestPermittedPathsAreHonoured(t *testing.T) {
	archive := archiveOf(t,
		file("main.go", "package main\n"),
		file("docs/secrets.md", "credentials are referenced as op://Vault/item/field\n"),
	)
	findings, err := Inspect(bytes.NewReader(archive), testPolicy())
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range findings {
		if finding.Path == "docs/secrets.md" {
			t.Errorf("term fired inside its own permitted path: %v", finding)
		}
	}
}

// TestEmptyArchiveIsAnErrorNotACleanTree is the floor, and it is the one this
// repository has got wrong most often: a rule set applied to nothing produces
// no findings, and no findings is what success looks like.
func TestEmptyArchiveIsAnErrorNotACleanTree(t *testing.T) {
	findings, err := Inspect(bytes.NewReader(archiveOf(t)), testPolicy())
	if err == nil {
		t.Fatalf("an empty archive was reported as clean with %d finding(s); a gate that examined "+
			"nothing must not be indistinguishable from one that found nothing", len(findings))
	}
	if !strings.Contains(err.Error(), "no regular files") {
		t.Errorf("error should say the archive was empty, got: %v", err)
	}
}

// TestPolicyWithNothingDeclaredIsRefused stops the gate being defanged by
// emptying its declaration rather than by changing its code.
func TestPolicyWithNothingDeclaredIsRefused(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		policy  Policy
		wantSub string
	}{
		{"no denied paths", Policy{Terms: []Term{{Value: "x", Why: "y"}}}, "no denied paths"},
		{"no terms", Policy{DeniedPaths: []string{"build/"}}, "no terms"},
		{
			"hashed term with no label",
			Policy{DeniedPaths: []string{"build/"}, HashedTerms: []HashedTerm{{SHA256: strings.Repeat("a", 64)}}},
			"no label",
		},
		{
			"hashed term with a truncated digest",
			Policy{DeniedPaths: []string{"build/"}, HashedTerms: []HashedTerm{{SHA256: "abc", Label: "x"}}},
			"want 64",
		},
		{
			"empty term matches everything",
			Policy{DeniedPaths: []string{"build/"}, Terms: []Term{{Value: "", Why: "y"}}},
			"matches every file",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := Inspect(bytes.NewReader(cleanTree(t)), testCase.policy)
			if err == nil {
				t.Fatal("a policy declaring nothing was accepted")
			}
			if !strings.Contains(err.Error(), testCase.wantSub) {
				t.Errorf("error %q does not mention %q", err, testCase.wantSub)
			}
		})
	}
}

// TestCommitMessagesAreScanned covers the half a tree scan cannot reach: a
// hostname deleted from a file is still in the message of the commit that
// deleted it.
func TestCommitMessagesAreScanned(t *testing.T) {
	findings, err := InspectCommitMessages(map[string]string{
		"abc1234": "provider: fix the network resource\n",
		"def5678": "ci: point the runner at " + secretHost + "\n",
	}, testPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("want exactly 1 finding, got %d: %v", len(findings), findings)
	}
	if findings[0].Path != "def5678" || findings[0].Rule != RuleCommitMessage {
		t.Errorf("finding = %+v, want commit-message on def5678", findings[0])
	}
	// The secret must not be echoed by the thing that detects it.
	if strings.Contains(findings[0].Detail, secretHost) {
		t.Errorf("the finding printed the identifier it is protecting: %q", findings[0].Detail)
	}
}

// TestNoCommitMessagesIsAnErrorNotAPass is the same floor as the empty archive,
// on the other input. A published range that resolves to nothing would
// otherwise report the history clean.
func TestNoCommitMessagesIsAnErrorNotAPass(t *testing.T) {
	_, err := InspectCommitMessages(map[string]string{}, testPolicy())
	if err == nil {
		t.Fatal("an empty commit range was reported as clean")
	}
}

// TestHashedTermsDoNotAppearInThePolicy is the property the hashed form exists
// for: the declaration must not spell what it guards, or committing the gate
// introduces the exposure into a mirrored repository.
func TestHashedTermsDoNotAppearInThePolicy(t *testing.T) {
	policy := testPolicy()
	for _, term := range policy.HashedTerms {
		if strings.Contains(strings.ToLower(term.Label), "example.invalid") {
			t.Errorf("label %q leaks the value it stands for", term.Label)
		}
		if len(term.SHA256) != 64 {
			t.Errorf("digest for %q is not a full sha256", term.Label)
		}
	}
	if HashToken(secretHost) == secretHost {
		t.Fatal("HashToken is returning its input; the digest would be the secret")
	}
	if HashToken("ABC") != HashToken("abc") {
		t.Error("HashToken is case-sensitive, so a capitalised hostname would slip past")
	}
}
