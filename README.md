# CloudQuery OCI Source Plugin

A [CloudQuery](https://www.cloudquery.io) source plugin that syncs container image metadata from OCI-compliant registries, Harbor, and GitHub Container Registry (GHCR) into any CloudQuery destination.

The plugin fetches **metadata only** — manifests, tags, image configs, labels, annotations, referrers, and build history. It never downloads image layers.

## Supported Registries

| Kind | Discovery Methods | Auth Modes |
|------|-------------------|------------|
| `oci` | `catalog`, `static` | `none`, `basic`, `bearer` |
| `harbor` | `harbor_api`, `static` | `basic` |
| `ghcr` | `packages_api`, `static` | `github_pat` |

## Quick Start

### 1. Build the plugin

```bash
make build
```

### 2. Create a config file

```yaml
kind: source
spec:
  name: oci_inventory
  registry: local
  path: ./bin/cq-source-oci
  version: "v0.1.0"
  tables: ["*"]
  destinations: ["postgresql"]
  spec:
    targets:
      - name: my_registry
        kind: oci
        endpoint: http://localhost:5000
        auth:
          mode: none
        discovery:
          source: catalog
```

### 3. Run a sync

```bash
cloudquery sync config.yml
```

## Configuration

The plugin is configured through the `spec` block. It accepts a list of `targets`, each pointing to a registry.

### Target

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | `string` | Yes | — | Unique name for this target |
| `kind` | `string` | No | `oci` | Registry type: `oci`, `harbor`, or `ghcr` |
| `endpoint` | `string` | Yes | — | Registry URL (e.g., `https://harbor.example.com`, `http://localhost:5000`) |
| `auth` | object | No | — | Authentication configuration |
| `discovery` | object | No | — | Repository discovery configuration |
| `harbor` | object | No | — | Harbor-specific options |

### Auth

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `mode` | `string` | `none` | Auth mode: `none`, `basic`, `bearer`, or `github_pat` |
| `username` | `string` | — | Username for `basic` auth |
| `password` | `string` | — | Password for `basic` auth |
| `token` | `string` | — | Token for `bearer` or `github_pat` auth |

### Discovery

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `source` | `string` | `static` | How to discover repositories: `static`, `catalog`, `harbor_api`, or `packages_api` |
| `repositories` | `[]string` | — | Explicit list of repositories (required for `static` discovery) |
| `projects` | `[]string` | — | Harbor projects to scope discovery (for `harbor_api`) |

### Harbor Options

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `include_labels` | `bool` | `false` | Fetch Harbor labels for artifacts |
| `include_accessories` | `bool` | `false` | Fetch Harbor accessories (signatures, SBOMs) |

## Tables

```
oci_repositories
└── oci_artifacts *
    ├── oci_layers
    ├── oci_index_children
    ├── oci_manifest_annotations
    ├── oci_image_configs
    │   ├── oci_image_labels
    │   └── oci_image_history
    └── oci_referrers

oci_tags *
```

Tables marked with `*` support [incremental syncs](https://www.cloudquery.io/docs/advanced-topics/incremental-tables) using `digest` as the incremental key.

## Examples

### Generic OCI registry with catalog discovery

```yaml
spec:
  targets:
    - name: internal_registry
      kind: oci
      endpoint: https://registry.internal.example.com
      auth:
        mode: basic
        username: ${REGISTRY_USER}
        password: ${REGISTRY_PASS}
      discovery:
        source: catalog
```

### Harbor with project scoping

```yaml
spec:
  targets:
    - name: harbor_prod
      kind: harbor
      endpoint: https://harbor.example.com
      auth:
        mode: basic
        username: ${HARBOR_USERNAME}
        password: ${HARBOR_PASSWORD}
      discovery:
        source: harbor_api
        projects:
          - platform
          - apps
      harbor:
        include_labels: true
```

### GitHub Container Registry

```yaml
spec:
  targets:
    - name: ghcr_org
      kind: ghcr
      endpoint: https://ghcr.io
      auth:
        mode: github_pat
        token: ${GHCR_PAT}
      discovery:
        source: static
        repositories:
          - my-org/my-app
          - my-org/my-service
```

### Multiple registries in one sync

```yaml
spec:
  targets:
    - name: harbor
      kind: harbor
      endpoint: https://harbor.example.com
      auth:
        mode: basic
        username: ${HARBOR_USER}
        password: ${HARBOR_PASS}
      discovery:
        source: harbor_api

    - name: ghcr
      kind: ghcr
      endpoint: https://ghcr.io
      auth:
        mode: github_pat
        token: ${GHCR_PAT}
      discovery:
        source: static
        repositories:
          - my-org/my-image
```

## Running with Docker

```bash
docker run --rm -p 7777:7777 ghcr.io/infobloxopen/cq-source-oci:latest
```

The container listens on gRPC port `7777` by default. Override the address:

```bash
docker run --rm -p 8080:8080 ghcr.io/infobloxopen/cq-source-oci:latest serve --address [::]:8080
```

## Development

### Prerequisites

- Go 1.26.1+
- Docker (for container builds)
- k3d (for e2e tests)

### Available Make targets

```
make help                  # Show all targets
make build                 # Build binary to bin/
make test                  # Run lint + unit tests
make test-unit             # Unit tests with race detector
make test-coverage         # Unit tests with coverage report
make lint                  # go vet + gofmt + go fix
make e2e                   # E2E tests (requires k3d cluster)
make docker-build          # Local dev image (cq-source-oci:local)
make docker-build-multiarch # Multi-arch image (amd64+arm64)
make tidy                  # go mod tidy
make clean                 # Remove build artifacts
```

### Running E2E tests

```bash
make setup-e2e       # Create k3d cluster with registries
make e2e             # Run e2e test suite
make teardown-e2e    # Clean up cluster
```

### Creating a release

```bash
make tag-release RELEASE_VERSION=1.2.3
```

This creates and pushes an annotated tag `v1.2.3-<git-hash>`, which triggers the release workflow to build multi-arch images and publish a GitHub Release.