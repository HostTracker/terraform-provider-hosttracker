resource "hosttracker_monitor" "marketing" {
  type = "http"
  url  = "https://example.com/"
  name = "Marketing site"

  locations = {
    pools = ["allworld"]
  }
}

resource "hosttracker_monitor" "api" {
  type = "http"
  url  = "https://api.example.com/health"
  name = "API"

  locations = {
    pools = ["allworld"]
  }
}

# A public status page. The component list is a snapshot: every save
# replaces the whole set, in the order written here, and a component the
# list omits is removed from the page.
resource "hosttracker_status_page" "acme" {
  slug  = "acme-status"
  title = "Acme status"

  settings = {
    homepage_url = "https://example.com"
    theme        = "light"
    theme_color  = "#2f6feb"
    density      = "wide"
    show_groups  = true
    robots_index = true
    language     = "en"
    sla_target   = 99.9

    # The WHOLE feature set: a feature not listed here is off.
    features = ["barCharts", "uptimePercent", "outageDetails", "subscribe"]

    # Leave this off while Terraform owns the components: a monitor the
    # API adds by itself is a component the next plan proposes to remove.
    auto_add_monitors = false
  }

  components = [
    {
      monitor_id = hosttracker_monitor.marketing.id
      group      = "Public"
    },
    {
      monitor_id = hosttracker_monitor.api.id
      name       = "Public API"
      group      = "Public"
    },
    # A dependency this account does not monitor. Its state is pinned by
    # hand, so changing it is an apply.
    {
      third_party  = true
      name         = "Payments provider"
      group        = "Dependencies"
      manual_state = "operational"
    },
  ]
}

output "acme_status_page" {
  value = hosttracker_status_page.acme.public_url
}
