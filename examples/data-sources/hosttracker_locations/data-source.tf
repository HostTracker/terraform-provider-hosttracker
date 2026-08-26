data "hosttracker_locations" "all" {}

# Locations that can run a ping check from Germany.
data "hosttracker_locations" "german_icmp" {
  country    = ["Germany"]
  capability = ["icmp"]
}

output "pool_ids" {
  value = data.hosttracker_locations.all.pool_ids
}

# The addresses those locations check from, for a firewall allow-list.
output "german_egress_ips" {
  value = [for a in data.hosttracker_locations.german_icmp.agents : a.ip if a.ip != null]
}
