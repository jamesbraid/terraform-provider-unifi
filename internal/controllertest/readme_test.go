package controllertest

import (
	"os"
	"strings"
	"testing"

	"github.com/ubiquiti-community/go-unifi/unifi"
)

// The README states the controller version in prose, which no generator can
// write; this keeps the sentence from lying after a bump.
func TestREADMEStatesTheLockedControllerVersion(t *testing.T) {
	data, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	want := "Acceptance tests run against UniFi Network " + unifi.UnifiVersion + "."
	if !strings.Contains(string(data), want) {
		t.Errorf("README.md does not contain %q; update the sentence to the version the SDK is locked to", want)
	}
}
