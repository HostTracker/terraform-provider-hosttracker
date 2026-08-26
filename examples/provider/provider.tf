terraform {
  required_providers {
    hosttracker = {
      source  = "HostTracker/hosttracker"
      version = "~> 0.1"
    }
  }
}

# The token is read from the HT_TOKEN environment variable when the
# provider block does not set it, which is where a credential belongs: a
# token written into a .tf file ends up in version control and in state.
provider "hosttracker" {
  # token    = var.hosttracker_token
  # base_url = "https://api2.host-tracker.com"
  # timeout  = 30
  # retry_max = 2
}
