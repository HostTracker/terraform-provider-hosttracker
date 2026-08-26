# The catalogue says which channels can be created through the API at all,
# which of them need confirming, and which alert delays each one accepts -
# a contact's `alert_delay` must be one of those, or the write is refused.
data "hosttracker_contact_types" "all" {}

output "email_alert_delays" {
  value = one([for t in data.hosttracker_contact_types.all.types : t.alert_delays if t.type == "email"])
}

output "creatable_types" {
  value = [for t in data.hosttracker_contact_types.all.types : t.type if t.creatable]
}
