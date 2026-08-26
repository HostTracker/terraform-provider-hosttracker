# Terraform Provider for HostTracker

Manage [HostTracker](https://www.host-tracker.com) monitoring as code: uptime, port, blacklist, certificate and
domain checks, declared in Terraform and reconciled through the HostTracker **API v2**.

* Registry: [`HostTracker/hosttracker`](https://registry.terraform.io/providers/HostTracker/hosttracker/latest)
* API reference: <https://www.host-tracker.com/apidocs/v2>
* Built on [terraform-plugin-framework](https://github.com/hashicorp/terraform-plugin-framework) (protocol 6) and the
  [HostTracker Go SDK](https://github.com/HostTracker/hosttracker-sdk-go).

Terraform 1.0 or newer, or OpenTofu.

## Install

```hcl
terraform {
  required_providers {
    hosttracker = {
      source  = "HostTracker/hosttracker"
      version = "~> 0.1"
    }
  }
}

provider "hosttracker" {}
```

## Authenticate

Mint a token in the web app under **Integrations → API**
(<https://www.host-tracker.com/integrations/api>), carrying the scopes the configuration needs -
`monitor:read` and `monitor:write` for the monitor resource, plus `account:read` for the
`hosttracker_account` data source. A token may also carry an IP allow-list and a self-cap.

Put it in the environment rather than in a `.tf` file: a token written into the configuration ends up in version
control, and in the state file as well.

```sh
export HT_TOKEN="…"
# export HT_BASE_URL="https://api2.host-tracker.com"   # the default
```

A token cannot be revoked before it expires - only an account-wide API disable kills every token at once. Treat it
like a password.

## Example

```hcl
data "hosttracker_locations" "all" {}

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
      keywords        = "Sign in"
      follow_redirect = true
      timeout         = 20000

      headers = [
        { name = "X-Monitored-By", value = "terraform" },
      ]

      # Sub-checks riding on this monitor rather than costing one of their own.
      attached = {
        ssl_exp = { enabled = true }
        dnsbl   = { enabled = true }
      }

      cert_watch_days = [7, 30]
    }
  }
}

output "marketing_state" {
  value = hosttracker_monitor.marketing.state
}
```

Run `terraform plan` and `terraform apply` as usual.

## Resources and data sources

| Resource | What it manages |
|---|---|
| `hosttracker_monitor` | One check of one address, on a schedule, from the locations you choose |
| `hosttracker_webhook` | A signed delivery endpoint, its event set, its scope and its signing secret |
| `hosttracker_alert_subscription` | Which state changes of one monitor reach one contact |
| `hosttracker_report_subscription` | How often one contact is sent an uptime report about one monitor |
| `hosttracker_status_page` | A public status page: its appearance, and the components it publishes |
| `hosttracker_contact` | One place an alert or a report is delivered to: an address, its language and its active hours |
| `hosttracker_contact_group` | A named preset of contacts and the events each of them is subscribed to by it |
| `hosttracker_maintenance` | A window of planned downtime, held out of the alerting, of the statistics, or of both |

| Data source | What it reads |
|---|---|
| `hosttracker_monitor` / `hosttracker_monitors` | One monitor, by id or lookup; or a filtered listing |
| `hosttracker_monitor_types` | The check types the account may create, with their interval floors |
| `hosttracker_locations` | The monitoring-location pools and agents |
| `hosttracker_account` | The account's limits and quota |
| `hosttracker_webhook` / `hosttracker_webhooks` | One webhook, by id or delivery address; or the account's webhooks |
| `hosttracker_status_page` / `hosttracker_status_pages` | One page with its settings and components, by id or slug; or the account's pages |
| `hosttracker_contact` / `hosttracker_contacts` | One contact, by id or lookup; or a filtered listing |
| `hosttracker_contact_group` | One contact group with its membership, by id or name |
| `hosttracker_contact_types` | The channels a contact can use, with the alert delays each one accepts |
| `hosttracker_maintenance_windows` | The account's maintenance windows, by span, state or covered monitor |

## Import

A monitor created in the web app is adopted by its id, which the address bar of its page shows and the
`hosttracker_monitors` data source publishes as `ids`:

```sh
terraform import hosttracker_monitor.marketing 8e2d4c8b-7a41-4a2b-9d0e-2f3a5c6b7d8e
```

Creating a monitor for an address that already has one is refused with `409 duplicate_monitor`, and the provider
puts the existing monitor's id in the error so that it can be imported instead.

## Things worth knowing

**Optional attributes are also computed.** The API reads an absent member as "leave this alone", never as "clear
this". Removing an attribute from the configuration therefore keeps the value it last had; to clear one, write the
empty value (`""`, `[]`) explicitly.

**`type` is immutable.** Changing it replaces the monitor, because the API refuses to change the type of an
existing one. `pageSpeed` is an accepted spelling of `waterfall`, and the spelling that is written is the spelling
that stays in state.

**`url` is raw.** It round-trips exactly as written; `effective_url` carries the normalized address actually
monitored.

**Types with no typed settings block yet** - `waterfall`, `tran`, `api`, `database`, `counter`, `snmp`, `cntCheck` -
are written through `settings_json`, which takes the settings object as JSON. Only the members it names are
managed: the rest of the stored settings are left alone, and drift is reported for the named members only.

**Rate limits.** Terraform's default parallelism of 10 is comfortable under a paid plan's quota. On a trial token
(10 reads and 5 writes a minute) run `terraform apply -parallelism=3`; the provider retries a `429 rate_limited`
honouring the API's `Retry-After`, but a whole plan applied at once can still exhaust the window.

**Every timestamp is Unix seconds** - `created`, `updated`, `since` - because that is what the API publishes.
`timeadd`/`formatdate` over `timestamp()` renders them when a human has to read one.

## Development

```sh
make build      # compile the provider
make test       # unit tests: no credential, no terraform binary
make docs       # regenerate docs/ from the schemas and examples
make lint       # go vet, gofmt, terraform fmt
```

To try an unreleased build, point Terraform at the compiled binary with a dev override in `~/.terraformrc`:

```hcl
provider_installation {
  dev_overrides {
    "HostTracker/hosttracker" = "/path/to/this/repo"
  }
  direct {}
}
```

Then run Terraform as usual and ignore the warning it prints about the override. `terraform init` is skipped for an
overridden provider.

### Acceptance tests

They create, change and delete **real monitors** on the account the token belongs to. Use an account kept for the
purpose, never a production one.

```sh
export TF_ACC=1
export HT_TOKEN="…"
export HT_BASE_URL="https://api2.host-tracker.com"
make testacc
```

They need a `terraform` binary on `PATH`; `make terraform` downloads one into `.tools/` rather than installing it
system-wide. `HT_ACC_PREFIX` renames the monitors they create, which keeps two runs on one account apart.

## Licence

Mozilla Public License 2.0 - see [LICENSE](LICENSE).
