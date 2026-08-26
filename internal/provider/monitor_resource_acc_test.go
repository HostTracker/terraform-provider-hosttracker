package provider_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// testAddress is the address the acceptance monitors watch. It is a real
// site that answers, so the monitor concludes rather than sitting down.
const testAddress = "https://www.host-tracker.com"

func testPrefix() string {
	if prefix := os.Getenv("HT_ACC_PREFIX"); prefix != "" {
		return prefix
	}
	return "tfacc"
}

func monitorConfig(name, keywords string, interval int) string {
	return fmt.Sprintf(`
resource "hosttracker_monitor" "test" {
  type     = "http"
  url      = %q
  name     = %q
  interval = %d

  locations = {
    pools = ["allworld"]
  }

  settings = {
    http = {
      keywords        = %q
      follow_redirect = true
    }
  }

  tags = ["terraform-acc"]
}
`, testAddress, name, interval, keywords)
}

// TestAccMonitor walks the whole lifecycle: create, read back, change two
// members, import into a fresh state, and destroy.
func TestAccMonitor(t *testing.T) {
	name := testPrefix() + "-monitor"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: monitorConfig(name, "HostTracker", 300),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("hosttracker_monitor.test", "id"),
					resource.TestCheckResourceAttr("hosttracker_monitor.test", "type", "http"),
					resource.TestCheckResourceAttr("hosttracker_monitor.test", "url", testAddress),
					resource.TestCheckResourceAttr("hosttracker_monitor.test", "name", name),
					resource.TestCheckResourceAttr("hosttracker_monitor.test", "interval", "300"),
					resource.TestCheckResourceAttr("hosttracker_monitor.test", "enabled", "true"),
					resource.TestCheckResourceAttr("hosttracker_monitor.test", "settings.http.keywords", "HostTracker"),
					resource.TestCheckResourceAttr("hosttracker_monitor.test", "locations.pools.#", "1"),
					resource.TestCheckResourceAttrSet("hosttracker_monitor.test", "effective_url"),
					resource.TestCheckResourceAttrSet("hosttracker_monitor.test", "created"),
				),
			},
			{
				// A change to two members, which must reach the API as a
				// PATCH carrying those two and nothing else.
				Config: monitorConfig(name+"-renamed", "Uptime", 600),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("hosttracker_monitor.test", "name", name+"-renamed"),
					resource.TestCheckResourceAttr("hosttracker_monitor.test", "interval", "600"),
					resource.TestCheckResourceAttr("hosttracker_monitor.test", "settings.http.keywords", "Uptime"),
				),
			},
			{
				ResourceName:      "hosttracker_monitor.test",
				ImportState:       true,
				ImportStateVerify: true,
				// The API never publishes these back, so an imported state
				// cannot carry them.
				ImportStateVerifyIgnore: []string{"on_overlimit", "settings.http.asserts_source"},
			},
		},
	})
}

// TestAccMonitorTypeReplacement proves that the immutable type replaces the
// resource rather than trying to patch it.
func TestAccMonitorTypeReplacement(t *testing.T) {
	name := testPrefix() + "-replace"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "hosttracker_monitor" "replaced" {
  type      = "http"
  url       = %q
  name      = %q
  locations = { pools = ["allworld"] }
}
`, testAddress, name),
			},
			{
				Config: fmt.Sprintf(`
resource "hosttracker_monitor" "replaced" {
  type      = "ping"
  url       = "www.host-tracker.com"
  name      = %q
  locations = { pools = ["allworld"] }
}
`, name),
				Check: resource.TestCheckResourceAttr("hosttracker_monitor.replaced", "type", "ping"),
			},
		},
	})
}

// TestAccMonitorSettingsJSON covers the escape hatch a type without a typed
// block uses.
func TestAccMonitorSettingsJSON(t *testing.T) {
	name := testPrefix() + "-json"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "hosttracker_monitor" "json" {
  type      = "http"
  url       = %q
  name      = %q
  locations = { pools = ["allworld"] }

  settings_json = jsonencode({
    keywords       = "HostTracker"
    followRedirect = true
  })
}
`, testAddress, name),
				Check: resource.TestCheckResourceAttrSet("hosttracker_monitor.json", "settings_json"),
			},
		},
	})
}
