package controllertest

import (
	"reflect"
	"strings"
	"testing"
)

func TestHerderChildEnv(t *testing.T) {
	t.Run("drops the reaper switch the harness sets for Compose", func(t *testing.T) {
		got := herderChildEnv([]string{
			"PATH=/usr/bin",
			"TESTCONTAINERS_RYUK_DISABLED=true",
			"DOCKER_HOST=unix:///var/run/docker.sock",
		})
		want := []string{"PATH=/usr/bin", "DOCKER_HOST=unix:///var/run/docker.sock"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("herderChildEnv() = %q, want %q", got, want)
		}
	})

	t.Run("drops it whatever its value", func(t *testing.T) {
		got := herderChildEnv([]string{"TESTCONTAINERS_RYUK_DISABLED=false"})
		if len(got) != 0 {
			t.Errorf("herderChildEnv() = %q, want empty", got)
		}
	})

	t.Run("keeps variables that merely share the prefix", func(t *testing.T) {
		environ := []string{
			"TESTCONTAINERS_RYUK_DISABLED_EXTRA=1",
			"TESTCONTAINERS_RYUK_CONTAINER_IMAGE=ryuk",
		}
		got := herderChildEnv(environ)
		if !reflect.DeepEqual(got, environ) {
			t.Errorf("herderChildEnv() = %q, want %q", got, environ)
		}
	})

	t.Run("gives the reaper the in-VM socket under Colima", func(t *testing.T) {
		got := herderChildEnv([]string{
			"DOCKER_HOST=unix:///Users/someone/.colima/default/docker.sock",
		})
		want := []string{
			"DOCKER_HOST=unix:///Users/someone/.colima/default/docker.sock",
			"TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock",
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("herderChildEnv() = %q, want %q", got, want)
		}
	})

	t.Run("leaves an override the caller already set", func(t *testing.T) {
		environ := []string{
			"DOCKER_HOST=unix:///Users/someone/.colima/default/docker.sock",
			"TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/run/user/501/docker.sock",
		}
		got := herderChildEnv(environ)
		if !reflect.DeepEqual(got, environ) {
			t.Errorf("herderChildEnv() = %q, want %q", got, environ)
		}
	})

	t.Run("adds nothing on a runtime that needs no override", func(t *testing.T) {
		environ := []string{"DOCKER_HOST=unix:///var/run/docker.sock"}
		got := herderChildEnv(environ)
		if !reflect.DeepEqual(got, environ) {
			t.Errorf("herderChildEnv() = %q, want %q", got, environ)
		}
	})
}

func TestInformEndpoint(t *testing.T) {
	t.Run("single attachment yields the canonical IPv4 inform URL", func(t *testing.T) {
		network, informURL, err := informEndpoint(map[string]string{"acc_default": "172.28.0.2"})
		if err != nil {
			t.Fatalf("informEndpoint() error = %v", err)
		}
		if network != "acc_default" {
			t.Errorf("network = %q, want acc_default", network)
		}
		if informURL != "http://172.28.0.2:8080/inform" {
			t.Errorf("informURL = %q, want http://172.28.0.2:8080/inform", informURL)
		}
	})

	// A second attachment means the controller has more than one address to
	// choose from, and the one it advertises after adoption need not be the
	// one the devices were pointed at. Devices would adopt and then stall, so
	// the ambiguity is refused here rather than debugged there.
	t.Run("two attachments are ambiguous", func(t *testing.T) {
		_, _, err := informEndpoint(map[string]string{
			"acc_default": "172.28.0.2",
			"acc_extra":   "172.29.0.2",
		})
		if err == nil {
			t.Fatal("informEndpoint() error = nil, want an ambiguity error")
		}
		if !strings.Contains(err.Error(), "acc_default") ||
			!strings.Contains(err.Error(), "acc_extra") {
			t.Errorf("error %q should name both networks", err)
		}
	})

	t.Run("no attachment is an error", func(t *testing.T) {
		if _, _, err := informEndpoint(nil); err == nil {
			t.Fatal("informEndpoint() error = nil, want an error")
		}
	})

	t.Run("an address that is not IPv4 is an error", func(t *testing.T) {
		for _, address := range []string{"", "fd00::2", "not-an-ip"} {
			if _, _, err := informEndpoint(map[string]string{"acc_default": address}); err == nil {
				t.Errorf("informEndpoint(%q) error = nil, want an error", address)
			}
		}
	})
}

func TestDecodeHerderEvents(t *testing.T) {
	// Protocol 1 promises NDJSON on stdout and that consumers ignore unknown
	// fields, so the stream below carries one the provider does not model.
	const stream = `{"protocol":1,"event":"started","run_id":"7f8b1ab4"}
{"protocol":1,"event":"ready","run_id":"7f8b1ab4","devices":[{"index":0,"model":"USM8P","mac":"02:aa:bb:cc:dd:ee","serial":"EMU1","name":"emu-usm8p-1","ip":"172.28.0.4","unknown":true}]}
{"protocol":1,"event":"stopped","run_id":"7f8b1ab4","reason":"signal"}
`

	events := make(chan herderEvent, 8)
	if err := decodeHerderEvents(strings.NewReader(stream), events); err != nil {
		t.Fatalf("decodeHerderEvents() error = %v", err)
	}
	close(events)

	var got []herderEvent
	for ev := range events {
		got = append(got, ev)
	}
	if len(got) != 3 {
		t.Fatalf("decoded %d events, want 3: %+v", len(got), got)
	}
	if got[0].Event != "started" || got[0].RunID != "7f8b1ab4" {
		t.Errorf("first event = %+v, want started", got[0])
	}
	if got[1].Event != "ready" || len(got[1].Devices) != 1 {
		t.Fatalf("second event = %+v, want ready with one device", got[1])
	}
	if got[1].Devices[0].MAC != "02:aa:bb:cc:dd:ee" {
		t.Errorf("ready MAC = %q, want 02:aa:bb:cc:dd:ee", got[1].Devices[0].MAC)
	}
	if got[2].Event != "stopped" || got[2].Reason != "signal" {
		t.Errorf("third event = %+v, want stopped/signal", got[2])
	}
}

func TestMatchHerderFleet(t *testing.T) {
	fleet := []herderFleetMember{
		{Model: "USM8P", Env: "SWITCH_MAC"},
		{Model: "U7PRO", Env: "AP_MAC"},
	}

	t.Run("pairs each device with its fleet member by request index", func(t *testing.T) {
		// Deliberately out of order: the index is what pairs them, not the
		// position in the ready event.
		devices := []herderDevice{
			{Index: 1, Model: "U7PRO", MAC: "02:aa:bb:cc:dd:ef"},
			{Index: 0, Model: "USM8P", MAC: "02:aa:bb:cc:dd:ee"},
		}
		got, err := matchHerderFleet(devices, fleet)
		if err != nil {
			t.Fatalf("matchHerderFleet() error = %v", err)
		}
		if got["SWITCH_MAC"].MAC != "02:aa:bb:cc:dd:ee" {
			t.Errorf("SWITCH_MAC = %q, want the USM8P", got["SWITCH_MAC"].MAC)
		}
		if got["AP_MAC"].MAC != "02:aa:bb:cc:dd:ef" {
			t.Errorf("AP_MAC = %q, want the U7PRO", got["AP_MAC"].MAC)
		}
	})

	t.Run("a short ready event is an error", func(t *testing.T) {
		devices := []herderDevice{{Index: 0, Model: "USM8P", MAC: "02:aa:bb:cc:dd:ee"}}
		if _, err := matchHerderFleet(devices, fleet); err == nil {
			t.Fatal("matchHerderFleet() error = nil, want a count mismatch")
		}
	})

	// A model that does not match the index it claims means the request and
	// the reply have drifted apart, which would silently hand a test the
	// wrong device.
	t.Run("a model that contradicts its index is an error", func(t *testing.T) {
		devices := []herderDevice{
			{Index: 0, Model: "U7PRO", MAC: "02:aa:bb:cc:dd:ee"},
			{Index: 1, Model: "USM8P", MAC: "02:aa:bb:cc:dd:ef"},
		}
		if _, err := matchHerderFleet(devices, fleet); err == nil {
			t.Fatal("matchHerderFleet() error = nil, want a model mismatch")
		}
	})

	t.Run("an out-of-range index is an error", func(t *testing.T) {
		devices := []herderDevice{
			{Index: 0, Model: "USM8P", MAC: "02:aa:bb:cc:dd:ee"},
			{Index: 7, Model: "U7PRO", MAC: "02:aa:bb:cc:dd:ef"},
		}
		if _, err := matchHerderFleet(devices, fleet); err == nil {
			t.Fatal("matchHerderFleet() error = nil, want an index error")
		}
	})

	t.Run("a device with no MAC is an error", func(t *testing.T) {
		devices := []herderDevice{
			{Index: 0, Model: "USM8P"},
			{Index: 1, Model: "U7PRO", MAC: "02:aa:bb:cc:dd:ef"},
		}
		if _, err := matchHerderFleet(devices, fleet); err == nil {
			t.Fatal("matchHerderFleet() error = nil, want a missing-MAC error")
		}
	})

	t.Run("two devices claiming one index is an error", func(t *testing.T) {
		devices := []herderDevice{
			{Index: 0, Model: "USM8P", MAC: "02:aa:bb:cc:dd:ee"},
			{Index: 0, Model: "USM8P", MAC: "02:aa:bb:cc:dd:ef"},
		}
		if _, err := matchHerderFleet(devices, fleet); err == nil {
			t.Fatal("matchHerderFleet() error = nil, want a duplicate-index error")
		}
	})
}

// TestHerderFleetIsWellFormed guards the fleet itself: a duplicate variable or
// a blank field would hand some test an empty MAC and skip it silently.
func TestHerderFleetIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for i, member := range herderFleet {
		if member.Model == "" || member.Env == "" {
			t.Errorf("herderFleet[%d] has a blank Model or Env: %+v", i, member)
		}
		if seen[member.Env] {
			t.Errorf("herderFleet[%d] reuses %s", i, member.Env)
		}
		seen[member.Env] = true
	}
	for _, env := range []string{EnvAccDeviceMAC, EnvAccAPMAC} {
		if !seen[env] {
			t.Errorf("no fleet member publishes %s", env)
		}
	}
}

func TestHerderFailureMessage(t *testing.T) {
	ev := herderEvent{
		Event:   "failed",
		Phase:   "validate",
		Code:    "reaper_disabled",
		Message: "the Testcontainers reaper is disabled",
	}
	got := herderFailureMessage(ev)
	for _, want := range []string{"validate", "reaper_disabled", "the Testcontainers reaper is disabled"} {
		if !strings.Contains(got, want) {
			t.Errorf("herderFailureMessage() = %q, want it to contain %q", got, want)
		}
	}
}
