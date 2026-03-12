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

	// Verify IsIncremental is set on the right tables
	incrementalTables := map[string]bool{}
	var walkIncremental func(tt schema.Tables)
	walkIncremental = func(tt schema.Tables) {
		for _, table := range tt {
			if table.IsIncremental {
				incrementalTables[table.Name] = true
			}
			walkIncremental(table.Relations)
		}
	}
	walkIncremental(tables)

	wantIncremental := []string{"oci_artifacts", "oci_tags"}
	for _, name := range wantIncremental {
		if !incrementalTables[name] {
			t.Errorf("expected table %q to be incremental", name)
		}
	}

	// Verify IncrementalKey is set on digest columns for incremental tables
	var walkIncrementalKeys func(tt schema.Tables)
	walkIncrementalKeys = func(tt schema.Tables) {
		for _, table := range tt {
			if table.IsIncremental {
				for _, col := range table.Columns {
					if col.Name == "digest" && !col.IncrementalKey {
						t.Errorf("table %q: digest column should have IncrementalKey", table.Name)
					}
				}
			}
			walkIncrementalKeys(table.Relations)
		}
	}
	walkIncrementalKeys(tables)
}
