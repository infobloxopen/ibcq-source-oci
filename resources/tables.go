package resources

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/cloudquery/plugin-sdk/v4/schema"
	"github.com/cloudquery/plugin-sdk/v4/transformers"
)

func Tables() schema.Tables {
	tables := schema.Tables{
		repositoriesTable(),
		tagsTable(),
	}
	if err := transformers.TransformTables(tables); err != nil {
		panic(err)
	}
	for _, t := range tables {
		schema.AddCqIDs(t)
	}
	return tables
}

func repositoriesTable() *schema.Table {
	return &schema.Table{
		Name:        "oci_repositories",
		Description: "OCI container image repositories",
		Transform:   transformers.TransformWithStruct(&Repository{}, transformers.WithPrimaryKeys("TargetName", "FullName")),
		Resolver:    fetchRepositories,
		Relations: schema.Tables{
			artifactsTable(),
		},
	}
}

func artifactsTable() *schema.Table {
	return &schema.Table{
		Name:        "oci_artifacts",
		Description: "OCI artifacts (image manifests and indexes)",
		Transform:   transformers.TransformWithStruct(&Artifact{}, transformers.WithPrimaryKeys("TargetName", "Repository", "Digest")),
		Resolver:    fetchArtifacts,
		Relations: schema.Tables{
			layersTable(),
			indexChildrenTable(),
			manifestAnnotationsTable(),
			imageConfigsTable(),
			referrersTable(),
		},
	}
}

func tagsTable() *schema.Table {
	return &schema.Table{
		Name:        "oci_tags",
		Description: "OCI repository tags",
		Transform:   transformers.TransformWithStruct(&Tag{}, transformers.WithPrimaryKeys("TargetName", "Repository", "Name")),
		Resolver:    fetchTags,
	}
}

func layersTable() *schema.Table {
	return &schema.Table{
		Name:        "oci_layers",
		Description: "Layers in an OCI artifact manifest",
		Transform:   transformers.TransformWithStruct(&Layer{}, transformers.WithPrimaryKeys("ParentDigest", "Digest")),
		Resolver:    fetchLayers,
	}
}

func indexChildrenTable() *schema.Table {
	return &schema.Table{
		Name:        "oci_index_children",
		Description: "Child manifests in an OCI image index",
		Transform:   transformers.TransformWithStruct(&IndexChild{}, transformers.WithPrimaryKeys("ParentDigest", "Digest")),
		Resolver:    fetchIndexChildren,
	}
}

func manifestAnnotationsTable() *schema.Table {
	return &schema.Table{
		Name:        "oci_manifest_annotations",
		Description: "Annotations on OCI manifests",
		Transform:   transformers.TransformWithStruct(&ManifestAnnotation{}, transformers.WithPrimaryKeys("Digest", "Key")),
		Resolver:    fetchManifestAnnotations,
	}
}

func imageConfigsTable() *schema.Table {
	return &schema.Table{
		Name:        "oci_image_configs",
		Description: "OCI image configuration blobs",
		Transform:   transformers.TransformWithStruct(&ImageConfigRow{}, transformers.WithPrimaryKeys("Digest")),
		Resolver:    fetchImageConfigs,
		Relations: schema.Tables{
			imageLabelsTable(),
			imageHistoryTable(),
		},
	}
}

func imageLabelsTable() *schema.Table {
	return &schema.Table{
		Name:        "oci_image_labels",
		Description: "Labels from OCI image config",
		Columns: schema.ColumnList{
			{Name: "digest", Type: arrow.BinaryTypes.String, PrimaryKey: true},
			{Name: "key", Type: arrow.BinaryTypes.String, PrimaryKey: true},
			{Name: "value", Type: arrow.BinaryTypes.String},
		},
		Resolver: fetchImageLabels,
	}
}

func imageHistoryTable() *schema.Table {
	return &schema.Table{
		Name:        "oci_image_history",
		Description: "Build history from OCI image config",
		Transform:   transformers.TransformWithStruct(&ImageHistory{}, transformers.WithPrimaryKeys("Digest", "Step")),
		Resolver:    fetchImageHistory,
	}
}

func referrersTable() *schema.Table {
	return &schema.Table{
		Name:        "oci_referrers",
		Description: "OCI referrers (signatures, SBOMs, etc.)",
		Transform:   transformers.TransformWithStruct(&Referrer{}, transformers.WithPrimaryKeys("SubjectDigest", "Digest")),
		Resolver:    fetchReferrers,
	}
}
