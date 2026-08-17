package evidencebundle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPublishedEvidenceCanBeArchived is the assertion that makes Publish worth
// having as a function rather than three copies.
//
// Archive requires the manifest to name exactly the files present. A producer
// that copies files and writes its own manifest can miss a line, and it finds
// out at the END of a run rather than at the copy. Publishing and archiving in
// one test is the only way to know the two agree.
func TestPublishedEvidenceCanBeArchived(t *testing.T) {
	root := t.TempDir()
	source := mkdirIn(t, root, "source")
	write(t, filepath.Join(source, "provider"), "#!/bin/sh\n")
	write(t, filepath.Join(source, "terraform.json"), `{"a":1}`)
	write(t, filepath.Join(source, "tofu.json"), `{"b":2}`)

	directory, err := Prepare(filepath.Join(root, "evidence"), mkdirIn(t, root, "repository"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Publish(directory, []Member{
		{Name: "terraform-provider-unifi", Source: filepath.Join(source, "provider"), Executable: true},
		{Name: "terraform-schema.json", Source: filepath.Join(source, "terraform.json")},
		{Name: "tofu-schema.json", Source: filepath.Join(source, "tofu.json")},
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if err := Archive(directory, filepath.Join(root, "evidence.tar.gz")); err != nil {
		t.Fatalf("the directory Publish wrote could not be archived: %v.\n"+
			"Publish writes the manifest Archive checks; if they disagree, a run discovers it "+
			"an hour after the copy", err)
	}
	members, err := archiveMembers(filepath.Join(root, "evidence.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 4 {
		t.Fatalf("archive holds %v, want the three members and SHA256SUMS", members)
	}
}

// TestTheProviderBinaryStaysExecutable. A reviewer runs it; a 0600 copy is an
// artifact they have to chmod before it is what it claims to be.
func TestTheProviderBinaryStaysExecutable(t *testing.T) {
	root := t.TempDir()
	source := mkdirIn(t, root, "source")
	write(t, filepath.Join(source, "provider"), "#!/bin/sh\n")
	write(t, filepath.Join(source, "schema.json"), "{}")

	directory, err := Prepare(filepath.Join(root, "evidence"), mkdirIn(t, root, "repository"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Publish(directory, []Member{
		{Name: "terraform-provider-unifi", Source: filepath.Join(source, "provider"), Executable: true},
		{Name: "terraform-schema.json", Source: filepath.Join(source, "schema.json")},
	}); err != nil {
		t.Fatal(err)
	}

	binary, err := os.Stat(filepath.Join(directory, "terraform-provider-unifi"))
	if err != nil {
		t.Fatal(err)
	}
	if binary.Mode()&0o111 == 0 {
		t.Fatalf("the provider binary is %v; a reviewer has to chmod it before it is what the "+
			"bundle says it is", binary.Mode())
	}
	schema, err := os.Stat(filepath.Join(directory, "terraform-schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if schema.Mode()&0o111 != 0 {
		t.Fatalf("a schema file is executable (%v)", schema.Mode())
	}
}

// TestPublishRefusesToDescribeNothing. An empty bundle reads as a run that
// produced no evidence rather than as a producer nobody asked for any from.
func TestPublishRefusesToDescribeNothing(t *testing.T) {
	root := t.TempDir()
	directory, err := Prepare(filepath.Join(root, "evidence"), mkdirIn(t, root, "repository"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Publish(directory, nil); err == nil {
		t.Fatal("an evidence directory containing only a manifest was published")
	}
}

// TestPublishRefusesAnUnsafeMemberName reuses the archiver's own rule rather
// than a second copy of it. A name that Archive would reject must not be
// written in the first place: the bundle would be unarchivable and the refusal
// would name the manifest rather than the producer that wrote it.
func TestPublishRefusesAnUnsafeMemberName(t *testing.T) {
	root := t.TempDir()
	source := mkdirIn(t, root, "source")
	write(t, filepath.Join(source, "x"), "x")
	directory, err := Prepare(filepath.Join(root, "evidence"), mkdirIn(t, root, "repository"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"/absolute", "-dashed", "../escaping"} {
		if err := Publish(directory, []Member{{Name: name, Source: filepath.Join(source, "x")}}); err == nil {
			t.Fatalf("published a member called %q", name)
		} else if !strings.Contains(err.Error(), "member") {
			t.Fatalf("refused %q with %v, which does not name the member", name, err)
		}
	}
}
