package plugin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudquery/plugin-sdk/v4/message"
	"github.com/cloudquery/plugin-sdk/v4/plugin"
	"github.com/cloudquery/plugin-sdk/v4/scheduler"
	"github.com/cloudquery/plugin-sdk/v4/schema"
	"github.com/infobloxopen/ibcq-source-oci/client"
	"github.com/infobloxopen/ibcq-source-oci/internal/harbor"
	"github.com/infobloxopen/ibcq-source-oci/internal/oci"
	"github.com/infobloxopen/ibcq-source-oci/resources"
	"github.com/rs/zerolog"
)

const (
	pluginName    = "oci-source"
	pluginVersion = "v0.1.0"
)

func Plugin() *plugin.Plugin {
	return plugin.NewPlugin(
		pluginName,
		pluginVersion,
		Configure,
		plugin.WithKind("source"),
	)
}

type PluginClient struct {
	plugin.UnimplementedDestination
	logger    zerolog.Logger
	spec      *client.Spec
	tables    schema.Tables
	scheduler *scheduler.Scheduler
	clients   []*client.Client
}

func Configure(ctx context.Context, logger zerolog.Logger, specBytes []byte, opts plugin.NewClientOptions) (plugin.Client, error) {
	var spec client.Spec
	if err := json.Unmarshal(specBytes, &spec); err != nil {
		return nil, fmt.Errorf("failed to unmarshal spec: %w", err)
	}
	spec.SetDefaults()
	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("invalid spec: %w", err)
	}

	tables := resources.Tables()

	s := scheduler.NewScheduler(
		scheduler.WithLogger(logger),
		scheduler.WithConcurrency(1000),
	)

	var clients []*client.Client
	for i := range spec.Targets {
		t := &spec.Targets[i]
		ociClient := oci.NewClient(
			t.Endpoint,
			t.Auth.Mode,
			t.Auth.Username,
			t.Auth.Password,
			t.Auth.Token,
			logger,
		)
		cl := &client.Client{
			Logger:    logger,
			Spec:      &spec,
			Target:    t,
			OCIClient: ociClient,
		}
		if t.Kind == "harbor" {
			cl.HarborClient = harbor.NewClient(t.Endpoint, t.Auth.Username, t.Auth.Password)
		}
		clients = append(clients, cl)
	}

	return &PluginClient{
		logger:    logger,
		spec:      &spec,
		tables:    tables,
		scheduler: s,
		clients:   clients,
	}, nil
}

func (c *PluginClient) Tables(ctx context.Context, options plugin.TableOptions) (schema.Tables, error) {
	return c.tables, nil
}

func (c *PluginClient) Sync(ctx context.Context, options plugin.SyncOptions, res chan<- message.SyncMessage) error {
	for _, cl := range c.clients {
		tt, err := c.tables.FilterDfs(options.Tables, options.SkipTables, options.SkipDependentTables)
		if err != nil {
			return err
		}
		if err := c.scheduler.Sync(ctx, cl, tt, res, scheduler.WithSyncDeterministicCQID(options.DeterministicCQID)); err != nil {
			return fmt.Errorf("sync target %s: %w", cl.ID(), err)
		}
	}
	return nil
}

func (c *PluginClient) Close(ctx context.Context) error {
	return nil
}
