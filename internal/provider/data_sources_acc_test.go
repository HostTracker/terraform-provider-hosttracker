package provider_test

import (
	"fmt"
	"testing"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccMonitorDataSources reads one monitor three ways - by id, by url
// and by name - against a monitor the same configuration creates.
func TestAccMonitorDataSources(t *testing.T) {
	name := testPrefix() + "-datasource"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "hosttracker_monitor" "source" {
  type      = "http"
  url       = %q
  name      = %q
  tags      = ["terraform-acc"]
  locations = { pools = ["allworld"] }
}

data "hosttracker_monitor" "by_id" {
  id = hosttracker_monitor.source.id
}

data "hosttracker_monitor" "by_name" {
  lookup_name = hosttracker_monitor.source.name
}

data "hosttracker_monitors" "tagged" {
  tag        = ["terraform-acc"]
  depends_on = [hosttracker_monitor.source]
}
`, testAddress, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("data.hosttracker_monitor.by_id", "id", "hosttracker_monitor.source", "id"),
					resource.TestCheckResourceAttr("data.hosttracker_monitor.by_id", "type", "http"),
					resource.TestCheckResourceAttrPair("data.hosttracker_monitor.by_name", "id", "hosttracker_monitor.source", "id"),
					resource.TestCheckResourceAttrWith("data.hosttracker_monitors.tagged", "monitors.#", atLeastOne),
				),
			},
		},
	})
}

// TestAccReferenceDataSources reads the catalogues that need no resource of
// their own.
func TestAccReferenceDataSources(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
data "hosttracker_monitor_types" "all" {}

data "hosttracker_locations" "all" {}

data "hosttracker_account" "current" {}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith("data.hosttracker_monitor_types.all", "types.#", atLeastOne),
					resource.TestCheckResourceAttrWith("data.hosttracker_locations.all", "pools.#", atLeastOne),
					resource.TestCheckResourceAttrWith("data.hosttracker_locations.all", "agents.#", atLeastOne),
					resource.TestCheckResourceAttrSet("data.hosttracker_account.current", "id"),
				),
			},
		},
	})
}

func atLeastOne(value string) error {
	if value == "" || value == "0" {
		return fmt.Errorf("expected at least one row, got %q", value)
	}
	return nil
}
