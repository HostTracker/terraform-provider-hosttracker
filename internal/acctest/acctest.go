// Package acctest wires the provider into the acceptance-test harness.
//
// Acceptance tests create, change and delete real monitors on the account
// the token belongs to. They run only when TF_ACC is set, and they want an
// account kept for the purpose - never a production one.
package acctest

import (
	"os"
	"testing"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// ProviderFactories serves the provider under test over protocol 6.
var ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"hosttracker": providerserver.NewProtocol6WithError(provider.New("acctest")()),
}

// PreCheck fails a test that would otherwise run without a credential and
// report an unhelpful authentication failure.
func PreCheck(t *testing.T) {
	t.Helper()
	if os.Getenv("HT_TOKEN") == "" {
		t.Fatal("HT_TOKEN must be set for acceptance tests. HT_BASE_URL selects the API; leave it unset for production.")
	}
}
