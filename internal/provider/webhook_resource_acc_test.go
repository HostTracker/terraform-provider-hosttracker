package provider_test

import (
	"fmt"
	"testing"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// webhookAddress is where the acceptance webhooks deliver. Nothing answers
// there, which is fine: a webhook's configuration is what is under test,
// and a failed delivery only moves the failure counter.
const webhookAddress = "https://hooks.example.com/terraform-acc"

func webhookConfig(name string, rotate int, enabled bool) string {
	rotation := ""
	if rotate > 0 {
		rotation = fmt.Sprintf("\n  rotate_secret = %d\n", rotate)
	}
	return fmt.Sprintf(`
resource "hosttracker_webhook" "test" {
  url     = %q
  name    = %q
  enabled = %t
  events  = ["monitor.down", "monitor.up"]

  scope = {
    all = true
  }

  headers = [
    {
      header = "X-Terraform-Acc"
      value  = "yes"
    },
  ]
%s}
`, webhookAddress, name, enabled, rotation)
}

func webhookTagScopeConfig(name string) string {
	return fmt.Sprintf(`
resource "hosttracker_webhook" "test" {
  url    = %q
  name   = %q
  events = ["monitor.down"]

  scope = {
    tags = ["terraform-acc"]
  }
}
`, webhookAddress, name)
}

// TestAccWebhook walks the whole lifecycle: create, read back, change the
// scope form, import into a fresh state, and destroy.
func TestAccWebhook(t *testing.T) {
	name := testPrefix() + "-webhook"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: webhookConfig(name, 0, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("hosttracker_webhook.test", "id"),
					resource.TestCheckResourceAttr("hosttracker_webhook.test", "url", webhookAddress),
					resource.TestCheckResourceAttr("hosttracker_webhook.test", "name", name),
					resource.TestCheckResourceAttr("hosttracker_webhook.test", "enabled", "true"),
					resource.TestCheckResourceAttr("hosttracker_webhook.test", "events.#", "2"),
					resource.TestCheckResourceAttr("hosttracker_webhook.test", "scope.all", "true"),
					resource.TestCheckResourceAttr("hosttracker_webhook.test", "headers.#", "1"),
					resource.TestCheckResourceAttrSet("hosttracker_webhook.test", "secret"),
					resource.TestCheckResourceAttr("hosttracker_webhook.test", "secret_set", "true"),
					resource.TestCheckResourceAttr("hosttracker_webhook.test", "consecutive_failures", "0"),
					resource.TestCheckResourceAttrSet("hosttracker_webhook.test", "created"),
				),
			},
			{
				// Switching the scope form sends the whole scope, and the
				// resolved members follow.
				Config: webhookTagScopeConfig(name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("hosttracker_webhook.test", "scope.tags.#", "1"),
					resource.TestCheckNoResourceAttr("hosttracker_webhook.test", "scope.all"),
					resource.TestCheckResourceAttr("hosttracker_webhook.test", "events.#", "1"),
					resource.TestCheckResourceAttrSet("hosttracker_webhook.test", "monitor_count"),
				),
			},
			{
				ResourceName:      "hosttracker_webhook.test",
				ImportState:       true,
				ImportStateVerify: true,
				// The API publishes a secret's value once, at the moment it
				// mints it, so an adopted webhook cannot carry one.
				ImportStateVerifyIgnore: []string{"secret", "rotate_secret"},
			},
		},
	})
}

// TestAccWebhookRotatesTheSecret proves that bumping the counter mints a
// new secret and that the answer's value reaches state.
func TestAccWebhookRotatesTheSecret(t *testing.T) {
	name := testPrefix() + "-webhook-rotate"
	var minted string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: webhookConfig(name, 0, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("hosttracker_webhook.test", "secret"),
					captureAttr("hosttracker_webhook.test", "secret", &minted),
				),
			},
			{
				Config: webhookConfig(name, 1, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("hosttracker_webhook.test", "rotate_secret", "1"),
					resource.TestCheckResourceAttrSet("hosttracker_webhook.test", "secret_previous_valid_until"),
					differsFrom("hosttracker_webhook.test", "secret", &minted),
				),
			},
		},
	})
}

// captureAttr remembers an attribute's value so that a later step can
// compare against it.
func captureAttr(address, attribute string, into *string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		rs, ok := state.RootModule().Resources[address]
		if !ok {
			return fmt.Errorf("%s is not in state", address)
		}
		*into = rs.Primary.Attributes[attribute]
		return nil
	}
}

// differsFrom fails when an attribute still carries a remembered value.
func differsFrom(address, attribute string, previous *string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		rs, ok := state.RootModule().Resources[address]
		if !ok {
			return fmt.Errorf("%s is not in state", address)
		}
		current := rs.Primary.Attributes[attribute]
		if current == "" {
			return fmt.Errorf("%s.%s is empty", address, attribute)
		}
		if current == *previous {
			return fmt.Errorf("%s.%s did not change", address, attribute)
		}
		return nil
	}
}
