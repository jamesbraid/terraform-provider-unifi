# THE UPGRADE FIXTURE. Its control must come back 0: the released provider has
# to settle this configuration, because the question is whether the CANDIDATE
# disturbs something the released provider left settled.
#
# It holds one resource on purpose, and that is a limit rather than a design.
# accept's manual run labelled this exact shape "control, settles auto" against
# the released provider, so it is the only one measured to satisfy the control.
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
