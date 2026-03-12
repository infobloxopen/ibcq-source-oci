I’d build this as a **hybrid CloudQuery source plugin in Go**, not as an “OCI-only crawler.”

The shape I’d use is:

1. **Provider-native discovery**

   * Harbor: enumerate projects, repositories, artifacts, Harbor labels, tags, accessories, and additions through the Harbor API.
   * GHCR: enumerate package namespaces and versions through the GitHub Packages REST API.
   * Generic OCI: inspect configured repositories, because the standardized OCI Distribution endpoints cover `/v2/`, manifests, tags, and referrers, but not a portable repository catalog; so pure OCI alone is not enough for global discovery. CloudQuery’s Go SDK fits this well because table resolvers can fetch top-level resources, dependent resolvers can enrich child rows, and multiplexers can parallelize one configured target at a time; incremental tables use cursor state in the state backend. ([CloudQuery][1])

2. **OCI-native inspection**

   * For every discovered digest, use the registry API for manifest/index inspection, referrers, and optionally the config blob.
   * Start with `GET /v2/`, then follow the standard auth challenge flow (`401` + `WWW-Authenticate` + bearer token retry) so one transport works for Harbor, GHCR, and generic registries. ([GitHub][2])

3. **Provider-specific enrichment**

   * Harbor-specific: labels, accessories, build history, chart additions.
   * GHCR-specific: package/version metadata and GitHub-visible annotation metadata.
   * Generic OCI-specific: referrers and artifact classification from `artifactType`, `config.mediaType`, media types, annotations, and config labels. ([GitHub][3])

### What you can discover without downloading layers

You can discover **image manifests, image indexes, layer descriptors, config descriptors, tags, and referrers** without downloading layer blobs. The manifest already gives you layer digests, sizes, and media types; referrers gives you attached artifacts like signatures and SBOMs when the registry supports it, with a tag-schema fallback when it does not. ([GitHub][2])

For your specific asks, there is one important exception: **OCI labels created during `docker build` and best-effort Dockerfile reconstruction both require fetching the image config blob**. The OCI image config contains `config.Labels`, and the history array contains `created_by` plus `empty_layer`, which is the closest standard source for reconstructing build steps. That is still **not** a layer download; it is a small metadata blob. Harbor can also expose build history directly, and Helm OCI artifacts can expose chart metadata through the OCI config blob because Helm’s OCI config is a JSON representation of `Chart.yaml`. The full original Dockerfile is not a standard first-class OCI object, so treat it as **best effort**, not guaranteed. ([GitHub][4])

### The resolver tree I’d use

CloudQuery’s dependent resolver model maps very naturally to this:

```text
oci_targets                      (top-level, multiplexed per configured target)
└── oci_repositories             (top-level per target)
    ├── oci_artifacts            (one row per digest in repo)
    │   ├── oci_layers
    │   ├── oci_index_children
    │   ├── oci_manifest_annotations
    │   ├── oci_image_configs
    │   │   ├── oci_image_labels
    │   │   └── oci_image_history
    │   ├── oci_referrers
    │   ├── oci_attached_artifacts
    │   ├── harbor_accessories      (Harbor only)
    │   └── harbor_additions        (Harbor only)
    └── oci_tags

harbor_projects                 (Harbor only)
harbor_labels                   (Harbor only)
└── harbor_artifact_labels      (join table)
```

That gives you a normalized cross-registry core and still keeps Harbor-specific metadata in first-class tables. CloudQuery calls dependent resolvers once per parent row, which is exactly what you want for repo → artifact → config/referrer enrichment. ([CloudQuery][1])

### The tables I would actually expose

Core tables:

* `oci_repositories`
* `oci_artifacts`
* `oci_tags`
* `oci_layers`
* `oci_index_children`
* `oci_manifest_annotations`
* `oci_image_configs`
* `oci_image_labels`
* `oci_image_history`
* `oci_referrers`
* `oci_attached_artifacts`
* `oci_helm_chart_metadata`

Harbor extension tables:

* `harbor_projects`
* `harbor_labels`
* `harbor_artifact_labels`
* `harbor_accessories`
* `harbor_additions`

I would also keep a `raw_json` column on the main artifact/config rows so future artifact types do not force a schema migration every time.

### How I’d model the three different “label/tag” spaces

You need three separate concepts in the schema:

1. **OCI image labels**
   These come from `config.Labels` in the image config blob. They belong in `oci_image_labels`. This is where Dockerfile `LABEL` data lives. ([GitHub][4])

2. **OCI manifest annotations**
   These live on manifests, indexes, or descriptors, and GHCR surfaces some predefined annotation keys on package pages; for multi-arch images, GHCR documents that the description comes from manifest annotations. I would store these separately in `oci_manifest_annotations`. ([GitHub Docs][5])

3. **Harbor labels**
   These are Harbor-managed labels, not OCI labels. Harbor exposes global (`g`) and project (`p`) labels and lets you attach them to artifacts, so these belong in `harbor_labels` plus `harbor_artifact_labels`. Harbor tags also deserve their own row space because Harbor lets you tag parent and child artifacts independently and tags do not change the artifact digest. ([GitHub][3])

I would also add `normalize_legacy_label_schema: true` as a config option, because OCI notes that old `org.label-schema.*` conventions are superseded by `org.opencontainers.image.*`, and tools may choose to support compatible mappings. ([GitHub][6])

### Harbor adapter design

For Harbor, use Harbor as the **authoritative discovery plane**. It has exactly the metadata you need:

* repository enumeration via `/repositories` or `/projects/{project}/repositories`
* artifact listing via `/projects/{project}/repositories/{repo}/artifacts`
* artifact responses already include digest, media type, manifest media type, artifact type, tags, labels, annotations, references, scan overview, and addition links
* optional flags exist for `with_tag`, `with_label`, `with_scan_overview`, and `with_sbom_overview`
* labels are listed via `/labels` with global/project scope
* accessories are listed per artifact
* addition types include `build_history`, `values.yaml`, `readme.md`, `dependencies`, `sbom`, `license`, and `files` ([GitHub][3])

That means Harbor can do more than plain OCI for your use case:

* **Helm charts** are first-class artifacts in Harbor’s OCI-compatible registry. ([Harbor][7])
* **Harbor labels** are first-class, with global and project scope. ([GitHub][3])
* **Cosign/Notation signatures** are supported in Harbor, and Harbor exposes accessories for attached items. ([Harbor][8])
* **SBOM support** exists in Harbor and is reflected in SBOM overview/accessory concepts. ([Harbor][9])
* **Build history** is explicitly available in Harbor and is the best Harbor-side answer to “Dockerfile definition if available.” ([Harbor][10])

So for Harbor I would default to:

* discover additions by presence only from `addition_links`
* fetch actual addition content only for explicitly requested small additions such as `build_history`, `dependencies`, or `sbom`

That keeps you in metadata-only mode.

### GHCR adapter design

For GHCR, use a **dual path**:

* **GitHub Packages REST API** for enumeration
  List packages for the org/user, then list package versions. The package version payload includes a digest-like `name` (`sha256:...`), `created_at`, `updated_at`, and `metadata.container.tags`. This is your best incremental discovery source. Access to package metadata uses GitHub Packages auth with `read:packages`. GHCR also supports anonymous access for public container images at the registry layer. ([GitHub Docs][11])

* **GHCR registry API** for inspection
  Once you know the repo and digest/tag, inspect manifests, indexes, referrers, and config blobs through `ghcr.io`. GHCR’s container registry stores Docker and OCI images, and GitHub documents supported annotation keys that are surfaced from image metadata. ([GitHub Docs][5])

One caveat: GitHub’s package enumeration is explicitly centered on `package_type=container`. Because of that, I would **not assume** GHCR package enumeration is sufficient for every non-image OCI artifact shape. For Helm charts or unusual OCI artifacts in GHCR, I would let the GHCR target accept `extra_repositories` or a fallback static-repository mode so the generic OCI inspector can still inspect them. That is a design hedge based on the documented container-package focus. ([GitHub Docs][11])

### Generic OCI adapter design

For a generic OCI registry, I would make repository discovery explicit:

* `discovery.source: static`
* optionally `discovery.source: extension_catalog` only when the user opts into non-standard endpoints
* never rely on pure OCI for repo enumeration, because the standardized endpoint set does not define it. ([GitHub][2])

Once a repository is known, generic OCI is very good for inspection:

* tags via `/v2/<name>/tags/list`
* manifests/indexes via `/v2/<name>/manifests/<reference>`
* referrers via `/v2/<name>/referrers/<digest>`
* config blobs via `/v2/<name>/blobs/<digest>` only when the descriptor is the config blob, not a layer blob ([GitHub][2])

I would set `referrers.mode: auto`, meaning:

* use the referrers API when present
* if it returns `404`, fall back to the referrers tag schema exactly as the distribution spec recommends. ([GitHub][2])

### Incremental strategy

This is the part where the “tag keys don’t change” assumption should become **two separate knobs**:

* `assume_tag_names_stable`
* `assume_tag_targets_immutable`

Those are not the same thing.

For **Harbor**, use repository `update_time` and artifact `push_time` as cursor candidates, with an overlap window and a periodic full reconcile. Harbor artifact rows already expose the timestamps you need. ([GitHub][3])

For **GHCR**, use `(updated_at, package_version_id)` as the incremental cursor, because package versions expose `updated_at` and tags in the version metadata. ([GitHub Docs][11])

For **generic OCI**, do **not** use “largest tag name” as a cursor. The spec says tag pagination is lexical/ASCIIbetical and paginated by `Link` / `last`, not chronological. So the safe default is:

* page through the tag list
* compute a tag-set diff
* resolve manifests only for new tags
* if `assume_tag_targets_immutable: false`, periodically re-resolve known tags too. The spec also says clients should prefer the `Link` header when available. ([GitHub][2])

Because CloudQuery incremental tables are at-least-once, I would recommend running this source with a destination write mode that tolerates duplicates and can delete stale rows, especially for tags and attachments. ([CloudQuery][12])

### Artifact classification

Classification should be **configurable**, but the default classifier should use:

1. provider-native type if present (`Harbor Artifact.type` / `artifact_type`)
2. OCI manifest `artifactType`
3. fallback to `config.mediaType` when `artifactType` is absent
4. layer media type heuristics
5. provider-specific accessory/addition hints ([GitHub][3])

That matters for Helm in particular, because Helm documents:

* config media type `application/vnd.cncf.helm.config.v1+json`, which is the JSON form of `Chart.yaml`
* chart content media type `application/vnd.cncf.helm.chart.content.v1.tar+gzip` ([Helm][13])

So `oci_helm_chart_metadata` can be populated from the config blob without downloading the chart layer.

---

CloudQuery source configs use `kind: source` with `spec.name`, `path`, `registry`, `version`, `tables`, `destinations`, and a plugin-specific `spec`. For a Go plugin, I would ship it as `registry: local` during development; if you later package it as a container image, CloudQuery also supports `registry: docker` and `docker_registry_auth_token`, including GHCR auth via username + PAT. ([CloudQuery][14])

```yaml
kind: source
spec:
  name: oci_inventory
  registry: local
  path: ./dist/cq-source-oci
  version: "v0.1.0"
  tables:
    - oci_repositories
    - oci_artifacts
    - oci_tags
    - oci_layers
    - oci_index_children
    - oci_manifest_annotations
    - oci_image_configs
    - oci_image_labels
    - oci_image_history
    - oci_referrers
    - oci_attached_artifacts
    - oci_helm_chart_metadata
    - harbor_projects
    - harbor_labels
    - harbor_artifact_labels
    - harbor_accessories
    - harbor_additions
  destinations: ["postgresql"]
  spec:
    defaults:
      request_timeout: 30s
      page_size: 100
      max_concurrency: 8

      fetch:
        manifests: true
        referrers: true
        config_blob: true          # needed for OCI labels, image history, Helm Chart.yaml
        harbor_addition_content: [] # e.g. ["build_history", "dependencies", "sbom"]

      dockerfile:
        mode: best_effort          # none | history | harbor_build_history | both

      tags:
        assume_tag_names_stable: true
        assume_tag_targets_immutable: true
        revalidate_after: 168h
        full_reconcile_every: 24h

      referrers:
        mode: auto                 # auto | api | tag_schema | off

      labels:
        normalize_legacy_label_schema: true

      classification:
        fallback_to_config_media_type: true
        custom_rules:
          - kind: helm_chart
            match:
              config_media_type: application/vnd.cncf.helm.config.v1+json
          - kind: helm_chart
            match:
              layer_media_type: application/vnd.cncf.helm.chart.content.v1.tar+gzip

    targets:
      - name: harbor_prod
        kind: harbor
        endpoint: https://harbor.example.com
        auth:
          mode: basic
          username: ${HARBOR_USERNAME}
          password: ${HARBOR_PASSWORD}

        discovery:
          projects: ["team-a", "team-b"]
          repositories: ["**"]

        harbor:
          include_labels: true
          include_accessories: true
          include_sbom_overview: true
          include_scan_overview: false
          discover_additions_only: true
          fetch_addition_content: ["build_history"]

        incremental:
          repositories:
            cursor: update_time
            overlap: 10m
          artifacts:
            cursor: push_time
            overlap: 10m

      - name: ghcr_org
        kind: ghcr
        endpoint: https://ghcr.io
        namespace_type: organization
        namespace: my-org
        auth:
          mode: github_pat
          token: ${GHCR_PAT}

        discovery:
          source: packages_api
          include_packages: ["*"]
          extra_repositories: []   # use this for chart/extra OCI repos if needed

        incremental:
          package_versions:
            cursor: updated_at
            overlap: 30m

      - name: vendor_registry
        kind: oci
        endpoint: https://registry.example.com
        auth:
          mode: bearer
          token: ${OCI_TOKEN}

        discovery:
          source: static
          repositories:
            - team-a/service-a
            - team-a/service-b
          allow_nonstandard_catalog: false

        incremental:
          tags:
            strategy: tag_set_diff
```

The one thing I would make **non-optional in v1** is `fetch.config_blob: true`. Without that, you lose the OCI labels from Docker build, the history needed for best-effort Dockerfile reconstruction, and the Helm `Chart.yaml` metadata path, even though you are still not downloading any layer blobs. ([GitHub][4])

If I were sequencing implementation, I’d do it in this order: **Harbor + GHCR discovery first, core OCI inspection second, config/label/history extraction third, Harbor additions and deeper attestation parsing last.**

[1]: https://docs.cloudquery.io/docs/integrations/creating-new-integration/go-source "https://docs.cloudquery.io/docs/integrations/creating-new-integration/go-source"
[2]: https://github.com/opencontainers/distribution-spec/blob/main/spec.md?plain=1 "https://github.com/opencontainers/distribution-spec/blob/main/spec.md?plain=1"
[3]: https://raw.githubusercontent.com/goharbor/harbor/main/api/v2.0/swagger.yaml "https://raw.githubusercontent.com/goharbor/harbor/main/api/v2.0/swagger.yaml"
[4]: https://github.com/opencontainers/image-spec/blob/main/config.md "https://github.com/opencontainers/image-spec/blob/main/config.md"
[5]: https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry "https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry"
[6]: https://github.com/opencontainers/image-spec/blob/main/annotations.md "https://github.com/opencontainers/image-spec/blob/main/annotations.md"
[7]: https://goharbor.io/docs/2.14.0/working-with-projects/working-with-images/repositories/ "https://goharbor.io/docs/2.14.0/working-with-projects/working-with-images/repositories/"
[8]: https://goharbor.io/docs/main/working-with-projects/working-with-images/sign-images/ "https://goharbor.io/docs/main/working-with-projects/working-with-images/sign-images/"
[9]: https://goharbor.io/docs/main/administration/sbom-integration/ "https://goharbor.io/docs/main/administration/sbom-integration/"
[10]: https://goharbor.io/docs/2.14.0/working-with-projects/project-configuration/ "https://goharbor.io/docs/2.14.0/working-with-projects/project-configuration/"
[11]: https://docs.github.com/en/rest/packages/packages "https://docs.github.com/en/rest/packages/packages"
[12]: https://docs.cloudquery.io/docs/advanced/managing-incremental-tables "https://docs.cloudquery.io/docs/advanced/managing-incremental-tables"
[13]: https://helm.sh/community/hips/hip-0017/ "https://helm.sh/community/hips/hip-0017/"
[14]: https://docs.cloudquery.io/docs/integrations/sources "https://docs.cloudquery.io/docs/integrations/sources"

