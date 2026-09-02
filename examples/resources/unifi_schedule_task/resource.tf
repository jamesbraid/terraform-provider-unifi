# A weekly firmware upgrade for two access points, running at 04:00 every
# Sunday.
resource "unifi_schedule_task" "weekly" {
  name              = "weekly-ap-upgrade"
  cron_expr         = "0 4 * * 0"
  execute_only_once = false

  upgrade_targets = [
    { mac = "00:11:22:33:44:55" },
    { mac = "00:11:22:33:44:66" },
  ]
}

# A one-off upgrade of a single switch at the next matching time.
resource "unifi_schedule_task" "one_off" {
  name              = "switch-upgrade"
  cron_expr         = "30 2 * * *"
  execute_only_once = true

  upgrade_targets = [
    { mac = "00:11:22:33:44:77" },
  ]
}
