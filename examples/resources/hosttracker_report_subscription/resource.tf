variable "reports_contact_id" {
  description = "A confirmed EMAIL contact: uptime reports are delivered by email only."
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

# A monthly uptime report, and a weekly one during the quarter's push.
# The set is written whole, so both frequencies are named together.
resource "hosttracker_report_subscription" "marketing_reports" {
  monitor_id  = hosttracker_monitor.marketing.id
  contact_id  = var.reports_contact_id
  frequencies = ["weekly", "monthly"]
}
