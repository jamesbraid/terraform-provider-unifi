package controllertest

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/ubiquiti-community/go-unifi/unifi"
)

func TestRemoveControllerImagesIsOptIn(t *testing.T) {
	t.Setenv(envRemoveControllerImages, "")
	if removeControllerImages() {
		t.Fatal("controller images are removed without an explicit opt-in")
	}

	t.Setenv(envRemoveControllerImages, "true")
	if !removeControllerImages() {
		t.Fatal("controller images are preserved after explicit removal opt-in")
	}
}

func TestComposeControllerImageComesFromTheHarnessNotALiteral(t *testing.T) {
	data, err := os.ReadFile("../../docker-compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	contents := string(data)
	if !strings.Contains(contents, "${UNIFI_TEST_CONTROLLER_IMAGE:?") {
		t.Error("docker-compose.yaml must require UNIFI_TEST_CONTROLLER_IMAGE (the harness sets it from unifi.UnifiVersion); a :- default would let a literal version drift from the SDK")
	}
	if regexp.MustCompile(`unifi-network:[0-9]+\.[0-9]+\.[0-9]+`).MatchString(contents) {
		t.Error("docker-compose.yaml names a controller version literally; the SDK owns that fact")
	}
	if !strings.Contains(contents, "${UNIFI_TEST_CONTROLLER_PULL_POLICY:-missing}") {
		t.Error("docker-compose.yaml must keep the offline-friendly pull policy")
	}
}

func TestDefaultControllerImageTracksTheSDK(t *testing.T) {
	want := "ghcr.io/jamesbraid/unifi-network:" + unifi.UnifiVersion + "-sim"
	if got := DefaultControllerImage(); got != want {
		t.Errorf("DefaultControllerImage() = %q, want %q", got, want)
	}
	t.Setenv("UNIFI_TEST_CONTROLLER_IMAGE", "example.test/pinned:1")
	if got := controllerImage(); got != "example.test/pinned:1" {
		t.Errorf("controllerImage() = %q; an explicit UNIFI_TEST_CONTROLLER_IMAGE must win", got)
	}
	t.Setenv("UNIFI_TEST_CONTROLLER_IMAGE", "")
	if got := controllerImage(); got != want {
		t.Errorf("controllerImage() = %q, want the SDK-derived default when unset", got)
	}
}

func TestComposePublishesControllerOnEphemeralHostPort(t *testing.T) {
	data, err := os.ReadFile("../../docker-compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	contents := string(data)
	if !strings.Contains(contents, `- "0:8443"`) {
		t.Fatal("docker-compose.yaml does not allocate an ephemeral API port")
	}
	if strings.Contains(contents, "- 8443:8443") {
		t.Fatal("docker-compose.yaml still reserves host port 8443")
	}
}
