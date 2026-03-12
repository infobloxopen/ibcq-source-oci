package resources

import (
	"encoding/json"

	"github.com/infobloxopen/ibcq-source-oci/internal/oci"
)

// Repository represents an OCI repository row.
type Repository struct {
	TargetName string `json:"target_name"`
	Registry   string `json:"registry"`
	Name       string `json:"name"`
	FullName   string `json:"full_name"`
}

// Artifact represents a single OCI artifact (manifest).
type Artifact struct {
	TargetName   string          `json:"target_name"`
	Registry     string          `json:"registry"`
	Repository   string          `json:"repository"`
	Digest       string          `json:"digest"`
	MediaType    string          `json:"media_type"`
	ArtifactType string          `json:"artifact_type"`
	Size         int64           `json:"size"`
	IsIndex      bool            `json:"is_index"`
	RawJSON      json.RawMessage `json:"raw_json"`
}

// Tag represents a tag pointing to a digest.
type Tag struct {
	TargetName string `json:"target_name"`
	Registry   string `json:"registry"`
	Repository string `json:"repository"`
	Name       string `json:"name"`
	Digest     string `json:"digest"`
}

// Layer represents a layer descriptor within an artifact.
type Layer struct {
	TargetName   string `json:"target_name"`
	Registry     string `json:"registry"`
	Repository   string `json:"repository"`
	Digest       string `json:"digest"`
	ParentDigest string `json:"parent_digest"`
	MediaType    string `json:"media_type"`
	Size         int64  `json:"size"`
	Index        int    `json:"index"`
}

// IndexChild represents a child manifest within an image index.
type IndexChild struct {
	TargetName   string `json:"target_name"`
	Registry     string `json:"registry"`
	Repository   string `json:"repository"`
	ParentDigest string `json:"parent_digest"`
	Digest       string `json:"digest"`
	MediaType    string `json:"media_type"`
	Size         int64  `json:"size"`
	Platform     string `json:"platform"`
}

// ManifestAnnotation represents a single annotation on a manifest.
type ManifestAnnotation struct {
	TargetName string `json:"target_name"`
	Registry   string `json:"registry"`
	Repository string `json:"repository"`
	Digest     string `json:"digest"`
	Key        string `json:"key"`
	Value      string `json:"value"`
}

// ImageConfigRow represents the image config blob metadata.
type ImageConfigRow struct {
	TargetName   string `json:"target_name"`
	Registry     string `json:"registry"`
	Repository   string `json:"repository"`
	Digest       string `json:"digest"`
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	Created      string `json:"created"`
	Author       string `json:"author"`

	// ParsedConfig holds the full parsed config for child resolvers. Not a column.
	ParsedConfig *oci.ImageConfig `json:"-"`
}

// ImageLabel represents a label from the image config.
type ImageLabel struct {
	TargetName string `json:"target_name"`
	Registry   string `json:"registry"`
	Repository string `json:"repository"`
	Digest     string `json:"digest"`
	Key        string `json:"key"`
	Value      string `json:"value"`
}

// ImageHistory represents a build history entry.
type ImageHistory struct {
	TargetName string `json:"target_name"`
	Registry   string `json:"registry"`
	Repository string `json:"repository"`
	Digest     string `json:"digest"`
	Step       int    `json:"step"`
	Created    string `json:"created"`
	CreatedBy  string `json:"created_by"`
	Comment    string `json:"comment"`
	EmptyLayer bool   `json:"empty_layer"`
}

// Referrer represents an artifact that references another artifact.
type Referrer struct {
	TargetName    string `json:"target_name"`
	Registry      string `json:"registry"`
	Repository    string `json:"repository"`
	SubjectDigest string `json:"subject_digest"`
	Digest        string `json:"digest"`
	MediaType     string `json:"media_type"`
	ArtifactType  string `json:"artifact_type"`
	Size          int64  `json:"size"`
}
