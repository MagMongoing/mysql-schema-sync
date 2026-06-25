package main

import (
	"reflect"
	"testing"

	"github.com/hidu/mysql-schema-sync/internal"
)

func TestApplyCLIOverridesPreservesConfigWhenFlagsNotVisited(t *testing.T) {
	cfg := &internal.Config{
		SingleSchemaChange: true,
		Tables:             []string{"from_config"},
		TablesIgnore:       []string{"ignored_config"},
	}
	applyCLIOverrides(cfg, map[string]bool{})
	if !cfg.SingleSchemaChange {
		t.Fatal("config single_schema_change was overwritten by the flag default")
	}
	if !reflect.DeepEqual(cfg.Tables, []string{"from_config"}) {
		t.Fatalf("config tables changed unexpectedly: %v", cfg.Tables)
	}
	if !reflect.DeepEqual(cfg.TablesIgnore, []string{"ignored_config"}) {
		t.Fatalf("config tables_ignore changed unexpectedly: %v", cfg.TablesIgnore)
	}
}

func TestApplyCLIOverridesReplacesTableLists(t *testing.T) {
	oldTables, oldTablesIgnore := *tables, *tablesIgnore
	t.Cleanup(func() {
		*tables = oldTables
		*tablesIgnore = oldTablesIgnore
	})
	*tables = "cli_a,cli_b"
	*tablesIgnore = "cli_ignore"

	cfg := &internal.Config{
		Tables:       []string{"from_config"},
		TablesIgnore: []string{"ignored_config"},
	}
	applyCLIOverrides(cfg, map[string]bool{"tables": true, "tables_ignore": true})
	if !reflect.DeepEqual(cfg.Tables, []string{"cli_a", "cli_b"}) {
		t.Fatalf("tables were not replaced: %v", cfg.Tables)
	}
	if !reflect.DeepEqual(cfg.TablesIgnore, []string{"cli_ignore"}) {
		t.Fatalf("tables_ignore were not replaced: %v", cfg.TablesIgnore)
	}
}

func TestApplyCLIOverridesCanClearMailRecipients(t *testing.T) {
	oldMailTo := *mailTo
	t.Cleanup(func() { *mailTo = oldMailTo })

	*mailTo = ""
	cfg := &internal.Config{
		Email: &internal.EmailStruct{To: "ops@example.com"},
	}
	applyCLIOverrides(cfg, map[string]bool{"mail_to": true})
	if cfg.Email.To != "" {
		t.Fatalf("explicit empty -mail_to did not clear configured recipients: %q", cfg.Email.To)
	}
}
