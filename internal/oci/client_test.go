package oci

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
)

func TestParseWWWAuthenticate(t *testing.T) {
	tests := []struct {
		header  string
		realm   string
		service string
		scope   string
	}{
		{
			header:  `Bearer realm="https://auth.example.com/token",service="registry.example.com",scope="repository:test:pull"`,
			realm:   "https://auth.example.com/token",
			service: "registry.example.com",
			scope:   "repository:test:pull",
		},
		{
			header:  `Bearer realm="https://auth.example.com/token",service="registry"`,
			realm:   "https://auth.example.com/token",
			service: "registry",
			scope:   "",
		},
	}

	for _, tt := range tests {
		realm, service, scope := parseWWWAuthenticate(tt.header)
		if realm != tt.realm {
			t.Errorf("realm: got %q, want %q", realm, tt.realm)
		}
		if service != tt.service {
			t.Errorf("service: got %q, want %q", service, tt.service)
		}
		if scope != tt.scope {
			t.Errorf("scope: got %q, want %q", scope, tt.scope)
		}
	}
}

func TestListTags(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/test/repo/tags/list", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"name": "test/repo",
			"tags": []string{"v1", "v2", "latest"},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	logger := zerolog.Nop()
	client := NewClient(server.URL, "none", "", "", "", logger)

	tags, err := client.ListTags(t.Context(), "test/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 3 {
		t.Fatalf("expected 3 tags, got %d", len(tags))
	}
	if tags[0] != "v1" || tags[1] != "v2" || tags[2] != "latest" {
		t.Errorf("unexpected tags: %v", tags)
	}
}

func TestCatalog(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/_catalog", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"repositories": []string{"repo1", "repo2"},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	logger := zerolog.Nop()
	client := NewClient(server.URL, "none", "", "", "", logger)

	repos, err := client.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}
}

func TestGetManifest(t *testing.T) {
	manifest := ImageManifest{
		SchemaVersion: 2,
		MediaType:     "application/vnd.oci.image.manifest.v1+json",
		Config: Descriptor{
			MediaType: "application/vnd.oci.image.config.v1+json",
			Digest:    "sha256:abc123",
			Size:      1234,
		},
		Layers: []Descriptor{
			{
				MediaType: "application/vnd.oci.image.layer.v1.tar+gzip",
				Digest:    "sha256:layer1",
				Size:      5678,
			},
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v2/test/repo/manifests/v1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
		w.Header().Set("Docker-Content-Digest", "sha256:manifestdigest")
		json.NewEncoder(w).Encode(manifest)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	logger := zerolog.Nop()
	client := NewClient(server.URL, "none", "", "", "", logger)

	resp, err := client.GetManifest(t.Context(), "test/repo", "v1")
	if err != nil {
		t.Fatal(err)
	}

	if resp.Digest != "sha256:manifestdigest" {
		t.Errorf("unexpected digest: %s", resp.Digest)
	}
	if resp.MediaType != "application/vnd.oci.image.manifest.v1+json" {
		t.Errorf("unexpected media type: %s", resp.MediaType)
	}

	var parsed ImageManifest
	if err := json.Unmarshal(resp.Body, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Config.Digest != "sha256:abc123" {
		t.Errorf("unexpected config digest: %s", parsed.Config.Digest)
	}
}
