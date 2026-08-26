# An http monitor with a keyword check, run from everywhere, that needs
# every location to agree before it calls the site down.
resource "hosttracker_monitor" "marketing" {
  type     = "http"
  url      = "https://example.com/"
  name     = "Marketing site"
  interval = 300
  tags     = ["prod", "web"]

  locations = {
    pools    = ["allworld"]
    fallback = "world"
  }

  recheck = {
    strategy = "fullAgreement"
  }

  settings = {
    http = {
      method          = "G"
      keywords        = "Sign in"
      keyword_mode    = "PresentAny"
      follow_redirect = true
      timeout         = 20000

      headers = [
        {
          name  = "X-Monitored-By"
          value = "terraform"
        },
      ]

      # Sub-checks that ride on this monitor rather than costing a monitor
      # of their own.
      attached = {
        ssl_exp = { enabled = true }
        dnsbl   = { enabled = true }
      }

      cert_watch_days = [7, 30]
    }
  }
}

# A ping monitor pinned to European locations.
resource "hosttracker_monitor" "gateway" {
  type = "ping"
  url  = "gateway.example.com"
  name = "Edge gateway"

  locations = {
    pools = ["europe"]
  }
}

# A certificate-expiry monitor. It runs on the fixed internal network, so
# it takes no locations and its settings block has no members.
resource "hosttracker_monitor" "certificate" {
  type = "sslExp"
  url  = "https://example.com/"
  name = "Certificate"
}

# A type this provider has no typed settings block for yet, written
# through the JSON escape hatch. Only the members named here are managed.
resource "hosttracker_monitor" "api_check" {
  type = "api"
  url  = "https://example.com/health"
  name = "Health endpoint"

  locations = {
    pools = ["allworld"]
  }

  settings_json = jsonencode({
    method  = "G"
    accept  = "application/json"
    timeout = 15000
  })
}
