# By id.
data "hosttracker_monitor" "by_id" {
  id = "8e2d4c8b-7a41-4a2b-9d0e-2f3a5c6b7d8e"
}

# By the address it watches. The lookup must match exactly one monitor.
data "hosttracker_monitor" "by_url" {
  lookup_url = "https://example.com/"
}

# By display name, for a monitor created outside Terraform.
data "hosttracker_monitor" "by_name" {
  lookup_name = "Marketing site"
}

output "marketing_state" {
  value = data.hosttracker_monitor.by_url.state
}
