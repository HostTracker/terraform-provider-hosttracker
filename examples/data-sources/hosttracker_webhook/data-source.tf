# By id.
data "hosttracker_webhook" "ops" {
  id = "0c3c7b07-cecb-43dd-9b76-8516d3b9c771"
}

# Or by the address it delivers to, which must match exactly one webhook.
data "hosttracker_webhook" "by_url" {
  lookup_url = "https://hooks.example.com/host-tracker"
}

output "ops_events" {
  value = data.hosttracker_webhook.ops.events
}

# How many monitors the scope resolved to - zero is worth alerting on.
output "ops_monitor_count" {
  value = data.hosttracker_webhook.ops.monitor_count
}
