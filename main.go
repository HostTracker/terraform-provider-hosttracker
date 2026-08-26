// terraform-provider-hosttracker manages HostTracker monitoring as code.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

// version is stamped by the release build. It rides the User-Agent, so an
// unstamped build is visible in the API logs as "dev".
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run the provider with support for debuggers such as delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/HostTracker/hosttracker",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
