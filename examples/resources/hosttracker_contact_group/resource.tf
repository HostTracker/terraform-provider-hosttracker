# A preset that subscribes the whole on-call rotation to outage alerts,
# and the team lead to the weekly report as well.
resource "hosttracker_contact_group" "ops" {
  name = "Ops"

  items = [
    {
      contact_id = hosttracker_contact.oncall.id
      events     = ["down", "up", "repeatedlyDown"]
    },
    {
      contact_id = hosttracker_contact.duty_phone.id
      events     = ["down"]
    },
    {
      contact_id = hosttracker_contact.lead.id
      events     = ["down", "weekly"]
    },
  ]
}

resource "hosttracker_contact" "oncall" {
  type    = "email"
  address = "oncall@example.com"
  name    = "On-call"
}

resource "hosttracker_contact" "duty_phone" {
  type    = "sms"
  address = "+15550100"
  name    = "Duty phone"
}

resource "hosttracker_contact" "lead" {
  type    = "email"
  address = "lead@example.com"
  name    = "Team lead"
}
