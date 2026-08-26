data "hosttracker_webhooks" "all" {}

# The webhooks the API has switched off after repeated delivery failures.
output "broken_webhooks" {
  value = [
    for webhook in data.hosttracker_webhooks.all.webhooks :
    webhook.url if webhook.disabled_reason != null
  ]
}
