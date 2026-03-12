package client

import "testing"

func TestSpec_Validate(t *testing.T) {
	tests := []struct {
		name    string
		spec    Spec
		wantErr bool
	}{
		{
			name:    "empty targets",
			spec:    Spec{},
			wantErr: true,
		},
		{
			name: "missing name",
			spec: Spec{Targets: []TargetSpec{{Endpoint: "http://example.com"}}},
			wantErr: true,
		},
		{
			name: "missing endpoint",
			spec: Spec{Targets: []TargetSpec{{Name: "test"}}},
			wantErr: true,
		},
		{
			name: "invalid kind",
			spec: Spec{Targets: []TargetSpec{{Name: "test", Endpoint: "http://example.com", Kind: "invalid"}}},
			wantErr: true,
		},
		{
			name: "valid oci target",
			spec: Spec{Targets: []TargetSpec{{Name: "test", Endpoint: "http://example.com", Kind: "oci"}}},
			wantErr: false,
		},
		{
			name: "valid harbor target",
			spec: Spec{Targets: []TargetSpec{{Name: "harbor", Endpoint: "https://harbor.example.com", Kind: "harbor"}}},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.spec.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSpec_SetDefaults(t *testing.T) {
	spec := Spec{
		Targets: []TargetSpec{{Name: "test", Endpoint: "http://example.com"}},
	}
	spec.SetDefaults()

	if spec.Targets[0].Kind != "oci" {
		t.Errorf("expected kind 'oci', got %q", spec.Targets[0].Kind)
	}
	if spec.Targets[0].Auth.Mode != "none" {
		t.Errorf("expected auth mode 'none', got %q", spec.Targets[0].Auth.Mode)
	}
	if spec.Targets[0].Discovery.Source != "static" {
		t.Errorf("expected discovery source 'static', got %q", spec.Targets[0].Discovery.Source)
	}
}
