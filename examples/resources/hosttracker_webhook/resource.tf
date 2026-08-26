variable "receiver_token" {
  description = "The bearer token the webhook receiver expects."
  type        = string
  sensitive   = true
}

# A webhook covering every monitor on the account, including ones created
# later. The signing secret is minted by the server and captured into
# state; bump `rotate_secret` to mint a new one.
resource "hosttracker_webhook" "ops" {
  url    = "https://hooks.example.com/host-tracker"
  name   = "Ops channel"
  events = ["monitor.down", "monitor.up", "incident.opened", "incident.closed"]

  scope = {
    all = true
  }

  headers = [
    {
      header = "Authorization"
      value  = "Bearer ${var.receiver_token}"
    },
  ]
}

# A webhook narrowed to the monitors carrying a tag. The set is matched
# afresh on every delivery, so a monitor that gains the tag joins without
# an apply.
resource "hosttracker_webhook" "production_pages" {
  url    = "https://hooks.example.com/host-tracker/production"
  name   = "Production paging"
  events = ["monitor.down", "monitor.repeatedlyDown"]

  scope = {
    tags = ["prod"]
  }
}

resource "hosttracker_monitor" "checkout" {
  type = "http"
  url  = "https://example.com/checkout"
  name = "Checkout"

  locations = {
    pools = ["allworld"]
  }
}

# A webhook addressed to named monitors, with the secret rotated. The
# previous secret keeps signing for 24 hours, so a verifier that accepts
# any matching signature sees no gap.
resource "hosttracker_webhook" "checkout" {
  url    = "https://hooks.example.com/host-tracker/checkout"
  name   = "Checkout only"
  events = ["monitor.down", "monitor.up"]

  scope = {
    monitor_ids = [hosttracker_monitor.checkout.id]
  }

  rotate_secret = 2
}

# Hand the secret to whatever verifies the deliveries. It is sensitive, so
# Terraform refuses to print it without `sensitive = true` here too.
output "ops_webhook_secret" {
  value     = hosttracker_webhook.ops.secret
  sensitive = true
}
