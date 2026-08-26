package provider_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// windowStart is an instant comfortably in the future, so that an
// acceptance window is `scheduled` rather than already over.
func windowStart() int64 {
	return time.Now().Add(24 * time.Hour).Truncate(time.Hour).Unix()
}

func contactGroupConfig(name, events string) string {
	return fmt.Sprintf(`
resource "hosttracker_contact" "member" {
  type    = "email"
  address = %q
  name    = "%s-member"
}

resource "hosttracker_contact_group" "test" {
  name = %q

  items = [
    {
      contact_id = hosttracker_contact.member.id
      events     = %s
    },
  ]
}
`, testEmail(), name, name, events)
}

// TestAccContactGroup walks the whole lifecycle of a group, whose
// membership is replaced rather than merged on every write.
func TestAccContactGroup(t *testing.T) {
	name := testPrefix() + "-group"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: contactGroupConfig(name, `["down"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("hosttracker_contact_group.test", "id"),
					resource.TestCheckResourceAttr("hosttracker_contact_group.test", "name", name),
					resource.TestCheckResourceAttr("hosttracker_contact_group.test", "items.#", "1"),
					resource.TestCheckResourceAttrSet("hosttracker_contact_group.test", "created"),
				),
			},
			{
				Config: contactGroupConfig(name, `["down", "up", "weekly"]`),
				Check: resource.TestCheckResourceAttr(
					"hosttracker_contact_group.test", "items.0.events.#", "3"),
			},
			{
				ResourceName:      "hosttracker_contact_group.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: contactGroupConfig(name, `["down"]`) + `
data "hosttracker_contact_group" "by_name" {
  lookup_name = hosttracker_contact_group.test.name
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair(
						"data.hosttracker_contact_group.by_name", "id",
						"hosttracker_contact_group.test", "id"),
					resource.TestCheckResourceAttr("data.hosttracker_contact_group.by_name", "contact_ids.#", "1"),
				),
			},
		},
	})
}

func maintenanceConfig(name string, from int64, duration int, stats bool) string {
	return fmt.Sprintf(`
resource "hosttracker_monitor" "covered" {
  type      = "http"
  url       = %q
  name      = "%s-monitor"
  locations = { pools = ["allworld"] }
}

resource "hosttracker_maintenance" "test" {
  name         = %q
  from         = %d
  duration_sec = %d
  timezone     = "Europe/Berlin"

  monitor_ids = [hosttracker_monitor.covered.id]

  suppress = {
    alerts = true
    stats  = %t
  }
}
`, testAddress, name, name, from, duration, stats)
}

// TestAccMaintenance walks the whole lifecycle of a window given as a
// start plus a length, whose end is read back derived.
func TestAccMaintenance(t *testing.T) {
	name := testPrefix() + "-window"
	from := windowStart()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: maintenanceConfig(name, from, 3600, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("hosttracker_maintenance.test", "id"),
					resource.TestCheckResourceAttr("hosttracker_maintenance.test", "name", name),
					resource.TestCheckResourceAttr("hosttracker_maintenance.test", "duration_sec", "3600"),
					resource.TestCheckResourceAttr("hosttracker_maintenance.test", "to", fmt.Sprint(from+3600)),
					resource.TestCheckResourceAttr("hosttracker_maintenance.test", "timezone", "Europe/Berlin"),
					resource.TestCheckResourceAttr("hosttracker_maintenance.test", "monitor_ids.#", "1"),
					resource.TestCheckResourceAttr("hosttracker_maintenance.test", "monitors.#", "1"),
					resource.TestCheckResourceAttr("hosttracker_maintenance.test", "state", "scheduled"),
					resource.TestCheckResourceAttrSet("hosttracker_maintenance.test", "from_rfc3339"),
				),
			},
			{
				// A longer window that only holds back alerting: both the
				// derived end and the coverage must follow.
				Config: maintenanceConfig(name+"-renamed", from, 7200, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("hosttracker_maintenance.test", "name", name+"-renamed"),
					resource.TestCheckResourceAttr("hosttracker_maintenance.test", "duration_sec", "7200"),
					resource.TestCheckResourceAttr("hosttracker_maintenance.test", "to", fmt.Sprint(from+7200)),
					resource.TestCheckResourceAttr("hosttracker_maintenance.test", "suppress.stats", "false"),
				),
			},
			{
				ResourceName:      "hosttracker_maintenance.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// TestAccMaintenancePerMonitorCoverage proves the other spelling of the
// coverage, for a window that treats its monitors differently.
func TestAccMaintenancePerMonitorCoverage(t *testing.T) {
	name := testPrefix() + "-window-mixed"
	from := windowStart()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "hosttracker_monitor" "first" {
  type      = "http"
  url       = %q
  name      = "%s-first"
  locations = { pools = ["allworld"] }
}

resource "hosttracker_maintenance" "mixed" {
  name = %q
  from = %d
  to   = %d

  recurrence = {
    week_days = ["Sunday"]
  }

  monitors = [
    {
      monitor_id = hosttracker_monitor.first.id
      suppress   = { alerts = true, stats = false }
    },
  ]
}

data "hosttracker_maintenance_windows" "scheduled" {
  state = ["scheduled"]
}
`, testAddress, name, name, from, from+1800),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("hosttracker_maintenance.mixed", "monitors.#", "1"),
					resource.TestCheckResourceAttr("hosttracker_maintenance.mixed", "duration_sec", "1800"),
					resource.TestCheckResourceAttr("hosttracker_maintenance.mixed", "recurrence.week_days.#", "1"),
					resource.TestCheckResourceAttrSet("data.hosttracker_maintenance_windows.scheduled", "windows.#"),
				),
			},
		},
	})
}
