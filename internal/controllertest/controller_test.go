package controllertest

import (
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/ubiquiti-community/go-unifi/unifi"
)

// fakeLogger records every Printf call so tests can assert on what a run
// would have logged.
type fakeLogger struct {
	lines []string
}

func (f *fakeLogger) Printf(format string, v ...any) {
	f.lines = append(f.lines, fmt.Sprintf(format, v...))
}

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

func TestEffectiveControllerImageHonoursTheOverride(t *testing.T) {
	t.Setenv("UNIFI_TEST_CONTROLLER_IMAGE", "example.invalid/unifi:pinned")
	if got := effectiveControllerImage(); got != "example.invalid/unifi:pinned" {
		t.Errorf("with an override set, effectiveControllerImage() = %q, want the override", got)
	}
	t.Setenv("UNIFI_TEST_CONTROLLER_IMAGE", "")
	if got, want := effectiveControllerImage(), DefaultControllerImage(); got != want {
		t.Errorf("with no override, effectiveControllerImage() = %q, want %q", got, want)
	}
}

func TestDefaultControllerImageTracksTheSDK(t *testing.T) {
	want := "ghcr.io/jamesbraid/unifi-network:" + unifi.UnifiVersion + "-sim"
	if got := DefaultControllerImage(); got != want {
		t.Errorf("DefaultControllerImage() = %q, want %q", got, want)
	}
}

// TestComposeEnv guards the collision fixed after Task 5 shipped: WithOsEnv
// already forwards an exported UNIFI_TEST_CONTROLLER_IMAGE into compose's
// environment, and WithEnv errors if asked to set a key already there, so
// composeEnv must back off rather than restate the override.
func TestComposeEnv(t *testing.T) {
	want := map[string]string{"UNIFI_TEST_CONTROLLER_IMAGE": DefaultControllerImage()}
	cases := map[string]struct {
		envValue string
		want     map[string]string
	}{
		"unset fills the default for WithOsEnv to carry":                 {envValue: "", want: want},
		"an explicit override is left for WithOsEnv alone, not restated": {envValue: "example.test/pinned:1", want: nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("UNIFI_TEST_CONTROLLER_IMAGE", tc.envValue)
			if got := composeEnv(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("composeEnv() = %#v, want %#v", got, tc.want)
			}
		})
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

// TestVerifyControllerVersion guards the rider a sibling project's incident
// motivated: a container published under the right tag is not proof it runs
// the release that tag names. The image tag is derived from
// unifi.UnifiVersion and cannot drift on its own, but nothing before this
// checked that the booted controller's own report agrees with it.
func TestVerifyControllerVersion(t *testing.T) {
	t.Run("reported version matches the SDK pin", func(t *testing.T) {
		logger := &fakeLogger{}
		if err := verifyControllerVersion(logger, unifi.UnifiVersion); err != nil {
			t.Fatalf("verifyControllerVersion() = %v, want nil for a matching version", err)
		}
		if len(logger.lines) != 1 || !strings.Contains(logger.lines[0], unifi.UnifiVersion) {
			t.Errorf("verifyControllerVersion() logged %v, want one line naming %q", logger.lines, unifi.UnifiVersion)
		}
	})

	t.Run("reported version disagrees with the SDK pin", func(t *testing.T) {
		logger := &fakeLogger{}
		const drifted = "1.2.3"
		err := verifyControllerVersion(logger, drifted)
		if err == nil {
			t.Fatal("verifyControllerVersion() = nil, want an error for a mismatched version")
		}
		if !strings.Contains(err.Error(), drifted) || !strings.Contains(err.Error(), unifi.UnifiVersion) {
			t.Errorf("verifyControllerVersion() error = %q, want it to name both %q and %q", err, drifted, unifi.UnifiVersion)
		}
	})

	t.Run("empty report counts as a mismatch", func(t *testing.T) {
		logger := &fakeLogger{}
		if err := verifyControllerVersion(logger, ""); err == nil {
			t.Fatal("verifyControllerVersion() = nil, want an error when the controller reported no version")
		}
	})
}
