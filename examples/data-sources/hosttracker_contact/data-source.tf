# By id.
data "hosttracker_contact" "by_id" {
  id = "8e2d4c8b-7a41-4a2b-9d0e-2f3a5c6b7d8e"
}

# By the address it delivers to. The lookup must match exactly one contact.
data "hosttracker_contact" "by_address" {
  lookup_address = "oncall@example.com"
}

# By display name, for a contact created outside Terraform - a messenger
# contact bound by registering with the bot, for instance.
data "hosttracker_contact" "by_name" {
  lookup_name = "Ops Telegram"
}

output "oncall_confirmed" {
  value = data.hosttracker_contact.by_address.confirmed
}
