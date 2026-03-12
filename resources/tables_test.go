package resources

import (
	"testing"

	"github.com/cloudquery/plugin-sdk/v4/schema"
)

func TestTables(t *testing.T) {
	tables := Tables()

	if len(tables) == 0 {
		t.Fatal("expected tables, got none")
	}

	tableNames := map[string]bool{}
	var walk func(tt schema.Tables)
	walk = func(tt schema.Tables) {
		for _, table := range tt {
			tableNames[table.Name] = true
			walk(table.Relations)
		}
	}
	walk(tables)

	expected := []string{
		"oci_repositories",
		"oci_artifacts",
		"oci_tags",
		"oci_layers",
		"oci_index_children",
		"oci_manifest_annotations",
		"oci_image_configs",
		"oci_image_labels",
		"oci_image_history",
		"oci_referrers",
	}

	for _, name := range expected {
		if !tableNames[name] {
			t.Errorf("expected table %q not found", name)
		}
	}

	t.Logf("Found %d tables total", len(tableNames))
}
