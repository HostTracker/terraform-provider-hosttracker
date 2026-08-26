# By the name the group is known under. Group names are unique per
# account, so this resolves to one group or to none.
data "hosttracker_contact_group" "ops" {
  lookup_name = "Ops"
}

# By id.
data "hosttracker_contact_group" "by_id" {
  id = "7d1b3e55-2a90-4c11-8f36-4b1d2e6a9c83"
}

# `contact_ids` is the group's membership flattened, for wiring its
# contacts into subscriptions.
output "ops_members" {
  value = data.hosttracker_contact_group.ops.contact_ids
}
