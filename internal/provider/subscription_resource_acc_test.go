package provider_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// accContactID names the contact the subscription tests alert. A contact
// has to be confirmed out of band before it can be subscribed - a code
// arrives by mail or by SMS and no test can read it - so the contact is
// prepared once on the acceptance account and named here rather than
// created by the test.
func accContactID(t *testing.T) string {
	t.Helper()
	id := os.Getenv("HT_ACC_CONTACT_ID")
	if id == "" {
		t.Skip("HT_ACC_CONTACT_ID must name a confirmed contact on the acceptance account")
	}
	return id
}

func subscriptionConfig(name, contactID, resourceType, attribute, values string) string {
	return fmt.Sprintf(`
resource "hosttracker_monitor" "subject" {
  type = "http"
  url  = %q
  name = %q

  locations = {
    pools = ["allworld"]
  }

  tags = ["terraform-acc"]
}

resource %q "test" {
  monitor_id = hosttracker_monitor.subject.id
  contact_id = %q
  %s         = %s
}
`, testAddress, name, resourceType, contactID, attribute, values)
}

// TestAccAlertSubscription walks create, read back, change the set,
// import by the composite id, and destroy.
func TestAccAlertSubscription(t *testing.T) {
	contactID := accContactID(t)
	name := testPrefix() + "-alert-sub"
	const address = "hosttracker_alert_subscription.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: subscriptionConfig(name, contactID, "hosttracker_alert_subscription",
					"alert_types", `["down"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(address, "id"),
					resource.TestCheckResourceAttr(address, "contact_id", contactID),
					resource.TestCheckResourceAttr(address, "alert_types.#", "1"),
					resource.TestCheckTypeSetElemAttr(address, "alert_types.*", "down"),
					resource.TestCheckResourceAttrSet(address, "created"),
					resource.TestCheckResourceAttrPair(address, "monitor_id", "hosttracker_monitor.subject", "id"),
				),
			},
			{
				// The set is written whole: adding a type replaces it.
				Config: subscriptionConfig(name, contactID, "hosttracker_alert_subscription",
					"alert_types", `["down", "up"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "alert_types.#", "2"),
					resource.TestCheckTypeSetElemAttr(address, "alert_types.*", "up"),
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

// TestAccReportSubscription walks the same lifecycle for reports, which
// are email-only.
func TestAccReportSubscription(t *testing.T) {
	contactID := accContactID(t)
	name := testPrefix() + "-report-sub"
	const address = "hosttracker_report_subscription.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: subscriptionConfig(name, contactID, "hosttracker_report_subscription",
					"frequencies", `["monthly"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(address, "id"),
					resource.TestCheckResourceAttr(address, "frequencies.#", "1"),
					resource.TestCheckTypeSetElemAttr(address, "frequencies.*", "monthly"),
				),
			},
			{
				Config: subscriptionConfig(name, contactID, "hosttracker_report_subscription",
					"frequencies", `["weekly", "monthly"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "frequencies.#", "2"),
					resource.TestCheckTypeSetElemAttr(address, "frequencies.*", "weekly"),
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
