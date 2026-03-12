package client

import (
	"github.com/infobloxopen/ibcq-source-oci/internal/harbor"
	"github.com/infobloxopen/ibcq-source-oci/internal/oci"
	"github.com/rs/zerolog"
)

// Client implements schema.ClientMeta for a single target.
type Client struct {
	Logger       zerolog.Logger
	Spec         *Spec
	Target       *TargetSpec
	OCIClient    *oci.Client
	HarborClient *harbor.Client // non-nil only for harbor targets
}

func (c *Client) ID() string {
	if c.Target != nil {
		return c.Target.Name
	}
	return "default"
}
