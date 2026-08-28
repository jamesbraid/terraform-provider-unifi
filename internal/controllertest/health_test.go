package controllertest

import (
	"testing"

	"github.com/moby/moby/api/types/container"
)

func TestHealthVerdict(t *testing.T) {
	cases := map[string]struct {
		state   *container.State
		ready   bool
		wantErr bool
	}{
		"no healthcheck declared is ready (nothing to wait on)": {state: &container.State{Running: true}, ready: true},
		"starting is not ready":                                 {state: &container.State{Running: true, Health: &container.Health{Status: "starting"}}},
		"healthy is ready":                                      {state: &container.State{Running: true, Health: &container.Health{Status: "healthy"}}, ready: true},
		"unhealthy is an error, not a wait":                     {state: &container.State{Running: true, Health: &container.Health{Status: "unhealthy", FailingStreak: 3}}, wantErr: true},
		"a container that exited is an error":                   {state: &container.State{Status: "exited", ExitCode: 137}, wantErr: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ready, err := healthVerdict(tc.state)
			if (err != nil) != tc.wantErr {
				t.Fatalf("healthVerdict() error = %v, wantErr %t", err, tc.wantErr)
			}
			if ready != tc.ready {
				t.Errorf("healthVerdict() ready = %t, want %t", ready, tc.ready)
			}
		})
	}
}
