package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/cloudquery/plugin-sdk/v4/message"
	"github.com/cloudquery/plugin-sdk/v4/plugin"
	"github.com/infobloxopen/ibcq-source-oci/client"
	internalPlugin "github.com/infobloxopen/ibcq-source-oci/plugin"
	"github.com/rs/zerolog"
)

const (
	registryEndpoint  = "http://localhost:30004"
	registry2Endpoint = "http://localhost:30005"
)

func skipIfNoCluster(t *testing.T) {
	t.Helper()
	// Check if docker registry is reachable
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", registryEndpoint+"/v2/", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Skipf("Skipping e2e test: registry not reachable at %s: %v", registryEndpoint, err)
	}
	resp.Body.Close()
}

// pushTestImage pushes a test image to the plain Docker registry using crane.
func pushTestImage(t *testing.T, repo, tag string) {
	t.Helper()

	// Use crane to copy a small image to the local registry
	// crane is more reliable than docker for pushing to plain registries
	src := "docker.io/library/alpine:3.19"
	dst := fmt.Sprintf("localhost:30004/%s:%s", repo, tag)

	cmd := exec.Command("crane", "copy", src, dst, "--insecure")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Fall back to using docker
		t.Logf("crane failed (%v), trying docker: %s", err, out)
		pushTestImageDocker(t, repo, tag)
		return
	}
}

func pushTestImageDocker(t *testing.T, repo, tag string) {
	t.Helper()

	dst := fmt.Sprintf("localhost:30004/%s:%s", repo, tag)

	cmds := [][]string{
		{"docker", "pull", "alpine:3.19"},
		{"docker", "tag", "alpine:3.19", dst},
		{"docker", "push", dst},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cmd %v failed: %v\n%s", args, err, out)
		}
	}
}

func syncPlugin(t *testing.T, spec client.Spec, tables []string) []message.SyncMessage {
	t.Helper()

	specJSON, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}

	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

	cl, err := internalPlugin.Configure(context.Background(), logger, specJSON, plugin.NewClientOptions{})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	defer cl.Close(context.Background())

	syncOpts := plugin.SyncOptions{
		Tables: tables,
	}

	var msgs []message.SyncMessage
	ch := make(chan message.SyncMessage, 1000)
	done := make(chan error, 1)
	go func() {
		done <- cl.Sync(context.Background(), syncOpts, ch)
		close(ch)
	}()

	for msg := range ch {
		msgs = append(msgs, msg)
	}
	if err := <-done; err != nil {
		t.Fatalf("Sync: %v", err)
	}
	return msgs
}

func countInsertMessages(msgs []message.SyncMessage, tableName string) int {
	count := 0
	for _, msg := range msgs {
		if insert, ok := msg.(*message.SyncInsert); ok {
			if insert.Record.Schema().Metadata().Values()[0] == tableName ||
				strings.Contains(fmt.Sprintf("%v", insert.Record.Schema()), tableName) {
				count++
			}
		}
	}
	return count
}

// TestDockerRegistry_BasicSync tests syncing from a plain Docker registry.
func TestDockerRegistry_BasicSync(t *testing.T) {
	skipIfNoCluster(t)

	// Push test images
	pushTestImage(t, "test/alpine", "v1")
	pushTestImage(t, "test/alpine", "v2")
	pushTestImage(t, "test/nginx", "latest")

	spec := client.Spec{
		Targets: []client.TargetSpec{
			{
				Name:     "docker-registry",
				Kind:     "oci",
				Endpoint: registryEndpoint,
				Auth:     client.AuthSpec{Mode: "none"},
				Discovery: client.DiscoverySpec{
					Source:       "catalog",
					Repositories: nil,
				},
			},
		},
	}

	msgs := syncPlugin(t, spec, []string{"*"})

	if len(msgs) == 0 {
		t.Fatal("expected sync messages, got none")
	}

	// Count messages by type
	insertCount := 0
	for _, msg := range msgs {
		if _, ok := msg.(*message.SyncInsert); ok {
			insertCount++
		}
	}

	t.Logf("Total insert messages: %d", insertCount)

	if insertCount == 0 {
		t.Fatal("expected insert messages, got none")
	}

	// We should have at least:
	// - 2 repositories (test/alpine, test/nginx)
	// - some artifacts
	// - some tags
	// Log details for debugging
	for _, msg := range msgs {
		if insert, ok := msg.(*message.SyncInsert); ok {
			tableName := ""
			md := insert.Record.Schema().Metadata()
			if md.Len() > 0 {
				keys := md.Keys()
				vals := md.Values()
				for i, k := range keys {
					if k == "cq:table_name" {
						tableName = vals[i]
						break
					}
				}
			}
			t.Logf("INSERT into %s: %d columns, %d rows", tableName, insert.Record.NumCols(), insert.Record.NumRows())
		}
	}
}

// TestDockerRegistry_StaticDiscovery tests syncing with static repository list.
func TestDockerRegistry_StaticDiscovery(t *testing.T) {
	skipIfNoCluster(t)

	pushTestImage(t, "test/myapp", "v1.0.0")

	spec := client.Spec{
		Targets: []client.TargetSpec{
			{
				Name:     "docker-registry-static",
				Kind:     "oci",
				Endpoint: registryEndpoint,
				Auth:     client.AuthSpec{Mode: "none"},
				Discovery: client.DiscoverySpec{
					Source:       "static",
					Repositories: []string{"test/myapp"},
				},
			},
		},
	}

	msgs := syncPlugin(t, spec, []string{"oci_repositories", "oci_tags"})

	if len(msgs) == 0 {
		t.Fatal("expected sync messages, got none")
	}

	foundRepo := false
	foundTag := false
	for _, msg := range msgs {
		if insert, ok := msg.(*message.SyncInsert); ok {
			md := insert.Record.Schema().Metadata()
			keys := md.Keys()
			vals := md.Values()
			for i, k := range keys {
				if k == "cq:table_name" {
					switch vals[i] {
					case "oci_repositories":
						foundRepo = true
					case "oci_tags":
						foundTag = true
					}
				}
			}
		}
	}

	if !foundRepo {
		t.Error("expected oci_repositories data")
	}
	if !foundTag {
		t.Error("expected oci_tags data")
	}
}

// TestDockerRegistry_ImageConfig tests that image config, labels, and history are fetched.
func TestDockerRegistry_ImageConfig(t *testing.T) {
	skipIfNoCluster(t)

	// Push an image with known labels using crane
	pushTestImage(t, "test/labeled", "v1")

	spec := client.Spec{
		Targets: []client.TargetSpec{
			{
				Name:     "docker-registry-config",
				Kind:     "oci",
				Endpoint: registryEndpoint,
				Auth:     client.AuthSpec{Mode: "none"},
				Discovery: client.DiscoverySpec{
					Source:       "static",
					Repositories: []string{"test/labeled"},
				},
			},
		},
	}

	msgs := syncPlugin(t, spec, []string{"oci_repositories", "oci_artifacts", "oci_image_configs", "oci_image_history"})

	tableNames := map[string]int{}
	for _, msg := range msgs {
		if insert, ok := msg.(*message.SyncInsert); ok {
			md := insert.Record.Schema().Metadata()
			keys := md.Keys()
			vals := md.Values()
			for i, k := range keys {
				if k == "cq:table_name" {
					tableNames[vals[i]] += int(insert.Record.NumRows())
				}
			}
		}
	}

	t.Logf("Table row counts: %+v", tableNames)

	if tableNames["oci_artifacts"] == 0 {
		t.Error("expected oci_artifacts data")
	}
	if tableNames["oci_image_configs"] == 0 {
		t.Error("expected oci_image_configs data")
	}
	if tableNames["oci_image_history"] == 0 {
		t.Error("expected oci_image_history data")
	}
}

// tableCounts extracts table name -> row count from sync messages.
func tableCounts(msgs []message.SyncMessage) map[string]int {
	counts := map[string]int{}
	for _, msg := range msgs {
		if insert, ok := msg.(*message.SyncInsert); ok {
			md := insert.Record.Schema().Metadata()
			keys := md.Keys()
			vals := md.Values()
			for i, k := range keys {
				if k == "cq:table_name" {
					counts[vals[i]] += int(insert.Record.NumRows())
				}
			}
		}
	}
	return counts
}

// columnValues extracts all string values for a given column from sync messages for a table.
func columnValues(msgs []message.SyncMessage, tableName, colName string) []string {
	var values []string
	for _, msg := range msgs {
		insert, ok := msg.(*message.SyncInsert)
		if !ok {
			continue
		}
		md := insert.Record.Schema().Metadata()
		keys := md.Keys()
		vals := md.Values()
		isTable := false
		for i, k := range keys {
			if k == "cq:table_name" && vals[i] == tableName {
				isTable = true
				break
			}
		}
		if !isTable {
			continue
		}
		rec := insert.Record
		for ci := 0; ci < int(rec.NumCols()); ci++ {
			if rec.ColumnName(ci) == colName {
				col := rec.Column(ci)
				for ri := 0; ri < col.Len(); ri++ {
					values = append(values, col.ValueStr(ri))
				}
			}
		}
	}
	return values
}

func skipIfNoSecondRegistry(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", registry2Endpoint+"/v2/", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Skipf("Skipping: second registry not reachable at %s: %v", registry2Endpoint, err)
	}
	resp.Body.Close()
}

// TestMultiTarget_Sync tests syncing from two registries in one plugin invocation.
func TestMultiTarget_Sync(t *testing.T) {
	skipIfNoCluster(t)
	skipIfNoSecondRegistry(t)

	// Push test image to second registry
	cmd := exec.Command("crane", "copy", "docker.io/library/busybox:1.36", "localhost:30005/test/busybox:v1", "--insecure")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("push busybox: %v: %s", err, out)
	}

	spec := client.Spec{
		Targets: []client.TargetSpec{
			{
				Name:     "registry-1",
				Kind:     "oci",
				Endpoint: registryEndpoint,
				Auth:     client.AuthSpec{Mode: "none"},
				Discovery: client.DiscoverySpec{
					Source:       "static",
					Repositories: []string{"test/alpine"},
				},
			},
			{
				Name:     "registry-2",
				Kind:     "oci",
				Endpoint: registry2Endpoint,
				Auth:     client.AuthSpec{Mode: "none"},
				Discovery: client.DiscoverySpec{
					Source: "catalog",
				},
			},
		},
	}

	msgs := syncPlugin(t, spec, []string{"*"})

	// Count tables
	tableNames := map[string]int{}
	for _, msg := range msgs {
		if insert, ok := msg.(*message.SyncInsert); ok {
			md := insert.Record.Schema().Metadata()
			keys := md.Keys()
			vals := md.Values()
			for i, k := range keys {
				if k == "cq:table_name" {
					tableNames[vals[i]] += int(insert.Record.NumRows())
				}
			}
		}
	}

	t.Logf("Multi-target table row counts: %+v", tableNames)

	if tableNames["oci_repositories"] < 2 {
		t.Errorf("expected repos from both targets, got %d", tableNames["oci_repositories"])
	}
	if tableNames["oci_artifacts"] == 0 {
		t.Error("expected oci_artifacts data")
	}
	if tableNames["oci_tags"] == 0 {
		t.Error("expected oci_tags data")
	}
}

// TestDockerRegistry_CatalogDiscovery tests the /v2/_catalog endpoint for auto-discovery.
func TestDockerRegistry_CatalogDiscovery(t *testing.T) {
	skipIfNoCluster(t)

	spec := client.Spec{
		Targets: []client.TargetSpec{
			{
				Name:     "docker-registry-catalog",
				Kind:     "oci",
				Endpoint: registryEndpoint,
				Auth:     client.AuthSpec{Mode: "none"},
				Discovery: client.DiscoverySpec{
					Source: "catalog",
				},
			},
		},
	}

	msgs := syncPlugin(t, spec, []string{"oci_repositories"})

	repoCount := 0
	for _, msg := range msgs {
		if insert, ok := msg.(*message.SyncInsert); ok {
			md := insert.Record.Schema().Metadata()
			keys := md.Keys()
			vals := md.Values()
			for i, k := range keys {
				if k == "cq:table_name" && vals[i] == "oci_repositories" {
					repoCount += int(insert.Record.NumRows())
				}
			}
		}
	}

	// We've pushed to test/alpine, test/nginx, test/myapp, test/labeled
	if repoCount < 2 {
		t.Errorf("expected at least 2 repositories from catalog, got %d", repoCount)
	}
	t.Logf("Catalog discovered %d repositories", repoCount)
}

// TestSinglePlatformImage verifies that a non-multi-arch image produces
// layers and image config directly (no index children).
func TestSinglePlatformImage(t *testing.T) {
	skipIfNoCluster(t)

	spec := client.Spec{
		Targets: []client.TargetSpec{{
			Name:     "singleplat",
			Kind:     "oci",
			Endpoint: registryEndpoint,
			Auth:     client.AuthSpec{Mode: "none"},
			Discovery: client.DiscoverySpec{
				Source:       "static",
				Repositories: []string{"edge/singleplat"},
			},
		}},
	}

	msgs := syncPlugin(t, spec, []string{"*"})
	tc := tableCounts(msgs)
	t.Logf("Single-platform counts: %+v", tc)

	if tc["oci_artifacts"] != 1 {
		t.Errorf("expected exactly 1 artifact (single manifest), got %d", tc["oci_artifacts"])
	}
	if tc["oci_index_children"] != 0 {
		t.Errorf("expected 0 index children for single-platform image, got %d", tc["oci_index_children"])
	}
	if tc["oci_layers"] == 0 {
		t.Error("expected layers for single-platform image")
	}
	if tc["oci_image_configs"] != 1 {
		t.Errorf("expected 1 image config, got %d", tc["oci_image_configs"])
	}
	if tc["oci_image_history"] == 0 {
		t.Error("expected image history for single-platform image")
	}
}

// TestImageLabels verifies that Dockerfile LABEL instructions are extracted
// into the oci_image_labels table.
func TestImageLabels(t *testing.T) {
	skipIfNoCluster(t)

	spec := client.Spec{
		Targets: []client.TargetSpec{{
			Name:     "labeled",
			Kind:     "oci",
			Endpoint: registryEndpoint,
			Auth:     client.AuthSpec{Mode: "none"},
			Discovery: client.DiscoverySpec{
				Source:       "static",
				Repositories: []string{"edge/labeled"},
			},
		}},
	}

	msgs := syncPlugin(t, spec, []string{"*"})
	tc := tableCounts(msgs)
	t.Logf("Labeled image counts: %+v", tc)

	// The labeled image was built with docker build from an ARM Mac so it's single-platform
	if tc["oci_artifacts"] != 1 {
		t.Errorf("expected 1 artifact, got %d", tc["oci_artifacts"])
	}
	if tc["oci_image_labels"] == 0 {
		t.Error("expected oci_image_labels rows for labeled image")
	}

	// Verify specific label keys are present
	labelKeys := columnValues(msgs, "oci_image_labels", "key")
	t.Logf("Label keys: %v", labelKeys)

	expectedKeys := map[string]bool{
		"org.opencontainers.image.title":   false,
		"org.opencontainers.image.version": false,
		"com.example.maintainer":           false,
		"com.example.description":          false,
	}
	for _, k := range labelKeys {
		if _, ok := expectedKeys[k]; ok {
			expectedKeys[k] = true
		}
	}
	for k, found := range expectedKeys {
		if !found {
			t.Errorf("expected label key %q not found in labels", k)
		}
	}
}

// TestMultiTagSameDigest verifies that when multiple tags point to the same
// digest, we get one artifact but multiple tags.
func TestMultiTagSameDigest(t *testing.T) {
	skipIfNoCluster(t)

	spec := client.Spec{
		Targets: []client.TargetSpec{{
			Name:     "multitag",
			Kind:     "oci",
			Endpoint: registryEndpoint,
			Auth:     client.AuthSpec{Mode: "none"},
			Discovery: client.DiscoverySpec{
				Source:       "static",
				Repositories: []string{"edge/multitag"},
			},
		}},
	}

	msgs := syncPlugin(t, spec, []string{"*"})
	tc := tableCounts(msgs)
	t.Logf("Multi-tag counts: %+v", tc)

	// 3 tags (latest, stable, v3.19) all point to the same digest
	if tc["oci_tags"] != 3 {
		t.Errorf("expected 3 tags, got %d", tc["oci_tags"])
	}
	// But only 1 unique artifact (dedup)
	if tc["oci_artifacts"] != 1 {
		t.Errorf("expected 1 artifact (dedup), got %d", tc["oci_artifacts"])
	}

	// Verify all 3 tag names appear
	tagNames := columnValues(msgs, "oci_tags", "name")
	t.Logf("Tag names: %v", tagNames)

	wantTags := map[string]bool{"latest": false, "stable": false, "v3.19": false}
	for _, name := range tagNames {
		if _, ok := wantTags[name]; ok {
			wantTags[name] = true
		}
	}
	for tag, found := range wantTags {
		if !found {
			t.Errorf("expected tag %q not found", tag)
		}
	}

	// Verify all tags point to the same digest
	digests := columnValues(msgs, "oci_tags", "digest")
	if len(digests) > 0 {
		first := digests[0]
		for _, d := range digests[1:] {
			if d != first {
				t.Errorf("expected all tags to share digest %s, but got %s", first, d)
			}
		}
	}
}

// TestNonexistentRepo verifies that syncing a nonexistent repo does not crash.
func TestNonexistentRepo(t *testing.T) {
	skipIfNoCluster(t)

	spec := client.Spec{
		Targets: []client.TargetSpec{{
			Name:     "nonexistent",
			Kind:     "oci",
			Endpoint: registryEndpoint,
			Auth:     client.AuthSpec{Mode: "none"},
			Discovery: client.DiscoverySpec{
				Source:       "static",
				Repositories: []string{"does/not/exist"},
			},
		}},
	}

	// Should not panic or return fatal error
	msgs := syncPlugin(t, spec, []string{"*"})
	tc := tableCounts(msgs)
	t.Logf("Nonexistent repo counts: %+v", tc)

	// We get 1 repository row (the static entry) but 0 artifacts/tags
	if tc["oci_repositories"] != 1 {
		t.Errorf("expected 1 repository row (static), got %d", tc["oci_repositories"])
	}
	if tc["oci_artifacts"] != 0 {
		t.Errorf("expected 0 artifacts for nonexistent repo, got %d", tc["oci_artifacts"])
	}
}

// TestEmptyStaticRepositories verifies that an empty repositories list produces no data.
func TestEmptyStaticRepositories(t *testing.T) {
	skipIfNoCluster(t)

	spec := client.Spec{
		Targets: []client.TargetSpec{{
			Name:     "empty",
			Kind:     "oci",
			Endpoint: registryEndpoint,
			Auth:     client.AuthSpec{Mode: "none"},
			Discovery: client.DiscoverySpec{
				Source:       "static",
				Repositories: []string{},
			},
		}},
	}

	msgs := syncPlugin(t, spec, []string{"*"})
	tc := tableCounts(msgs)
	t.Logf("Empty static repos counts: %+v", tc)

	if tc["oci_repositories"] != 0 {
		t.Errorf("expected 0 repositories, got %d", tc["oci_repositories"])
	}
	if tc["oci_tags"] != 0 {
		t.Errorf("expected 0 tags, got %d", tc["oci_tags"])
	}
}

// TestDeepNestedRepoPath verifies that repositories with deeply nested paths work.
func TestDeepNestedRepoPath(t *testing.T) {
	skipIfNoCluster(t)

	spec := client.Spec{
		Targets: []client.TargetSpec{{
			Name:     "deep-path",
			Kind:     "oci",
			Endpoint: registryEndpoint,
			Auth:     client.AuthSpec{Mode: "none"},
			Discovery: client.DiscoverySpec{
				Source:       "static",
				Repositories: []string{"org/team/project/deep-image"},
			},
		}},
	}

	msgs := syncPlugin(t, spec, []string{"*"})
	tc := tableCounts(msgs)
	t.Logf("Deep path counts: %+v", tc)

	if tc["oci_repositories"] != 1 {
		t.Errorf("expected 1 repository, got %d", tc["oci_repositories"])
	}
	if tc["oci_artifacts"] == 0 {
		t.Error("expected artifacts for deep-nested repo")
	}
	if tc["oci_tags"] == 0 {
		t.Error("expected tags for deep-nested repo")
	}

	// Verify the full repo path is preserved
	repos := columnValues(msgs, "oci_repositories", "full_name")
	found := false
	for _, r := range repos {
		if r == "org/team/project/deep-image" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected full repo name 'org/team/project/deep-image', got %v", repos)
	}
}

// TestHelmChartArtifact verifies that Helm OCI charts are detected with the
// correct artifact type and that their config blob (Chart.yaml as JSON) is fetched.
func TestHelmChartArtifact(t *testing.T) {
	skipIfNoCluster(t)

	spec := client.Spec{
		Targets: []client.TargetSpec{{
			Name:     "helm",
			Kind:     "oci",
			Endpoint: registryEndpoint,
			Auth:     client.AuthSpec{Mode: "none"},
			Discovery: client.DiscoverySpec{
				Source:       "static",
				Repositories: []string{"edge/test-chart"},
			},
		}},
	}

	msgs := syncPlugin(t, spec, []string{"*"})
	tc := tableCounts(msgs)
	t.Logf("Helm chart counts: %+v", tc)

	if tc["oci_artifacts"] != 1 {
		t.Errorf("expected 1 artifact for helm chart, got %d", tc["oci_artifacts"])
	}
	if tc["oci_index_children"] != 0 {
		t.Errorf("expected 0 index children for helm chart, got %d", tc["oci_index_children"])
	}

	// Verify artifact type is the Helm config media type
	artifactTypes := columnValues(msgs, "oci_artifacts", "artifact_type")
	t.Logf("Artifact types: %v", artifactTypes)
	foundHelm := false
	for _, at := range artifactTypes {
		if strings.Contains(at, "helm") {
			foundHelm = true
		}
	}
	if !foundHelm {
		t.Errorf("expected helm artifact type, got %v", artifactTypes)
	}

	// Helm chart manifests have OCI annotations
	if tc["oci_manifest_annotations"] == 0 {
		t.Error("expected manifest annotations on helm chart (title, version, etc.)")
	}

	// Verify specific annotation keys from helm push
	annoKeys := columnValues(msgs, "oci_manifest_annotations", "key")
	t.Logf("Annotation keys: %v", annoKeys)
	wantAnnotations := map[string]bool{
		"org.opencontainers.image.title":   false,
		"org.opencontainers.image.version": false,
	}
	for _, k := range annoKeys {
		if _, ok := wantAnnotations[k]; ok {
			wantAnnotations[k] = true
		}
	}
	for k, found := range wantAnnotations {
		if !found {
			t.Errorf("expected annotation %q not found", k)
		}
	}

	// Helm config blob should be fetched (isImageConfigMediaType matches helm)
	if tc["oci_image_configs"] != 1 {
		t.Errorf("expected 1 image config for helm chart, got %d", tc["oci_image_configs"])
	}

	// Layers: Helm chart has 1 content layer
	if tc["oci_layers"] != 1 {
		t.Errorf("expected 1 layer for helm chart, got %d", tc["oci_layers"])
	}

	// Verify layer media type
	layerTypes := columnValues(msgs, "oci_layers", "media_type")
	t.Logf("Layer media types: %v", layerTypes)
	foundChartLayer := false
	for _, mt := range layerTypes {
		if strings.Contains(mt, "helm.chart.content") {
			foundChartLayer = true
		}
	}
	if !foundChartLayer {
		t.Errorf("expected helm chart content layer, got %v", layerTypes)
	}
}

// TestSingleTableSync verifies that syncing only one table works without errors.
func TestSingleTableSync(t *testing.T) {
	skipIfNoCluster(t)

	spec := client.Spec{
		Targets: []client.TargetSpec{{
			Name:     "single-table",
			Kind:     "oci",
			Endpoint: registryEndpoint,
			Auth:     client.AuthSpec{Mode: "none"},
			Discovery: client.DiscoverySpec{
				Source:       "static",
				Repositories: []string{"edge/singleplat"},
			},
		}},
	}

	// Sync only oci_tags — this is a top-level table
	msgs := syncPlugin(t, spec, []string{"oci_tags"})
	tc := tableCounts(msgs)
	t.Logf("Single table sync (oci_tags): %+v", tc)

	if tc["oci_tags"] == 0 {
		t.Error("expected oci_tags data")
	}
	// Should not produce data for other tables
	if tc["oci_repositories"] != 0 {
		t.Errorf("unexpected oci_repositories data: %d", tc["oci_repositories"])
	}
	if tc["oci_artifacts"] != 0 {
		t.Errorf("unexpected oci_artifacts data: %d", tc["oci_artifacts"])
	}
}

// TestMixedRepos verifies syncing a target that mixes repos of different types
// (multi-arch, single-platform, helm chart) in one go.
func TestMixedRepos(t *testing.T) {
	skipIfNoCluster(t)

	spec := client.Spec{
		Targets: []client.TargetSpec{{
			Name:     "mixed",
			Kind:     "oci",
			Endpoint: registryEndpoint,
			Auth:     client.AuthSpec{Mode: "none"},
			Discovery: client.DiscoverySpec{
				Source: "static",
				Repositories: []string{
					"test/alpine",     // multi-arch
					"edge/singleplat", // single-platform
					"edge/labeled",    // single-platform with labels
					"edge/test-chart", // helm chart
				},
			},
		}},
	}

	msgs := syncPlugin(t, spec, []string{"*"})
	tc := tableCounts(msgs)
	t.Logf("Mixed repos counts: %+v", tc)

	if tc["oci_repositories"] != 4 {
		t.Errorf("expected 4 repositories, got %d", tc["oci_repositories"])
	}
	// We should have a mix of index children (from alpine) and direct layers
	if tc["oci_index_children"] == 0 {
		t.Error("expected index children from multi-arch alpine")
	}
	if tc["oci_layers"] == 0 {
		t.Error("expected layers from single-platform and helm images")
	}
	if tc["oci_image_labels"] == 0 {
		t.Error("expected labels from the labeled image")
	}
	if tc["oci_manifest_annotations"] == 0 {
		t.Error("expected annotations from helm chart")
	}
}

// TestInvalidSpec verifies that an invalid spec returns an error at configuration time.
func TestInvalidSpec(t *testing.T) {
	tests := []struct {
		name string
		spec string
	}{
		{
			name: "empty targets",
			spec: `{"targets": []}`,
		},
		{
			name: "missing endpoint",
			spec: `{"targets": [{"name": "x", "kind": "oci"}]}`,
		},
		{
			name: "unknown kind",
			spec: `{"targets": [{"name": "x", "kind": "unknown", "endpoint": "http://localhost"}]}`,
		},
	}

	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := internalPlugin.Configure(context.Background(), logger, []byte(tt.spec), plugin.NewClientOptions{})
			if err == nil {
				t.Error("expected error for invalid spec, got nil")
			}
			t.Logf("Got expected error: %v", err)
		})
	}
}

// TestUnreachableRegistry verifies that a target with an unreachable endpoint
// fails gracefully during sync, not at configuration time.
func TestUnreachableRegistry(t *testing.T) {
	spec := client.Spec{
		Targets: []client.TargetSpec{{
			Name:     "unreachable",
			Kind:     "oci",
			Endpoint: "http://localhost:19999", // nothing listening
			Auth:     client.AuthSpec{Mode: "none"},
			Discovery: client.DiscoverySpec{
				Source:       "static",
				Repositories: []string{"some/repo"},
			},
		}},
	}

	specJSON, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}

	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

	// Configuration should succeed (lazy connection)
	cl, err := internalPlugin.Configure(context.Background(), logger, specJSON, plugin.NewClientOptions{})
	if err != nil {
		t.Fatalf("Configure should succeed even for unreachable endpoint: %v", err)
	}
	defer cl.Close(context.Background())

	// Sync should handle unreachable endpoint gracefully
	ch := make(chan message.SyncMessage, 100)
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go func() {
		done <- cl.Sync(ctx, plugin.SyncOptions{Tables: []string{"*"}}, ch)
		close(ch)
	}()

	var msgs []message.SyncMessage
	for msg := range ch {
		msgs = append(msgs, msg)
	}
	syncErr := <-done

	// The sync may or may not error depending on how the resolvers handle it,
	// but it should NOT panic.
	t.Logf("Unreachable sync: err=%v, msgs=%d", syncErr, len(msgs))
}

// TestContextCancellation verifies that a cancelled context stops the sync gracefully.
func TestContextCancellation(t *testing.T) {
	skipIfNoCluster(t)

	spec := client.Spec{
		Targets: []client.TargetSpec{{
			Name:     "cancel-test",
			Kind:     "oci",
			Endpoint: registryEndpoint,
			Auth:     client.AuthSpec{Mode: "none"},
			Discovery: client.DiscoverySpec{
				Source: "catalog", // full catalog = many repos = gives time to cancel
			},
		}},
	}

	specJSON, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}

	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

	cl, err := internalPlugin.Configure(context.Background(), logger, specJSON, plugin.NewClientOptions{})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	defer cl.Close(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan message.SyncMessage, 1000)
	done := make(chan error, 1)

	go func() {
		done <- cl.Sync(ctx, plugin.SyncOptions{Tables: []string{"*"}}, ch)
		close(ch)
	}()

	// Cancel almost immediately
	cancel()

	var msgs []message.SyncMessage
	for msg := range ch {
		msgs = append(msgs, msg)
	}
	syncErr := <-done

	// Should not panic. May return context cancelled error.
	t.Logf("Cancelled sync: err=%v, msgs=%d", syncErr, len(msgs))
}

// TestMixedExistingAndNonexistentRepos verifies that a target with a mix
// of valid and invalid repos processes the valid ones and skips invalid.
func TestMixedExistingAndNonexistentRepos(t *testing.T) {
	skipIfNoCluster(t)

	spec := client.Spec{
		Targets: []client.TargetSpec{{
			Name:     "mixed-exist",
			Kind:     "oci",
			Endpoint: registryEndpoint,
			Auth:     client.AuthSpec{Mode: "none"},
			Discovery: client.DiscoverySpec{
				Source: "static",
				Repositories: []string{
					"edge/singleplat",  // exists
					"does/not/exist",   // does not exist
					"edge/labeled",     // exists
					"also/nonexistent", // does not exist
				},
			},
		}},
	}

	msgs := syncPlugin(t, spec, []string{"*"})
	tc := tableCounts(msgs)
	t.Logf("Mixed existing/nonexistent counts: %+v", tc)

	// All 4 repos appear as repository rows (static discovery)
	if tc["oci_repositories"] != 4 {
		t.Errorf("expected 4 repository rows, got %d", tc["oci_repositories"])
	}
	// But only 2 repos have artifacts
	if tc["oci_artifacts"] < 2 {
		t.Errorf("expected at least 2 artifacts from valid repos, got %d", tc["oci_artifacts"])
	}
}

// TestIncremental_OCI verifies that pushing a new image between syncs
// produces additional artifacts and tags on the second sync.
func TestIncremental_OCI(t *testing.T) {
	skipIfNoCluster(t)

	repoName := "incr/oci-test"

	// Push a first image
	pushTestImage(t, repoName, "v1")

	spec := client.Spec{
		Targets: []client.TargetSpec{{
			Name:     "incr-oci",
			Kind:     "oci",
			Endpoint: registryEndpoint,
			Auth:     client.AuthSpec{Mode: "none"},
			Discovery: client.DiscoverySpec{
				Source:       "static",
				Repositories: []string{repoName},
			},
		}},
	}

	// First sync
	msgs1 := syncPlugin(t, spec, []string{"*"})
	tc1 := tableCounts(msgs1)
	t.Logf("Incremental OCI sync 1 counts: %+v", tc1)

	if tc1["oci_tags"] != 1 {
		t.Errorf("sync 1: expected 1 tag, got %d", tc1["oci_tags"])
	}

	// Push a second image with a new tag (different digest via busybox)
	dst := fmt.Sprintf("localhost:30004/%s:v2", repoName)
	cmd := exec.Command("crane", "copy", "docker.io/library/busybox:1.36", dst, "--insecure")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("push second image: %v\n%s", err, out)
	}

	// Second sync
	msgs2 := syncPlugin(t, spec, []string{"*"})
	tc2 := tableCounts(msgs2)
	t.Logf("Incremental OCI sync 2 counts: %+v", tc2)

	if tc2["oci_tags"] != 2 {
		t.Errorf("sync 2: expected 2 tags (v1+v2), got %d", tc2["oci_tags"])
	}
	if tc2["oci_artifacts"] <= tc1["oci_artifacts"] {
		t.Errorf("sync 2: expected more artifacts than sync 1 (%d vs %d)", tc2["oci_artifacts"], tc1["oci_artifacts"])
	}

	// Verify both tags appear
	tagNames := columnValues(msgs2, "oci_tags", "name")
	wantTags := map[string]bool{"v1": false, "v2": false}
	for _, name := range tagNames {
		if _, ok := wantTags[name]; ok {
			wantTags[name] = true
		}
	}
	for tag, found := range wantTags {
		if !found {
			t.Errorf("sync 2: expected tag %q not found", tag)
		}
	}
}
