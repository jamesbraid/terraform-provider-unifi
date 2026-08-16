# THE UPGRADE FIXTURE. Its control must come back 0: the released provider has
# to settle this configuration, because the question is whether the CANDIDATE
# disturbs something the released provider left settled.
#
# EXPECT_OLD_PLAN=0 IS AN EXPECTATION, NOT A MEASUREMENT, AND THE FIRST RUN IS
# WHAT EARNS IT.
#
# An earlier version of this comment said this shape was "measured to satisfy
# the control". It was not. The evidence behind it is a label from a manual run
# -- "control, settles auto" -- which came from the controller's own
# /rest/networkconf reply and the state the released provider wrote. That is
# strong reason to expect a clean plan and it is not a plan exit code. The same
# run never planned this resource in ISOLATION: its old-side plan covered four
# resources together and returned 2, driven entirely by the zone-attached one,
# so this one's individual contribution was never separated. And that run's
# "old" side was a branch base rather than v0.101.2, so against the actual
# baseline this shape's plan behaviour has never been observed at all.
#
# The harness resolves this on its own the first time it runs: the control plan
# IS the measurement, and the receipt records control_exit beside
# expected_control_exit. A control of 2 here is not a harness failure, it is the
# finding that this resource does not settle under v0.101.2 and cannot found an
# upgrade fixture.
#
# It holds one resource on purpose, and that is a limit rather than a design.
#
# BROADENING THIS IS THE OBVIOUS NEXT STEP AND IT HAS A RULE: every addition
# must first be SHOWN to settle under v0.101.2. A resource that does not settle
# there turns the control red, and a red control voids the whole run rather than
# just its own row -- so one unproven addition costs the measurement, not a line
# of it. Add them one at a time, with the control observed green each time.
#
# The zone-attached shape belongs in fixtures/upgrade-regression, not here: it
# is the state the released provider gets WRONG, which is what makes it a
# regression fixture and useless as an upgrade control.

terraform {
  required_providers {
    unifi = {
      source = "registry.terraform.io/ubiquiti-community/unifi"
    }
  }
}

provider "unifi" {}

resource "unifi_network" "bare_corporate" {
  name   = "tf-upg-corp"
  subnet = "10.191.0.1/24"
  vlan   = 191
}
