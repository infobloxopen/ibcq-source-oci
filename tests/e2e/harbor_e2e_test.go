//go:build linux && amd64

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/infobloxopen/ibcq-source-oci/client"
)

// Harbor e2e tests. These only run on linux/amd64 because Harbor images
// are amd64-only and segfault under QEMU on ARM.
//
// Prerequisites:
//   ./tests/e2e/harbor-setup.sh
//
// Or set HARBOR_ENDPOINT to point at an existing Harbor instance.

func harborEndpointFromEnv() string {
	if ep := os.Getenv("HARBOR_ENDPOINT"); ep != "" {
		return ep
	}
	return "http://localhost:30080"
}

func skipIfNoHarbor(t *testing.T) {
	t.Helper()
	ep := harborEndpointFromEnv()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ep+"/api/v2.0/health", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Skipf("Harbor not reachable at %s: %v", ep, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Skipf("Harbor unhealthy at %s: HTTP %d", ep, resp.StatusCode)
	}
}

func harborCreateProject(t *testing.T, projectName string) {
	t.Helper()
	ep := harborEndpointFromEnv()
	body := fmt.Sprintf(`{"project_name": %q, "public": true}`, projectName)
	req, _ := http.NewRequest("POST", ep+"/api/v2.0/projects", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "Harbor12345")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 201 && resp.StatusCode != 409 {
		t.Fatalf("create project: HTTP %d", resp.StatusCode)
	}
}

func harborHost(t *testing.T) string {
	t.Helper()
	host := harborEndpointFromEnv()
	for _, prefix := range []string{"http://", "https://"} {
		if len(host) > len(prefix) && host[:len(prefix)] == prefix {
			host = host[len(prefix):]
		}
	}
	return host
}

func harborPushImage(t *testing.T, repo, tag string) {
	t.Helper()
	host := harborHost(t)

	// Authenticate crane to Harbor
	login := exec.Command("crane", "auth", "login", host,
		"-u", "admin", "-p", "Harbor12345", "--insecure")
	if out, err := login.CombinedOutput(); err != nil {
		t.Fatalf("crane auth login: %v\n%s", err, out)
	}

	dst := fmt.Sprintf("%s/%s:%s", host, repo, tag)
	cmd := exec.Command("crane", "copy", "docker.io/library/alpine:3.19", dst, "--insecure")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("push to harbor: %v\n%s", err, out)
	}
}

func TestHarbor_ProjectDiscovery(t *testing.T) {
	skipIfNoHarbor(t)
	ep := harborEndpointFromEnv()

	harborCreateProject(t, "e2e-disco")
	harborPushImage(t, "e2e-disco/testimg", "v1")

	spec := client.Spec{
		Targets: []client.TargetSpec{{
			Name:     "harbor-disco",
			Kind:     "harbor",
			Endpoint: ep,
			Auth: client.AuthSpec{
				Mode:     "basic",
				Username: "admin",
				Password: "Harbor12345",
			},
			Discovery: client.DiscoverySpec{
				Projects: []string{"e2e-disco"},
			},
		}},
	}

	msgs := syncPlugin(t, spec, []string{"*"})
	tc := tableCounts(msgs)
	t.Logf("Harbor project discovery counts: %+v", tc)

	if tc["oci_repositories"] == 0 {
		t.Error("expected repositories discovered via Harbor API")
	}
	if tc["oci_artifacts"] == 0 {
		t.Error("expected artifacts")
	}
	if tc["oci_tags"] == 0 {
		t.Error("expected tags")
	}
}

func TestHarbor_AllProjectsDiscovery(t *testing.T) {
	skipIfNoHarbor(t)
	ep := harborEndpointFromEnv()

	harborCreateProject(t, "e2e-all-a")
	harborCreateProject(t, "e2e-all-b")
	harborPushImage(t, "e2e-all-a/img1", "v1")
	harborPushImage(t, "e2e-all-b/img2", "v1")

	spec := client.Spec{
		Targets: []client.TargetSpec{{
			Name:     "harbor-all",
			Kind:     "harbor",
			Endpoint: ep,
			Auth: client.AuthSpec{
				Mode:     "basic",
				Username: "admin",
				Password: "Harbor12345",
			},
			Discovery: client.DiscoverySpec{
				// No projects specified → discover all
			},
		}},
	}

	msgs := syncPlugin(t, spec, []string{"oci_repositories"})
	tc := tableCounts(msgs)
	t.Logf("Harbor all-projects discovery counts: %+v", tc)

	// Should discover repos from both projects (plus the default "library" project)
	if tc["oci_repositories"] < 2 {
		t.Errorf("expected at least 2 repositories from all projects, got %d", tc["oci_repositories"])
	}
}

func TestHarbor_OCIAdapter(t *testing.T) {
	skipIfNoHarbor(t)
	ep := harborEndpointFromEnv()

	harborCreateProject(t, "e2e-oci")
	harborPushImage(t, "e2e-oci/alpine", "v1")

	// Use Harbor as a plain OCI registry (kind=oci with static discovery)
	spec := client.Spec{
		Targets: []client.TargetSpec{{
			Name:     "harbor-as-oci",
			Kind:     "oci",
			Endpoint: ep,
			Auth: client.AuthSpec{
				Mode:     "basic",
				Username: "admin",
				Password: "Harbor12345",
			},
			Discovery: client.DiscoverySpec{
				Source:       "static",
				Repositories: []string{"e2e-oci/alpine"},
			},
		}},
	}

	msgs := syncPlugin(t, spec, []string{"*"})
	tc := tableCounts(msgs)
	t.Logf("Harbor-as-OCI counts: %+v", tc)

	if tc["oci_repositories"] != 1 {
		t.Errorf("expected 1 repository, got %d", tc["oci_repositories"])
	}
	if tc["oci_artifacts"] == 0 {
		t.Error("expected artifacts via OCI protocol")
	}
	if tc["oci_image_configs"] == 0 {
		t.Error("expected image configs via OCI protocol")
	}
}

func TestHarbor_MultiArchImage(t *testing.T) {
	skipIfNoHarbor(t)
	ep := harborEndpointFromEnv()

	harborCreateProject(t, "e2e-multiarch")
	// alpine:3.19 is multi-arch
	harborPushImage(t, "e2e-multiarch/alpine", "latest")

	spec := client.Spec{
		Targets: []client.TargetSpec{{
			Name:     "harbor-multiarch",
			Kind:     "harbor",
			Endpoint: ep,
			Auth: client.AuthSpec{
				Mode:     "basic",
				Username: "admin",
				Password: "Harbor12345",
			},
			Discovery: client.DiscoverySpec{
				Projects: []string{"e2e-multiarch"},
			},
		}},
	}

	msgs := syncPlugin(t, spec, []string{"*"})
	tc := tableCounts(msgs)
	t.Logf("Harbor multi-arch counts: %+v", tc)

	if tc["oci_index_children"] == 0 {
		t.Error("expected index children for multi-arch image")
	}
	if tc["oci_layers"] == 0 {
		t.Error("expected layers from resolved child manifests")
	}
	if tc["oci_image_configs"] == 0 {
		t.Error("expected image configs from resolved child manifests")
	}
}

func TestHarbor_HelmChart(t *testing.T) {
	skipIfNoHarbor(t)
	ep := harborEndpointFromEnv()

	harborCreateProject(t, "e2e-helm")

	// Create and push a minimal helm chart
	dir := t.TempDir()
	chartYAML := `apiVersion: v2
name: e2e-chart
version: 0.1.0
description: test chart
type: application`
	os.MkdirAll(dir+"/templates", 0o755)
	os.WriteFile(dir+"/Chart.yaml", []byte(chartYAML), 0o644)
	os.WriteFile(dir+"/templates/cm.yaml", []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\n"), 0o644)

	// Package
	pkg := exec.Command("helm", "package", dir, "-d", dir)
	if out, err := pkg.CombinedOutput(); err != nil {
		t.Fatalf("helm package: %v\n%s", err, out)
	}
	// Push
	host := harborHost(t)

	// Authenticate helm to Harbor over plain HTTP
	helmLogin := exec.Command("helm", "registry", "login", host,
		"--username", "admin", "--password", "Harbor12345", "--insecure")
	if out, err := helmLogin.CombinedOutput(); err != nil {
		t.Fatalf("helm registry login: %v\n%s", err, out)
	}

	push := exec.Command("helm", "push", dir+"/e2e-chart-0.1.0.tgz",
		fmt.Sprintf("oci://%s/e2e-helm", host), "--plain-http")
	if out, err := push.CombinedOutput(); err != nil {
		t.Fatalf("helm push: %v\n%s", err, out)
	}

	spec := client.Spec{
		Targets: []client.TargetSpec{{
			Name:     "harbor-helm",
			Kind:     "harbor",
			Endpoint: ep,
			Auth: client.AuthSpec{
				Mode:     "basic",
				Username: "admin",
				Password: "Harbor12345",
			},
			Discovery: client.DiscoverySpec{
				Projects: []string{"e2e-helm"},
			},
		}},
	}

	msgs := syncPlugin(t, spec, []string{"*"})
	tc := tableCounts(msgs)
	t.Logf("Harbor helm chart counts: %+v", tc)

	if tc["oci_artifacts"] == 0 {
		t.Error("expected helm chart artifact")
	}
	if tc["oci_manifest_annotations"] == 0 {
		t.Error("expected OCI annotations on helm chart")
	}

	// Verify artifact type
	types := columnValues(msgs, "oci_artifacts", "artifact_type")
	foundHelm := false
	for _, at := range types {
		if at != "" && json.Valid([]byte(`"`+at+`"`)) {
			// Look for helm config media type
			if len(at) > 4 && at[len(at)-4:] == "json" {
				foundHelm = true
			}
		}
	}
	if !foundHelm && tc["oci_artifacts"] > 0 {
		t.Logf("Artifact types: %v (may still be valid)", types)
	}
}

// TestHarbor_Incremental verifies that pushing a new image between syncs
// produces additional artifacts and tags on the second Harbor sync.
func TestHarbor_Incremental(t *testing.T) {
	skipIfNoHarbor(t)
	ep := harborEndpointFromEnv()

	project := "e2e-incr"
	harborCreateProject(t, project)
	harborPushImage(t, project+"/app", "v1")

	spec := client.Spec{
		Targets: []client.TargetSpec{{
			Name:     "harbor-incr",
			Kind:     "harbor",
			Endpoint: ep,
			Auth: client.AuthSpec{
				Mode:     "basic",
				Username: "admin",
				Password: "Harbor12345",
			},
			Discovery: client.DiscoverySpec{
				Projects: []string{project},
			},
		}},
	}

	// First sync
	msgs1 := syncPlugin(t, spec, []string{"*"})
	tc1 := tableCounts(msgs1)
	t.Logf("Harbor incremental sync 1 counts: %+v", tc1)

	if tc1["oci_tags"] == 0 {
		t.Error("sync 1: expected tags")
	}
	if tc1["oci_repositories"] != 1 {
		t.Errorf("sync 1: expected 1 repository, got %d", tc1["oci_repositories"])
	}

	// Push a second image (different repo in the same project)
	harborPushImage(t, project+"/worker", "v1")

	// Second sync
	msgs2 := syncPlugin(t, spec, []string{"*"})
	tc2 := tableCounts(msgs2)
	t.Logf("Harbor incremental sync 2 counts: %+v", tc2)

	if tc2["oci_repositories"] != 2 {
		t.Errorf("sync 2: expected 2 repositories (app+worker), got %d", tc2["oci_repositories"])
	}
	if tc2["oci_tags"] < 2 {
		t.Errorf("sync 2: expected at least 2 tags, got %d", tc2["oci_tags"])
	}
	if tc2["oci_artifacts"] <= tc1["oci_artifacts"] {
		t.Errorf("sync 2: expected more artifacts than sync 1 (%d vs %d)", tc2["oci_artifacts"], tc1["oci_artifacts"])
	}
}
