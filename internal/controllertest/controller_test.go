package controllertest

import (
	"os"
	"strings"
	"testing"
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

func TestComposeControllerImageCanBePinnedAndKeptOffline(t *testing.T) {
	data, err := os.ReadFile("../../docker-compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	contents := string(data)
	for _, want := range []string{
		"${UNIFI_TEST_CONTROLLER_IMAGE:-",
		"${UNIFI_TEST_CONTROLLER_PULL_POLICY:-missing}",
	} {
		if !strings.Contains(contents, want) {
			t.Errorf("docker-compose.yaml is missing %q", want)
		}
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
