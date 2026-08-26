# The windows that have not happened yet.
data "hosttracker_maintenance_windows" "scheduled" {
  state = ["scheduled"]
}

# The windows covering one monitor, whichever state they are in.
data "hosttracker_maintenance_windows" "for_marketing" {
  monitor = [hosttracker_monitor.marketing.id]
}

# The windows starting inside one span. `from` and `to` bound the window's
# START, not its extent: a long window that began earlier is not matched.
data "hosttracker_maintenance_windows" "this_quarter" {
  from        = 1785712608
  to          = 1793488608
  max_results = 200
}

resource "hosttracker_monitor" "marketing" {
  type      = "http"
  url       = "https://example.com/"
  name      = "Marketing site"
  locations = { pools = ["allworld"] }
}

output "next_window_starts" {
  value = try(data.hosttracker_maintenance_windows.scheduled.windows[0].from_rfc3339, null)
}
