data "hosttracker_monitor_types" "all" {}

# The interval floor this account may use for http monitors, which is the
# larger of the type's own floor and what the package sells.
locals {
  http_type = one([for t in data.hosttracker_monitor_types.all.types : t if t.type == "http"])
}

output "http_min_interval" {
  value = local.http_type.account_limits.min_interval
}
