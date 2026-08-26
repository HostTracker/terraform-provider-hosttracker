data "hosttracker_account" "current" {}

output "package" {
  value = data.hosttracker_account.current.package.name
}

# What is left of the API quota for the configured token.
output "api_calls_remaining" {
  value = data.hosttracker_account.current.quota.remaining
}
