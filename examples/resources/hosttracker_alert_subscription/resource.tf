variable "oncall_contact_id" {
  description = "A confirmed contact on the account. Confirmation happens out of band, so a contact is prepared once and referred to here."
  type        = string
}

variable "escalation_contact_id" {
  description = "The contact paged only when a monitor stays down."
  type        = string
}

resource "hosttracker_monitor" "marketing" {
  type = "http"
  url  = "https://example.com/"
  name = "Marketing site"

  locations = {
    pools = ["allworld"]
  }
}

# Alert the on-call contact when the site goes down and when it comes
# back. The set is written whole: adding a type here replaces what the
# pair had, and destroying the resource stops the alerts entirely.
resource "hosttracker_alert_subscription" "marketing_oncall" {
  monitor_id  = hosttracker_monitor.marketing.id
  contact_id  = var.oncall_contact_id
  alert_types = ["down", "up"]
}

# Only the repeated failures, for a contact that should not hear about a
# single blip.
resource "hosttracker_alert_subscription" "marketing_escalation" {
  monitor_id  = hosttracker_monitor.marketing.id
  contact_id  = var.escalation_contact_id
  alert_types = ["repeatedlyDown"]
}
