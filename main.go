package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/neondatabase/terraform-provider-neon/provider"
)

// Run "go generate" to format example terraform files and generate the docs for the registry/website

// If you do not have terraform installed, you can remove the formatting command, but its suggested to
// ensure the documentation is formatted properly.
//go:generate terraform fmt -recursive ./examples/

// Run the docs generation tool, check its repository for more information on how it works and how docs
// can be customized. The provider name is explicit so checkouts in any directory, such as git
// worktrees, keep the same page titles instead of using the directory name.
//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-name terraform-provider-neon

var (
	// these will be set by the goreleaser configuration
	// to appropriate values for the compiled binary.
	version string = "0.0.0-alpha.0"
)

func main() {
	var debugMode bool

	flag.BoolVar(&debugMode, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/" + provider.Name,
		Debug:   debugMode,
	})
	if err != nil {
		log.Fatal(err)
	}
}
