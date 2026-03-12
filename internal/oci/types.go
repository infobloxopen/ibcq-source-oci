package oci

// OCI spec types used throughout the plugin.

// Descriptor describes an OCI content descriptor.
type Descriptor struct {
	MediaType    string            `json:"mediaType"`
	Digest       string            `json:"digest"`
	Size         int64             `json:"size"`
	URLs         []string          `json:"urls,omitempty"`
	Annotations  map[string]string `json:"annotations,omitempty"`
	ArtifactType string            `json:"artifactType,omitempty"`
	Platform     *Platform         `json:"platform,omitempty"`
}

// Platform describes the platform for an image.
type Platform struct {
	Architecture string   `json:"architecture"`
	OS           string   `json:"os"`
	Variant      string   `json:"variant,omitempty"`
	OSVersion    string   `json:"os.version,omitempty"`
	OSFeatures   []string `json:"os.features,omitempty"`
}

// ImageManifest is an OCI image manifest.
type ImageManifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	MediaType     string            `json:"mediaType,omitempty"`
	ArtifactType  string            `json:"artifactType,omitempty"`
	Config        Descriptor        `json:"config"`
	Layers        []Descriptor      `json:"layers"`
	Annotations   map[string]string `json:"annotations,omitempty"`
	Subject       *Descriptor       `json:"subject,omitempty"`
}

// ImageIndex is an OCI image index (multi-arch manifest list).
type ImageIndex struct {
	SchemaVersion int               `json:"schemaVersion"`
	MediaType     string            `json:"mediaType,omitempty"`
	ArtifactType  string            `json:"artifactType,omitempty"`
	Manifests     []Descriptor      `json:"manifests"`
	Annotations   map[string]string `json:"annotations,omitempty"`
	Subject       *Descriptor       `json:"subject,omitempty"`
}

// ImageConfig is the OCI image configuration.
type ImageConfig struct {
	Architecture string          `json:"architecture,omitempty"`
	OS           string          `json:"os,omitempty"`
	Config       ContainerConfig `json:"config"`
	RootFS       RootFS          `json:"rootfs"`
	History      []HistoryEntry  `json:"history,omitempty"`
	Created      string          `json:"created,omitempty"`
	Author       string          `json:"author,omitempty"`
}

// ContainerConfig holds container runtime config from the image config blob.
type ContainerConfig struct {
	Labels       map[string]string `json:"Labels,omitempty"`
	Env          []string          `json:"Env,omitempty"`
	Cmd          []string          `json:"Cmd,omitempty"`
	Entrypoint   []string          `json:"Entrypoint,omitempty"`
	ExposedPorts map[string]any    `json:"ExposedPorts,omitempty"`
	WorkingDir   string            `json:"WorkingDir,omitempty"`
	User         string            `json:"User,omitempty"`
	Volumes      map[string]any    `json:"Volumes,omitempty"`
	StopSignal   string            `json:"StopSignal,omitempty"`
}

// RootFS describes the root filesystem.
type RootFS struct {
	Type    string   `json:"type"`
	DiffIDs []string `json:"diff_ids"`
}

// HistoryEntry represents one step in the image build history.
type HistoryEntry struct {
	Created    string `json:"created,omitempty"`
	CreatedBy  string `json:"created_by,omitempty"`
	Comment    string `json:"comment,omitempty"`
	EmptyLayer bool   `json:"empty_layer,omitempty"`
	Author     string `json:"author,omitempty"`
}
