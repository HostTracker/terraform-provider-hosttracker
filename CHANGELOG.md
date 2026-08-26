# Changelog

## Unreleased

NOTES:

* Built on `hosttracker-sdk-go` v0.2.0.

FEATURES:

* **New provider:** `hosttracker`, on the HostTracker API v2. Configured with `token` (`HT_TOKEN`), `base_url`
  (`HT_BASE_URL`), `timeout` and `retry_max`.
* **New resource:** `hosttracker_monitor`, with typed `settings` blocks for the `http`, `ping`, `port`, `dnsbl`,
  `sslExp`, `domainExp` and `webRisk` types, a `settings_json` escape hatch for the rest, and import by id.
* **New data source:** `hosttracker_monitor`, by id or by a `lookup_url` / `lookup_name` that resolves to exactly one.
* **New data source:** `hosttracker_monitors`, filtered by state, type, tag and free text.
* **New data source:** `hosttracker_monitor_types`, the type catalogue with the calling account's own interval floors.
* **New data source:** `hosttracker_locations`, the monitoring-location pools and the locations in them.
* **New data source:** `hosttracker_account`, the account's package, limits and remaining API quota.
* **New resource:** `hosttracker_contact`, for the `email`, `sms` and `voiceCall` channels, with active hours,
  language, alert delay and import by id. An `http` contact is refused at plan time in favour of
  `hosttracker_webhook`.
* **New resource:** `hosttracker_contact_group`, a named membership preset with its per-contact event sets.
* **New resource:** `hosttracker_maintenance`, a window given as a start plus an end or a length, covering its
  monitors uniformly or one by one, with weekly recurrence.
* **New data source:** `hosttracker_contact`, by id or by a `lookup_address` / `lookup_name` that resolves to
  exactly one.
* **New data source:** `hosttracker_contacts`, filtered by type, confirmation state and free text.
* **New data source:** `hosttracker_contact_group`, by id or by name, with its membership flattened to `contact_ids`.
* **New data source:** `hosttracker_contact_types`, the channel catalogue with the alert delays each one accepts.
* **New data source:** `hosttracker_maintenance_windows`, filtered by span, state, covered monitor and name.

FIXED:

* `hosttracker_maintenance` and `hosttracker_webhook` no longer plan an update on every run. A configuration that
  leaves out a nested attribute - `monitors` on a window it covers through `monitor_ids`, `headers` on a webhook
  that sends none - is proposed by Terraform as unknown rather than as the value the state holds, which read as an
  edit and turned every computed attribute into "known after apply". Both resources now settle for themselves
  whether anything the configuration owns has changed, and plan nothing when it has not.
* `hosttracker_status_page` accepts a component whose `monitor_id` is only known after apply -
  `monitor_id = hosttracker_monitor.x.id` is the ordinary way to write one. The plan-time check that a component
  names something to show was reading an unknown id as an absent one and refusing the configuration.
* A `hosttracker_status_page` adopted with `terraform import` reads its monitored components the way a
  configuration writes them: without the label they inherit from their monitor and without the `third_party = false`
  that naming a monitor already implies, so the first plan after the import has nothing to say.
* `hosttracker_webhook` state is taken from a read of the stored webhook rather than from the answer to the write,
  whose `created` and `updated` can be a second behind it. The signing secret, which is published only in that
  answer, is carried across.
