# Every contact on the account.
data "hosttracker_contacts" "all" {}

# The text-message and voice contacts that have not confirmed their
# address yet - the ones alerts are silently not delivered to.
data "hosttracker_contacts" "unconfirmed_phones" {
  type      = ["sms", "voiceCall"]
  confirmed = false
}

# A free-text search over name and address.
data "hosttracker_contacts" "ops" {
  q           = "ops"
  max_results = 100
}

output "unconfirmed_phone_addresses" {
  value = [for c in data.hosttracker_contacts.unconfirmed_phones.contacts : c.address]
}
