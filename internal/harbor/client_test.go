package harbor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer(handler http.Handler) *httptest.Server {
	return httptest.NewServer(handler)
}

func TestListProjects(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2.0/projects", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]Project{
			{ProjectID: 1, Name: "library", RepoCount: 3},
			{ProjectID: 2, Name: "team-a", RepoCount: 5},
		})
	})
	srv := newTestServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL, "admin", "pass")
	projects, err := c.ListProjects(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 2 {
		t.Fatalf("got %d projects, want 2", len(projects))
	}
	if projects[0].Name != "library" {
		t.Errorf("got name %q, want library", projects[0].Name)
	}
}

func TestListRepositories(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2.0/projects/library/repositories", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]Repository{
			{ID: 1, Name: "library/nginx", ProjectID: 1, ArtifactCount: 2},
		})
	})
	srv := newTestServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL, "admin", "pass")
	repos, err := c.ListRepositories(t.Context(), "library")
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 {
		t.Fatalf("got %d repos, want 1", len(repos))
	}
}

func TestListArtifacts(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2.0/projects/library/repositories/nginx/artifacts", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("with_tag") != "true" {
			t.Error("expected with_tag=true")
		}
		json.NewEncoder(w).Encode([]Artifact{
			{
				ID:     1,
				Digest: "sha256:abc123",
				Tags:   []Tag{{ID: 1, Name: "latest"}},
				Labels: []Label{{ID: 1, Name: "production", Scope: "g"}},
				Accessories: []Accessory{
					{ID: 1, Digest: "sha256:sig1", Type: "signature.cosign", Size: 512},
				},
			},
		})
	})
	srv := newTestServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL, "admin", "pass")
	artifacts, err := c.ListArtifacts(t.Context(), "library", "nginx")
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("got %d artifacts, want 1", len(artifacts))
	}
	if len(artifacts[0].Tags) != 1 {
		t.Errorf("got %d tags, want 1", len(artifacts[0].Tags))
	}
	if len(artifacts[0].Accessories) != 1 {
		t.Errorf("got %d accessories, want 1", len(artifacts[0].Accessories))
	}
}

func TestListLabels(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2.0/labels", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]Label{
			{ID: 1, Name: "production", Scope: "g"},
		})
	})
	srv := newTestServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL, "admin", "pass")
	labels, err := c.ListLabels(t.Context(), "g", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != 1 {
		t.Fatalf("got %d labels, want 1", len(labels))
	}
}

func TestHTTPError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2.0/projects", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"errors":[{"message":"db down"}]}`))
	})
	srv := newTestServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL, "", "")
	_, err := c.ListProjects(t.Context())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected 500 in error, got: %v", err)
	}
}

func TestAuthHeader(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2.0/projects", func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode([]Project{})
	})
	srv := newTestServer(mux)
	defer srv.Close()

	// Bad creds
	_, err := NewClient(srv.URL, "admin", "wrong").ListProjects(t.Context())
	if err == nil {
		t.Error("expected auth error")
	}

	// Good creds
	projects, err := NewClient(srv.URL, "admin", "secret").ListProjects(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 0 {
		t.Errorf("got %d projects, want 0", len(projects))
	}
}
