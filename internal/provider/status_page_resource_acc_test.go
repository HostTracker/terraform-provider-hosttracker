package provider_test

import (
	"fmt"
	"testing"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func statusPageConfig(slug, title, components string) string {
	return fmt.Sprintf(`
resource "hosttracker_monitor" "published" {
  type = "http"
  url  = %q
  name = "%s subject"

  locations = {
    pools = ["allworld"]
  }

  tags = ["terraform-acc"]
}

resource "hosttracker_status_page" "test" {
  slug  = %q
  title = %q

  settings = {
    theme        = "light"
    show_groups  = true
    robots_index = false
    features     = ["barCharts", "uptimePercent"]
  }

%s
}
`, testAddress, slug, slug, title, components)
}

const statusPageOneComponent = `  components = [
    {
      monitor_id = hosttracker_monitor.published.id
      group      = "Public"
    },
  ]`

const statusPageTwoComponents = `  components = [
    {
      third_party  = true
      name         = "Payments provider"
      manual_state = "operational"
    },
    {
      monitor_id = hosttracker_monitor.published.id
      group      = "Public"
    },
  ]`

// TestAccStatusPage walks the whole lifecycle: create with a component,
// change the title and the component set, import into a fresh state, and
// destroy.
func TestAccStatusPage(t *testing.T) {
	slug := testPrefix() + "-status"
	const address = "hosttracker_status_page.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: statusPageConfig(slug, "Acceptance status", statusPageOneComponent),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(address, "id"),
					resource.TestCheckResourceAttr(address, "slug", slug),
					resource.TestCheckResourceAttr(address, "title", "Acceptance status"),
					resource.TestCheckResourceAttr(address, "settings.theme", "light"),
					resource.TestCheckResourceAttr(address, "settings.features.#", "2"),
					resource.TestCheckResourceAttr(address, "components.#", "1"),
					resource.TestCheckResourceAttr(address, "components.0.group", "Public"),
					resource.TestCheckResourceAttr(address, "component_count", "1"),
					resource.TestCheckResourceAttr(address, "has_password", "false"),
					resource.TestCheckResourceAttr(address, "public_url",
						"https://status.host-tracker.com/"+slug),
					resource.TestCheckResourceAttrSet(address, "created"),
				),
			},
			{
				// The component set is a snapshot: the new order and the
				// added third-party row replace what was there.
				Config: statusPageConfig(slug, "Acceptance status, live", statusPageTwoComponents),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "title", "Acceptance status, live"),
					resource.TestCheckResourceAttr(address, "components.#", "2"),
					resource.TestCheckResourceAttr(address, "components.0.third_party", "true"),
					resource.TestCheckResourceAttr(address, "components.0.manual_state", "operational"),
					resource.TestCheckResourceAttrPair(address, "components.1.monitor_id",
						"hosttracker_monitor.published", "id"),
					resource.TestCheckResourceAttr(address, "component_count", "2"),
				),
			},
			{
				ResourceName:      address,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
