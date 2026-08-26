# Every production monitor that is currently down.
data "hosttracker_monitors" "down_in_prod" {
  tag   = ["prod"]
  state = ["down"]
}

output "down_urls" {
  value = [for m in data.hosttracker_monitors.down_in_prod.monitors : m.url]
}

# Every http monitor, with its settings read as well.
data "hosttracker_monitors" "web" {
  type             = ["http"]
  include_settings = true
  max_results      = 500
}
