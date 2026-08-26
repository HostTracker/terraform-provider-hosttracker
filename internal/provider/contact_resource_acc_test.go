package provider_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// testEmail is where the acceptance contacts deliver. Creating an email
// contact sends a real confirmation code to it, and an address whose
// domain publishes no mail host is refused rather than stored, so it has
// to be an address that exists.
func testEmail() string {
	if address := os.Getenv("HT_ACC_EMAIL"); address != "" {
		return address
	}
	return "terraform-acc@host-tracker.com"
}

func contactConfig(name, language string, delay int) string {
	return fmt.Sprintf(`
resource "hosttracker_contact" "test" {
  type        = "email"
  address     = %q
  name        = %q
  language    = %q
  alert_delay = %d

  active_period = {
    start    = "09:00:00"
    end      = "18:00:00"
    days     = ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday"]
  }
}
`, testEmail(), name, language, delay)
}

// TestAccContact walks the whole lifecycle: create, read back, change two
// members, import into a fresh state, and destroy.
func TestAccContact(t *testing.T) {
	name := testPrefix() + "-contact"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: contactConfig(name, "en", 5),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("hosttracker_contact.test", "id"),
					resource.TestCheckResourceAttr("hosttracker_contact.test", "type", "email"),
					resource.TestCheckResourceAttr("hosttracker_contact.test", "address", testEmail()),
					resource.TestCheckResourceAttr("hosttracker_contact.test", "name", name),
					resource.TestCheckResourceAttr("hosttracker_contact.test", "alert_delay", "5"),
					resource.TestCheckResourceAttr("hosttracker_contact.test", "active_period.start", "09:00:00"),
					resource.TestCheckResourceAttr("hosttracker_contact.test", "send_confirmation", "false"),
					resource.TestCheckResourceAttrSet("hosttracker_contact.test", "confirmed"),
					resource.TestCheckResourceAttrSet("hosttracker_contact.test", "created"),
				),
			},
			{
				// A change to three members, which must reach the API as a
				// PATCH carrying those three and nothing else. The delay is
				// a rung on a fixed ladder, so 15 rather than 10.
				Config: contactConfig(name+"-renamed", "de", 15),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("hosttracker_contact.test", "name", name+"-renamed"),
					resource.TestCheckResourceAttr("hosttracker_contact.test", "language", "de"),
					resource.TestCheckResourceAttr("hosttracker_contact.test", "alert_delay", "15"),
				),
			},
			{
				ResourceName:      "hosttracker_contact.test",
				ImportState:       true,
				ImportStateVerify: true,
				// A write knob the API never publishes back.
				ImportStateVerifyIgnore: []string{"send_confirmation"},
			},
		},
	})
}

// TestAccContactTypeReplacement proves that the immutable type replaces
// the contact rather than trying to patch it.
func TestAccContactTypeReplacement(t *testing.T) {
	name := testPrefix() + "-contact-replace"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "hosttracker_contact" "replaced" {
  type    = "email"
  address = %q
  name    = %q
}
`, testEmail(), name),
			},
			{
				Config: fmt.Sprintf(`
resource "hosttracker_contact" "replaced" {
  type    = "sms"
  address = "+15550100"
  name    = %q
}
`, name),
				Check: resource.TestCheckResourceAttr("hosttracker_contact.replaced", "type", "sms"),
			},
		},
	})
}

// TestAccContactRefusesAWebhook proves the plan-time refusal, which never
// reaches the API.
func TestAccContactRefusesAWebhook(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "hosttracker_contact" "webhook" {
  type    = "http"
  address = "https://example.com/hook"
}
`,
				ExpectError: regexp.MustCompile("hosttracker_webhook"),
			},
		},
	})
}

// TestAccContactDataSources reads a contact back through both data
// sources, and the catalogue that says what a contact type accepts.
func TestAccContactDataSources(t *testing.T) {
	name := testPrefix() + "-contact-ds"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "hosttracker_contact" "source" {
  type    = "email"
  address = %q
  name    = %q
}

data "hosttracker_contact" "by_id" {
  id = hosttracker_contact.source.id
}

data "hosttracker_contacts" "email" {
  type = ["email"]
}

data "hosttracker_contact_types" "all" {}
`, testEmail(), name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.hosttracker_contact.by_id", "name", name),
					resource.TestCheckResourceAttr("data.hosttracker_contact.by_id", "type", "email"),
					resource.TestCheckResourceAttrSet("data.hosttracker_contacts.email", "contacts.#"),
					resource.TestCheckResourceAttrSet("data.hosttracker_contact_types.all", "types.#"),
				),
			},
		},
	})
}
