package main

import (
	"flag"
	"log"

	"terraform-provider-hpu/internal/provider"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/plugin"
)

func main() {

	var debugMode bool
	var pluginPath string

	log.Print("this is the main")

	flag.BoolVar(&debugMode, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.StringVar(&pluginPath, "registry", "terraform-provider-hpu", "specify path, useful for local debugging")
	flag.Parse()

	opts := &plugin.ServeOpts{ProviderFunc: func() *schema.Provider {
		return provider.Provider()
	}, Debug: debugMode, ProviderAddr: pluginPath}

	plugin.Serve(opts)
}
