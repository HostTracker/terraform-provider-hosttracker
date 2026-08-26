# A Sunday-night database window that holds back both the alerting and
# the statistics for every monitor it covers. The end is given as a
# length; `to` is read back derived from it.
resource "hosttracker_maintenance" "weekly_database" {
  name         = "Weekly database maintenance"
  from         = 1785712608
  duration_sec = 3600
  timezone     = "Europe/Berlin"

  recurrence = {
    week_days = ["Sunday"]
  }

  monitor_ids = [
    hosttracker_monitor.marketing.id,
    hosttracker_monitor.api.id,
  ]

  suppress = {
    alerts = true
    stats  = true
  }
}

# A one-off deploy window that treats its monitors differently: the
# marketing site is silenced outright, while the API only stays out of the
# uptime figures. The end is given as an instant here.
resource "hosttracker_maintenance" "deploy" {
  name     = "Release 4.2"
  from     = 1785712608
  to       = 1785714408
  timezone = "Europe/Berlin"

  monitors = [
    {
      monitor_id = hosttracker_monitor.marketing.id
      suppress   = { alerts = true, stats = true }
    },
    {
      monitor_id = hosttracker_monitor.api.id
      suppress   = { alerts = false, stats = true }
    },
  ]
}

resource "hosttracker_monitor" "marketing" {
  type      = "http"
  url       = "https://example.com/"
  name      = "Marketing site"
  locations = { pools = ["allworld"] }
}

resource "hosttracker_monitor" "api" {
  type      = "http"
  url       = "https://api.example.com/health"
  name      = "API"
  locations = { pools = ["allworld"] }
}

output "deploy_window_ends" {
  # Instants are Unix seconds; the RFC 3339 twin is there to be read.
  value = hosttracker_maintenance.deploy.to_rfc3339
}
