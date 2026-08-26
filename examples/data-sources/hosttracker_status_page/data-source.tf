# By slug, which is the page's permanent public address and unique across
# the product.
data "hosttracker_status_page" "acme" {
  slug = "acme-status"
}

# Or by id.
data "hosttracker_status_page" "by_id" {
  id = "6b1f2a70-3c8e-4d51-9f2a-7c4e5d6b8a90"
}

output "acme_public_url" {
  value = data.hosttracker_status_page.acme.public_url
}

# The monitors the page publishes, third-party components left out.
output "acme_published_monitors" {
  value = [
    for component in data.hosttracker_status_page.acme.components :
    component.monitor_id if !component.third_party
  ]
}
