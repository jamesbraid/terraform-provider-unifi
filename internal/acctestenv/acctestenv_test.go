package acctestenv_test

import (
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/acctestenv"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/controllertest"
)

// THE TWO DECLARATIONS MUST AGREE, and this is the only place that can say so.
//
// acctestenv restates names that internal/controllertest also declares, because
// the harness is grafted onto the released tree and cannot import a package that
// tree does not have. Duplication with no check is how two constants drift until
// an acceptance run reads an environment variable nobody sets.
//
// IT LIVES HERE RATHER THAN IN THE HARNESS because the graft copies test files
// too: the same import that breaks the released build from herder.go breaks it
// from herder_test.go. Here it costs one package a heavy test binary and leaves
// ./unifi at 59 modules, which is the whole point of the exercise.
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
