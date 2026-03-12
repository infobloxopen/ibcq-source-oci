package client

import "fmt"

// Spec holds the plugin configuration.
type Spec struct {
	Targets []TargetSpec `json:"targets"`
}

// TargetSpec defines a single registry target.
type TargetSpec struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"` // "oci", "harbor", "ghcr"
	Endpoint string   `json:"endpoint"`
	Auth     AuthSpec `json:"auth"`

	// Discovery settings
	Discovery DiscoverySpec `json:"discovery"`

	// Harbor-specific
	Harbor HarborSpec `json:"harbor"`
}

// AuthSpec configures registry authentication.
type AuthSpec struct {
	Mode     string `json:"mode"` // "none", "basic", "bearer", "github_pat"
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
}

// DiscoverySpec configures repository discovery.
type DiscoverySpec struct {
	Source       string   `json:"source,omitempty"` // "static", "catalog", "harbor_api", "packages_api"
	Repositories []string `json:"repositories,omitempty"`
	Projects     []string `json:"projects,omitempty"`
}

// HarborSpec configures Harbor-specific options.
type HarborSpec struct {
	IncludeLabels      bool `json:"include_labels"`
	IncludeAccessories bool `json:"include_accessories"`
}

func (s *Spec) SetDefaults() {
	for i := range s.Targets {
		if s.Targets[i].Kind == "" {
			s.Targets[i].Kind = "oci"
		}
		if s.Targets[i].Auth.Mode == "" {
			s.Targets[i].Auth.Mode = "none"
		}
		if s.Targets[i].Discovery.Source == "" {
			s.Targets[i].Discovery.Source = "static"
		}
	}
}

func (s *Spec) Validate() error {
	if len(s.Targets) == 0 {
		return fmt.Errorf("at least one target must be configured")
	}
	for _, t := range s.Targets {
		if t.Name == "" {
			return fmt.Errorf("target name is required")
		}
		if t.Endpoint == "" {
			return fmt.Errorf("target %q: endpoint is required", t.Name)
		}
		switch t.Kind {
		case "oci", "harbor", "ghcr":
		default:
			return fmt.Errorf("target %q: unsupported kind %q", t.Name, t.Kind)
		}
	}
	return nil
}
