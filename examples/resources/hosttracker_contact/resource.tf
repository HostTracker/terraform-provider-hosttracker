# An on-call mailbox that only accepts delivery during office hours.
resource "hosttracker_contact" "oncall" {
  type        = "email"
  address     = "oncall@example.com"
  name        = "On-call"
  language    = "en"
  alert_delay = 5

  active_period = {
    start    = "09:00:00"
    end      = "18:00:00"
    days     = ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday"]
    timezone = "W. Europe Standard Time"
  }
}

# A phone that is woken only once a failure has held for fifteen minutes,
# with every simultaneous alert collapsed into one message.
resource "hosttracker_contact" "duty_phone" {
  type           = "sms"
  address        = "+15550100"
  name           = "Duty phone"
  alert_delay    = 15
  grouped_alerts = true

  # The API sends a confirmation code when the contact is created. This
  # sends it once more, for a channel where the first can go astray; the
  # code is entered by a person, so Terraform never sees it and the
  # contact stays unconfirmed until they do.
  send_confirmation = true
}

# The address the account's billing notices go to.
resource "hosttracker_contact" "billing" {
  type                  = "email"
  address               = "billing@example.com"
  name                  = "Accounts"
  billing_notifications = true
  send_news             = false
}

output "oncall_confirmed" {
  value = hosttracker_contact.oncall.confirmed
}
