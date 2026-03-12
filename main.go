package main

import (
	"context"
	"log"

	internalPlugin "github.com/infobloxopen/ibcq-source-oci/plugin"

	"github.com/cloudquery/plugin-sdk/v4/serve"
)

func main() {
	p := serve.Plugin(internalPlugin.Plugin())
	if err := p.Serve(context.Background()); err != nil {
		log.Fatalf("failed to serve plugin: %v", err)
	}
}
