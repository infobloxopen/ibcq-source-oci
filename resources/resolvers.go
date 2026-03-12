package resources

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudquery/plugin-sdk/v4/schema"
	"github.com/infobloxopen/ibcq-source-oci/client"
	"github.com/infobloxopen/ibcq-source-oci/internal/oci"
)

func fetchRepositories(ctx context.Context, meta schema.ClientMeta, parent *schema.Resource, res chan<- any) error {
	cl := meta.(*client.Client)
	target := cl.Target

	var repos []string
	var err error

	switch {
	case target.Kind == "harbor" && cl.HarborClient != nil:
		repos, err = discoverHarborRepos(ctx, cl)
		if err != nil {
			return fmt.Errorf("harbor discovery: %w", err)
		}
	case target.Discovery.Source == "catalog":
		repos, err = cl.OCIClient.Catalog(ctx)
		if err != nil {
			return fmt.Errorf("catalog discovery: %w", err)
		}
	default:
		repos = target.Discovery.Repositories
	}

	for _, repo := range repos {
		res <- &Repository{
			TargetName: target.Name,
			Registry:   target.Endpoint,
			Name:       repo,
			FullName:   repo,
		}
	}
	return nil
}

func discoverHarborRepos(ctx context.Context, cl *client.Client) ([]string, error) {
	target := cl.Target
	var repos []string

	// If specific projects are configured, enumerate those
	projects := target.Discovery.Projects
	if len(projects) == 0 {
		// Discover all projects
		harborProjects, err := cl.HarborClient.ListProjects(ctx)
		if err != nil {
			return nil, err
		}
		for _, p := range harborProjects {
			projects = append(projects, p.Name)
		}
	}

	for _, project := range projects {
		harborRepos, err := cl.HarborClient.ListRepositories(ctx, project)
		if err != nil {
			cl.Logger.Warn().Err(err).Str("project", project).Msg("failed to list harbor repositories")
			continue
		}
		for _, r := range harborRepos {
			repos = append(repos, r.Name)
		}
	}

	// Also include any statically configured repos
	repos = append(repos, target.Discovery.Repositories...)
	return repos, nil
}

func fetchArtifacts(ctx context.Context, meta schema.ClientMeta, parent *schema.Resource, res chan<- any) error {
	cl := meta.(*client.Client)
	repo := parent.Item.(*Repository)

	tags, err := cl.OCIClient.ListTags(ctx, repo.Name)
	if err != nil {
		return fmt.Errorf("list tags for %s: %w", repo.Name, err)
	}

	// Track digests we've already seen to avoid duplicates (multiple tags can point to same digest)
	seen := make(map[string]bool)

	for _, tag := range tags {
		manifest, err := cl.OCIClient.GetManifest(ctx, repo.Name, tag)
		if err != nil {
			cl.Logger.Warn().Err(err).Str("repo", repo.Name).Str("tag", tag).Msg("failed to get manifest")
			continue
		}

		digest := manifest.Digest
		if digest == "" {
			digest = computeDigest(manifest.Body)
		}

		if seen[digest] {
			continue
		}
		seen[digest] = true

		mediaType := manifest.MediaType
		isIndex := isIndexMediaType(mediaType)

		artifactType := ""
		if isIndex {
			var idx oci.ImageIndex
			if err := json.Unmarshal(manifest.Body, &idx); err == nil {
				artifactType = idx.ArtifactType
			}
		} else {
			var m oci.ImageManifest
			if err := json.Unmarshal(manifest.Body, &m); err == nil {
				artifactType = m.ArtifactType
				if artifactType == "" {
					artifactType = m.Config.MediaType
				}
			}
		}

		res <- &Artifact{
			TargetName:   cl.Target.Name,
			Registry:     cl.Target.Endpoint,
			Repository:   repo.Name,
			Digest:       digest,
			MediaType:    mediaType,
			ArtifactType: artifactType,
			Size:         manifest.Size,
			IsIndex:      isIndex,
			RawJSON:      manifest.Body,
		}

		// For image indexes, also resolve each child manifest so layers/configs are captured
		if isIndex {
			var idx oci.ImageIndex
			if err := json.Unmarshal(manifest.Body, &idx); err != nil {
				continue
			}
			for _, child := range idx.Manifests {
				if seen[child.Digest] {
					continue
				}
				seen[child.Digest] = true

				childManifest, err := cl.OCIClient.GetManifest(ctx, repo.Name, child.Digest)
				if err != nil {
					cl.Logger.Warn().Err(err).Str("digest", child.Digest).Msg("failed to get child manifest")
					continue
				}

				childMediaType := childManifest.MediaType
				childIsIndex := isIndexMediaType(childMediaType)
				childArtifactType := ""
				if !childIsIndex {
					var m oci.ImageManifest
					if err := json.Unmarshal(childManifest.Body, &m); err == nil {
						childArtifactType = m.ArtifactType
						if childArtifactType == "" {
							childArtifactType = m.Config.MediaType
						}
					}
				}

				res <- &Artifact{
					TargetName:   cl.Target.Name,
					Registry:     cl.Target.Endpoint,
					Repository:   repo.Name,
					Digest:       child.Digest,
					MediaType:    childMediaType,
					ArtifactType: childArtifactType,
					Size:         childManifest.Size,
					IsIndex:      childIsIndex,
					RawJSON:      childManifest.Body,
				}
			}
		}
	}
	return nil
}

func fetchTags(ctx context.Context, meta schema.ClientMeta, parent *schema.Resource, res chan<- any) error {
	cl := meta.(*client.Client)
	target := cl.Target

	// Discover repos first
	var repos []string
	switch target.Discovery.Source {
	case "catalog":
		var err error
		repos, err = cl.OCIClient.Catalog(ctx)
		if err != nil {
			return fmt.Errorf("catalog discovery: %w", err)
		}
	default:
		repos = target.Discovery.Repositories
	}

	for _, repo := range repos {
		tags, err := cl.OCIClient.ListTags(ctx, repo)
		if err != nil {
			cl.Logger.Warn().Err(err).Str("repo", repo).Msg("failed to list tags")
			continue
		}
		for _, tag := range tags {
			manifest, err := cl.OCIClient.GetManifest(ctx, repo, tag)
			if err != nil {
				cl.Logger.Warn().Err(err).Str("repo", repo).Str("tag", tag).Msg("failed to get manifest for tag")
				continue
			}
			digest := manifest.Digest
			if digest == "" {
				digest = computeDigest(manifest.Body)
			}
			res <- &Tag{
				TargetName: target.Name,
				Registry:   target.Endpoint,
				Repository: repo,
				Name:       tag,
				Digest:     digest,
			}
		}
	}
	return nil
}

func fetchLayers(ctx context.Context, meta schema.ClientMeta, parent *schema.Resource, res chan<- any) error {
	cl := meta.(*client.Client)
	artifact := parent.Item.(*Artifact)

	if artifact.IsIndex {
		return nil
	}

	var m oci.ImageManifest
	if err := json.Unmarshal(artifact.RawJSON, &m); err != nil {
		return nil
	}

	for i, layer := range m.Layers {
		res <- &Layer{
			TargetName:   cl.Target.Name,
			Registry:     cl.Target.Endpoint,
			Repository:   artifact.Repository,
			Digest:       layer.Digest,
			ParentDigest: artifact.Digest,
			MediaType:    layer.MediaType,
			Size:         layer.Size,
			Index:        i,
		}
	}
	return nil
}

func fetchIndexChildren(ctx context.Context, meta schema.ClientMeta, parent *schema.Resource, res chan<- any) error {
	cl := meta.(*client.Client)
	artifact := parent.Item.(*Artifact)

	if !artifact.IsIndex {
		return nil
	}

	var idx oci.ImageIndex
	if err := json.Unmarshal(artifact.RawJSON, &idx); err != nil {
		return nil
	}

	for _, child := range idx.Manifests {
		platform := ""
		if child.Platform != nil {
			platform = fmt.Sprintf("%s/%s", child.Platform.OS, child.Platform.Architecture)
			if child.Platform.Variant != "" {
				platform += "/" + child.Platform.Variant
			}
		}
		res <- &IndexChild{
			TargetName:   cl.Target.Name,
			Registry:     cl.Target.Endpoint,
			Repository:   artifact.Repository,
			ParentDigest: artifact.Digest,
			Digest:       child.Digest,
			MediaType:    child.MediaType,
			Size:         child.Size,
			Platform:     platform,
		}
	}
	return nil
}

func fetchManifestAnnotations(ctx context.Context, meta schema.ClientMeta, parent *schema.Resource, res chan<- any) error {
	cl := meta.(*client.Client)
	artifact := parent.Item.(*Artifact)

	var annotations map[string]string

	if artifact.IsIndex {
		var idx oci.ImageIndex
		if err := json.Unmarshal(artifact.RawJSON, &idx); err == nil {
			annotations = idx.Annotations
		}
	} else {
		var m oci.ImageManifest
		if err := json.Unmarshal(artifact.RawJSON, &m); err == nil {
			annotations = m.Annotations
		}
	}

	for k, v := range annotations {
		res <- &ManifestAnnotation{
			TargetName: cl.Target.Name,
			Registry:   cl.Target.Endpoint,
			Repository: artifact.Repository,
			Digest:     artifact.Digest,
			Key:        k,
			Value:      v,
		}
	}
	return nil
}

func fetchImageConfigs(ctx context.Context, meta schema.ClientMeta, parent *schema.Resource, res chan<- any) error {
	cl := meta.(*client.Client)
	artifact := parent.Item.(*Artifact)

	if artifact.IsIndex {
		return nil
	}

	var m oci.ImageManifest
	if err := json.Unmarshal(artifact.RawJSON, &m); err != nil {
		return nil
	}

	// Only fetch config for actual image configs
	if !isImageConfigMediaType(m.Config.MediaType) {
		return nil
	}

	configBlob, err := cl.OCIClient.GetBlob(ctx, artifact.Repository, m.Config.Digest)
	if err != nil {
		cl.Logger.Warn().Err(err).Str("digest", m.Config.Digest).Msg("failed to fetch config blob")
		return nil
	}

	var config oci.ImageConfig
	if err := json.Unmarshal(configBlob, &config); err != nil {
		cl.Logger.Warn().Err(err).Msg("failed to parse config blob")
		return nil
	}

	res <- &ImageConfigRow{
		TargetName:   cl.Target.Name,
		Registry:     cl.Target.Endpoint,
		Repository:   artifact.Repository,
		Digest:       artifact.Digest,
		Architecture: config.Architecture,
		OS:           config.OS,
		Created:      config.Created,
		Author:       config.Author,
		ParsedConfig: &config,
	}
	return nil
}

func fetchImageLabels(ctx context.Context, meta schema.ClientMeta, parent *schema.Resource, res chan<- any) error {
	data := parent.Item.(*ImageConfigRow)
	if data.ParsedConfig == nil {
		return nil
	}

	for k, v := range data.ParsedConfig.Config.Labels {
		res <- &ImageLabel{
			TargetName: data.TargetName,
			Registry:   data.Registry,
			Repository: data.Repository,
			Digest:     data.Digest,
			Key:        k,
			Value:      v,
		}
	}
	return nil
}

func fetchImageHistory(ctx context.Context, meta schema.ClientMeta, parent *schema.Resource, res chan<- any) error {
	data := parent.Item.(*ImageConfigRow)
	if data.ParsedConfig == nil {
		return nil
	}

	for i, h := range data.ParsedConfig.History {
		res <- &ImageHistory{
			TargetName: data.TargetName,
			Registry:   data.Registry,
			Repository: data.Repository,
			Digest:     data.Digest,
			Step:       i,
			Created:    h.Created,
			CreatedBy:  h.CreatedBy,
			Comment:    h.Comment,
			EmptyLayer: h.EmptyLayer,
		}
	}
	return nil
}

func fetchReferrers(ctx context.Context, meta schema.ClientMeta, parent *schema.Resource, res chan<- any) error {
	cl := meta.(*client.Client)
	artifact := parent.Item.(*Artifact)

	referrers, err := cl.OCIClient.ListReferrers(ctx, artifact.Repository, artifact.Digest)
	if err != nil {
		cl.Logger.Debug().Err(err).Str("digest", artifact.Digest).Msg("failed to list referrers")
		return nil
	}

	for _, ref := range referrers {
		res <- &Referrer{
			TargetName:    cl.Target.Name,
			Registry:      cl.Target.Endpoint,
			Repository:    artifact.Repository,
			SubjectDigest: artifact.Digest,
			Digest:        ref.Digest,
			MediaType:     ref.MediaType,
			ArtifactType:  ref.ArtifactType,
			Size:          ref.Size,
		}
	}
	return nil
}

func isIndexMediaType(mediaType string) bool {
	return mediaType == "application/vnd.oci.image.index.v1+json" ||
		mediaType == "application/vnd.docker.distribution.manifest.list.v2+json"
}

func isImageConfigMediaType(mediaType string) bool {
	return mediaType == "application/vnd.oci.image.config.v1+json" ||
		mediaType == "application/vnd.docker.container.image.v1+json" ||
		strings.HasPrefix(mediaType, "application/vnd.cncf.helm.config.")
}

func computeDigest(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", h)
}
