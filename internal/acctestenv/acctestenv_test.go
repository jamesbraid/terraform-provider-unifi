package acctestenv_test

import (
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/acctestenv"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/controllertest"
)

// TestTheseNamesMatchTheHarness is the only check that acctestenv's
// duplicated declarations still agree with controllertest's.
func TestTheseNamesMatchTheHarness(t *testing.T) {
	if acctestenv.EnvAccDeviceMAC != controllertest.EnvAccDeviceMAC {
		t.Errorf("device MAC variable: acctestenv says %q, the harness sets %q",
			acctestenv.EnvAccDeviceMAC, controllertest.EnvAccDeviceMAC)
	}
	if acctestenv.EnvAccAPMAC != controllertest.EnvAccAPMAC {
		t.Errorf("AP MAC variable: acctestenv says %q, the harness sets %q",
			acctestenv.EnvAccAPMAC, controllertest.EnvAccAPMAC)
	}
}
