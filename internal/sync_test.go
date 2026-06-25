// Copyright(C) 2022 github.com/fsgo  All Rights Reserved.
// Author: hidu <duv123@gmail.com>
// Date: 2022/9/25

package internal

import (
	"regexp"
	"strings"
	"testing"

	"github.com/xanygo/anygo/xt"
)

func TestSchemaSync_getAlterDataBySchema(t *testing.T) {
	type args struct {
		table   string
		sSchema string
		dSchema string
		cfg     *Config
	}
	tests := []struct {
		name     string
		sc       *SchemaSync
		argsFunc func(t *testing.T) args
		wantFunc func(t *testing.T) string
	}{
		{
			name: "user 0-1",
			argsFunc: func(t *testing.T) args {
				return args{
					table:   "user",
					sSchema: testLoadFile(t, "testdata/user/user_0.sql"),
					dSchema: testLoadFile(t, "testdata/user/user_1.sql"),
					cfg:     &Config{},
				}
			},
			sc: &SchemaSync{
				Config: &Config{},
			},
			wantFunc: func(t *testing.T) string {
				return testLoadFile(t, "testdata/user/result_1.sql")
			},
		},
		{
			name: "user 0-1 ssc",
			argsFunc: func(t *testing.T) args {
				return args{
					table:   "user",
					sSchema: testLoadFile(t, "testdata/user/user_0.sql"),
					dSchema: testLoadFile(t, "testdata/user/user_1.sql"),
					cfg: &Config{
						SingleSchemaChange: true,
					},
				}
			},
			sc: &SchemaSync{
				Config: &Config{},
			},
			wantFunc: func(t *testing.T) string {
				return testLoadFile(t, "testdata/user/result_2.sql")
			},
		},
		{
			name: "user 1-0 ssc",
			argsFunc: func(t *testing.T) args {
				return args{
					table:   "user",
					sSchema: testLoadFile(t, "testdata/user/user_1.sql"),
					dSchema: testLoadFile(t, "testdata/user/user_0.sql"),
					cfg: &Config{
						SingleSchemaChange: true,
					},
				}
			},
			sc: &SchemaSync{
				Config: &Config{},
			},
			wantFunc: func(t *testing.T) string {
				return testLoadFile(t, "testdata/user/result_3.sql")
			},
		},
		{
			name: "user 2-0 ssc",
			argsFunc: func(t *testing.T) args {
				return args{
					table:   "user",
					sSchema: testLoadFile(t, "testdata/user/user_2.sql"),
					dSchema: testLoadFile(t, "testdata/user/user_0.sql"),
					cfg:     &Config{},
				}
			},
			sc: &SchemaSync{
				Config: &Config{},
			},
			wantFunc: func(t *testing.T) string {
				return testLoadFile(t, "testdata/user/result_4.sql")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := tt.argsFunc(t)
			want := tt.wantFunc(t)
			got := tt.sc.getAlterDataBySchema(args.table, args.sSchema, args.dSchema, args.cfg)
			t.Log("got alter:\n", got.String())
			xt.Equal(t, want, got.String())
		})
	}
}

func TestSchemaSync_FieldOrder(t *testing.T) {
	// Source: fields in order id, name, email
	sourceSchema := "CREATE TABLE `user` (\n  `id` bigint NOT NULL AUTO_INCREMENT,\n  `name` varchar(100) NOT NULL,\n  `email` varchar(200) NOT NULL,\n  PRIMARY KEY (`id`)\n)"
	// Dest: fields in order id, email, name (different order)
	destSchema := "CREATE TABLE `user` (\n  `id` bigint NOT NULL AUTO_INCREMENT,\n  `email` varchar(200) NOT NULL,\n  `name` varchar(100) NOT NULL,\n  PRIMARY KEY (`id`)\n)"

	sourceFields := map[string]*FieldInfo{
		"id":    {ColumnName: "id", ColumnType: "bigint", DataType: "bigint", IsNullAble: "NO", OrdinalPosition: 1, Extra: "auto_increment"},
		"name":  {ColumnName: "name", ColumnType: "varchar(100)", DataType: "varchar", IsNullAble: "NO", OrdinalPosition: 2},
		"email": {ColumnName: "email", ColumnType: "varchar(200)", DataType: "varchar", IsNullAble: "NO", OrdinalPosition: 3},
	}
	destFields := map[string]*FieldInfo{
		"id":    {ColumnName: "id", ColumnType: "bigint", DataType: "bigint", IsNullAble: "NO", OrdinalPosition: 1, Extra: "auto_increment"},
		"email": {ColumnName: "email", ColumnType: "varchar(200)", DataType: "varchar", IsNullAble: "NO", OrdinalPosition: 2},
		"name":  {ColumnName: "name", ColumnType: "varchar(100)", DataType: "varchar", IsNullAble: "NO", OrdinalPosition: 3},
	}

	t.Run("FieldOrder disabled - no MODIFY for order difference", func(t *testing.T) {
		sc := &SchemaSync{Config: &Config{FieldOrder: false}}
		cfg := &Config{FieldOrder: false}
		sd := sc.getAlterDataBySchema("user", sourceSchema, destSchema, cfg)
		sd.SchemaDiff = NewSchemaDiffWithFieldInfos("user",
			RemoveTableSchemaConfig(sourceSchema), RemoveTableSchemaConfig(destSchema),
			sourceFields, destFields)
		diffLines := sc.getSchemaDiff(sd)
		t.Logf("diffLines: %v", diffLines)
		for _, line := range diffLines {
			if strings.Contains(line, "MODIFY COLUMN") {
				t.Errorf("FieldOrder disabled, should not generate MODIFY COLUMN, got: %s", line)
			}
		}
	})

	t.Run("FieldOrder enabled - MODIFY for order difference", func(t *testing.T) {
		sc := &SchemaSync{Config: &Config{FieldOrder: true}}
		cfg := &Config{FieldOrder: true}
		sd := sc.getAlterDataBySchema("user", sourceSchema, destSchema, cfg)
		sd.SchemaDiff = NewSchemaDiffWithFieldInfos("user",
			RemoveTableSchemaConfig(sourceSchema), RemoveTableSchemaConfig(destSchema),
			sourceFields, destFields)
		diffLines := sc.getSchemaDiff(sd)
		t.Logf("diffLines: %v", diffLines)
		hasModify := false
		for _, line := range diffLines {
			if strings.Contains(line, "MODIFY COLUMN") {
				hasModify = true
			}
		}
		if !hasModify {
			t.Error("FieldOrder enabled, expected MODIFY COLUMN for order difference")
		}
	})
}

func TestSchemaSync_Drop(t *testing.T) {
	// Source: only has id, name
	sourceSchema := "CREATE TABLE `user` (\n  `id` bigint NOT NULL AUTO_INCREMENT,\n  `name` varchar(100) NOT NULL,\n  PRIMARY KEY (`id`)\n)"
	// Dest: has id, name, extra_field (extra column)
	destSchema := "CREATE TABLE `user` (\n  `id` bigint NOT NULL AUTO_INCREMENT,\n  `name` varchar(100) NOT NULL,\n  `extra_field` varchar(50) DEFAULT NULL,\n  PRIMARY KEY (`id`)\n)"

	t.Run("Drop disabled - extra field not dropped", func(t *testing.T) {
		sc := &SchemaSync{Config: &Config{Drop: false}}
		cfg := &Config{Drop: false}
		got := sc.getAlterDataBySchema("user", sourceSchema, destSchema, cfg)
		t.Logf("alter result: %s", got.String())
		// Catch all DROP forms emitted by index.go: DROP COLUMN, DROP INDEX,
		// DROP FOREIGN KEY, DROP CHECK, DROP PRIMARY KEY, plus the bare
		// ``DROP `name``` form for columns. Any drop must be flagged when
		// Drop=false.
		dropRe := regexp.MustCompile(`(?i)\bdrop\s+(column|index|foreign\s+key|check|primary\s+key|` + "`" + `)`)
		for _, sql := range got.SQL {
			if dropRe.MatchString(sql) {
				t.Errorf("Drop disabled, should not generate DROP, got: %s", sql)
			}
		}
	})

	t.Run("Drop enabled - extra field is dropped", func(t *testing.T) {
		sc := &SchemaSync{Config: &Config{Drop: true}}
		cfg := &Config{Drop: true}
		got := sc.getAlterDataBySchema("user", sourceSchema, destSchema, cfg)
		t.Logf("alter result: %s", got.String())
		// M17: use case-insensitive regex to match both "drop `col`" and
		// "DROP COLUMN `col`" forms, avoiding brittleness on keyword casing.
		dropExtraRe := regexp.MustCompile(`(?i)drop\s+(?:column\s+)?` + "`" + `extra_field` + "`")
		hasDrop := false
		for _, sql := range got.SQL {
			if dropExtraRe.MatchString(sql) {
				hasDrop = true
			}
		}
		if !hasDrop {
			t.Error("Drop enabled, expected DROP for extra_field")
		}
	})
}

func TestIgnoredMissingFieldIsNotUsedAsAfterAnchor(t *testing.T) {
	source := "CREATE TABLE `users` (\n" +
		"  `id` int NOT NULL,\n" +
		"  `ignored_col` int NOT NULL,\n" +
		"  `new_col` varchar(20) NOT NULL,\n" +
		"  PRIMARY KEY (`id`)\n" +
		")"
	dest := "CREATE TABLE `users` (\n" +
		"  `id` int NOT NULL,\n" +
		"  PRIMARY KEY (`id`)\n" +
		")"
	cfg := &Config{AlterIgnore: map[string]*AlterIgnoreTable{
		"users": {Column: []string{"ignored_col"}},
	}}
	sc := &SchemaSync{Config: cfg}
	got := sc.getAlterDataBySchema("users", source, dest, cfg)
	if len(got.SQL) != 1 {
		t.Fatalf("expected one ALTER statement, got %v", got.SQL)
	}
	if strings.Contains(got.SQL[0], "AFTER `ignored_col`") {
		t.Fatalf("new field references ignored destination-missing field: %s", got.SQL[0])
	}
	if !strings.Contains(got.SQL[0], "AFTER `id`") {
		t.Fatalf("new field should anchor after existing id: %s", got.SQL[0])
	}
}

func TestIndexReplacementRemainsAtomicWithSingleSchemaChange(t *testing.T) {
	source := "CREATE TABLE `users` (\n" +
		"  `id` int NOT NULL,\n" +
		"  `email` varchar(100) NOT NULL,\n" +
		"  PRIMARY KEY (`id`),\n" +
		"  UNIQUE KEY `uq_email` (`email`)\n" +
		")"
	dest := "CREATE TABLE `users` (\n" +
		"  `id` int NOT NULL,\n" +
		"  `email` varchar(100) NOT NULL,\n" +
		"  PRIMARY KEY (`id`),\n" +
		"  KEY `uq_email` (`email`)\n" +
		")"
	cfg := &Config{SingleSchemaChange: true}
	sc := &SchemaSync{Config: cfg}
	got := sc.getAlterDataBySchema("users", source, dest, cfg)
	if len(got.SQL) != 1 {
		t.Fatalf("index replacement must remain one ALTER, got %v", got.SQL)
	}
	upper := strings.ToUpper(got.SQL[0])
	if !strings.Contains(upper, "DROP INDEX `UQ_EMAIL`") ||
		!strings.Contains(upper, "ADD UNIQUE KEY `UQ_EMAIL`") {
		t.Fatalf("index replacement is not atomic: %s", got.SQL[0])
	}
}

func TestSchemaSync_SkipTimestampToDatetime(t *testing.T) {
	// Source (production): has timestamp field
	sourceSchema := "CREATE TABLE `orders` (\n  `id` bigint NOT NULL AUTO_INCREMENT,\n  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,\n  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,\n  `name` varchar(100) NOT NULL DEFAULT '',\n  PRIMARY KEY (`id`)\n)"

	// Dest (test): has datetime field instead of timestamp
	destSchema := "CREATE TABLE `orders` (\n  `id` bigint NOT NULL AUTO_INCREMENT,\n  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,\n  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,\n  `name` varchar(100) NOT NULL DEFAULT '',\n  PRIMARY KEY (`id`)\n)"

	t.Run("without skip flag - should generate CHANGE", func(t *testing.T) {
		sc := &SchemaSync{Config: &Config{}}
		cfg := &Config{}
		got := sc.getAlterDataBySchema("orders", sourceSchema, destSchema, cfg)
		t.Log("alter result:\n", got.String())
		if got.Type == alterTypeNo {
			t.Error("expected alter, got no change")
		}
		// Verify a CHANGE statement is actually emitted for at least one of the
		// timestamp columns — a generic non-empty check would let unrelated SQL pass.
		hasChange := false
		for _, sql := range got.SQL {
			lower := strings.ToLower(sql)
			if strings.Contains(lower, "change ") &&
				(strings.Contains(lower, "created_at") || strings.Contains(lower, "updated_at")) {
				hasChange = true
				break
			}
		}
		if !hasChange {
			t.Errorf("expected CHANGE SQL targeting created_at/updated_at, got SQL=%v", got.SQL)
		}
	})

	t.Run("with skip flag - should NOT generate CHANGE for timestamp fields", func(t *testing.T) {
		sc := &SchemaSync{Config: &Config{SkipTimestampToDatetime: true}}
		cfg := &Config{SkipTimestampToDatetime: true}
		got := sc.getAlterDataBySchema("orders", sourceSchema, destSchema, cfg)
		t.Log("alter result:\n", got.String())
		if got.Type != alterTypeNo {
			t.Errorf("expected no change, got type=%s, sql=%v", got.Type, got.SQL)
		}
	})

	t.Run("with skip flag but other differences - should generate CHANGE for non-timestamp fields", func(t *testing.T) {
		destSchemaDiff := "CREATE TABLE `orders` (\n  `id` bigint NOT NULL AUTO_INCREMENT,\n  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,\n  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,\n  `name` varchar(200) NOT NULL DEFAULT '',\n  PRIMARY KEY (`id`)\n)"
		sc := &SchemaSync{Config: &Config{SkipTimestampToDatetime: true}}
		cfg := &Config{SkipTimestampToDatetime: true}
		got := sc.getAlterDataBySchema("orders", sourceSchema, destSchemaDiff, cfg)
		t.Log("alter result:\n", got.String())
		if got.Type == alterTypeNo {
			t.Error("expected alter for varchar difference, got no change")
		}
		// L33: negative assertion — verify no CHANGE for timestamp/datetime fields.
		for _, sql := range got.SQL {
			lower := strings.ToLower(sql)
			if strings.Contains(lower, "change ") &&
				(strings.Contains(lower, "created_at") || strings.Contains(lower, "updated_at")) {
				t.Errorf("SkipTimestampToDatetime should suppress CHANGE for timestamp fields, got: %s", sql)
			}
		}
		// Also verify that a CHANGE for the name column IS present.
		hasNameChange := false
		for _, sql := range got.SQL {
			if strings.Contains(strings.ToLower(sql), "name") && strings.Contains(strings.ToLower(sql), "change ") {
				hasNameChange = true
			}
		}
		if !hasNameChange {
			t.Error("expected CHANGE for name (varchar) difference, but none found")
		}
	})
}

// TestSchemaSync_SkipTimestampToDatetime_StructuredPath verifies the SkipTimestampToDatetime
// feature through the structured FieldInfo comparison path (production path). M5.
func TestSchemaSync_SkipTimestampToDatetime_StructuredPath(t *testing.T) {
	sourceSchema := "CREATE TABLE `orders` (\n  `id` bigint NOT NULL AUTO_INCREMENT,\n  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,\n  `name` varchar(100) NOT NULL DEFAULT '',\n  PRIMARY KEY (`id`)\n)"
	destSchema := "CREATE TABLE `orders` (\n  `id` bigint NOT NULL AUTO_INCREMENT,\n  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,\n  `name` varchar(100) NOT NULL DEFAULT '',\n  PRIMARY KEY (`id`)\n)"

	sourceFields := map[string]*FieldInfo{
		"id":         {ColumnName: "id", ColumnType: "bigint", DataType: "bigint", IsNullAble: "NO", OrdinalPosition: 1, Extra: "auto_increment"},
		"created_at": {ColumnName: "created_at", ColumnType: "timestamp", DataType: "timestamp", IsNullAble: "NO", OrdinalPosition: 2, ColumnDefault: stringPtr("CURRENT_TIMESTAMP")},
		"name":       {ColumnName: "name", ColumnType: "varchar(100)", DataType: "varchar", IsNullAble: "NO", OrdinalPosition: 3, ColumnDefault: stringPtr("")},
	}
	destFields := map[string]*FieldInfo{
		"id":         {ColumnName: "id", ColumnType: "bigint", DataType: "bigint", IsNullAble: "NO", OrdinalPosition: 1, Extra: "auto_increment"},
		"created_at": {ColumnName: "created_at", ColumnType: "datetime", DataType: "datetime", IsNullAble: "NO", OrdinalPosition: 2, ColumnDefault: stringPtr("CURRENT_TIMESTAMP")},
		"name":       {ColumnName: "name", ColumnType: "varchar(100)", DataType: "varchar", IsNullAble: "NO", OrdinalPosition: 3, ColumnDefault: stringPtr("")},
	}

	t.Run("skip flag suppresses timestamp→datetime in structured path", func(t *testing.T) {
		sc := &SchemaSync{Config: &Config{SkipTimestampToDatetime: true}}
		cfg := &Config{SkipTimestampToDatetime: true}
		sd := sc.getAlterDataBySchema("orders", sourceSchema, destSchema, cfg)
		sd.SchemaDiff = NewSchemaDiffWithFieldInfos("orders",
			RemoveTableSchemaConfig(sourceSchema), RemoveTableSchemaConfig(destSchema),
			sourceFields, destFields)
		diffLines := sc.getSchemaDiff(sd)
		t.Logf("diffLines: %v", diffLines)
		for _, line := range diffLines {
			lower := strings.ToLower(line)
			if strings.Contains(lower, "created_at") {
				t.Errorf("SkipTimestampToDatetime should suppress CHANGE for created_at in structured path, got: %s", line)
			}
		}
	})

	t.Run("without skip flag, timestamp→datetime generates CHANGE in structured path", func(t *testing.T) {
		sc := &SchemaSync{Config: &Config{}}
		cfg := &Config{}
		sd := sc.getAlterDataBySchema("orders", sourceSchema, destSchema, cfg)
		sd.SchemaDiff = NewSchemaDiffWithFieldInfos("orders",
			RemoveTableSchemaConfig(sourceSchema), RemoveTableSchemaConfig(destSchema),
			sourceFields, destFields)
		diffLines := sc.getSchemaDiff(sd)
		t.Logf("diffLines: %v", diffLines)
		hasChange := false
		for _, line := range diffLines {
			if strings.Contains(strings.ToLower(line), "created_at") && strings.Contains(strings.ToLower(line), "change ") {
				hasChange = true
			}
		}
		if !hasChange {
			t.Error("expected CHANGE for created_at in structured path without skip flag")
		}
	})
}

// TestSchemaSync_StructuredPath exercises the production FieldInfo comparison
// path through getSchemaDiff. M7.
func TestSchemaSync_StructuredPath(t *testing.T) {
	sourceSchema := "CREATE TABLE `users` (\n  `id` bigint NOT NULL AUTO_INCREMENT,\n  `name` varchar(100) NOT NULL,\n  `email` varchar(200) NOT NULL DEFAULT '',\n  PRIMARY KEY (`id`)\n)"
	destSchema := "CREATE TABLE `users` (\n  `id` bigint NOT NULL AUTO_INCREMENT,\n  `name` varchar(200) NOT NULL,\n  `email` varchar(200) NOT NULL DEFAULT '',\n  PRIMARY KEY (`id`)\n)"

	sourceFields := map[string]*FieldInfo{
		"id":    {ColumnName: "id", ColumnType: "bigint", DataType: "bigint", IsNullAble: "NO", OrdinalPosition: 1, Extra: "auto_increment"},
		"name":  {ColumnName: "name", ColumnType: "varchar(100)", DataType: "varchar", IsNullAble: "NO", OrdinalPosition: 2},
		"email": {ColumnName: "email", ColumnType: "varchar(200)", DataType: "varchar", IsNullAble: "NO", OrdinalPosition: 3, ColumnDefault: stringPtr("")},
	}
	destFields := map[string]*FieldInfo{
		"id":    {ColumnName: "id", ColumnType: "bigint", DataType: "bigint", IsNullAble: "NO", OrdinalPosition: 1, Extra: "auto_increment"},
		"name":  {ColumnName: "name", ColumnType: "varchar(200)", DataType: "varchar", IsNullAble: "NO", OrdinalPosition: 2},
		"email": {ColumnName: "email", ColumnType: "varchar(200)", DataType: "varchar", IsNullAble: "NO", OrdinalPosition: 3, ColumnDefault: stringPtr("")},
	}

	t.Run("varchar length change detected via structured path", func(t *testing.T) {
		sc := &SchemaSync{Config: &Config{}}
		cfg := &Config{}
		sd := sc.getAlterDataBySchema("users", sourceSchema, destSchema, cfg)
		sd.SchemaDiff = NewSchemaDiffWithFieldInfos("users",
			RemoveTableSchemaConfig(sourceSchema), RemoveTableSchemaConfig(destSchema),
			sourceFields, destFields)
		diffLines := sc.getSchemaDiff(sd)
		t.Logf("diffLines: %v", diffLines)
		hasNameChange := false
		for _, line := range diffLines {
			if strings.Contains(strings.ToLower(line), "name") && strings.Contains(strings.ToLower(line), "change ") {
				hasNameChange = true
			}
		}
		if !hasNameChange {
			t.Error("expected CHANGE for name (varchar 100→200) via structured path")
		}
	})

	t.Run("identical fields produce no diff via structured path", func(t *testing.T) {
		sc := &SchemaSync{Config: &Config{}}
		cfg := &Config{}
		sd := sc.getAlterDataBySchema("users", sourceSchema, sourceSchema, cfg)
		sd.SchemaDiff = NewSchemaDiffWithFieldInfos("users",
			RemoveTableSchemaConfig(sourceSchema), RemoveTableSchemaConfig(sourceSchema),
			sourceFields, sourceFields)
		diffLines := sc.getSchemaDiff(sd)
		if len(diffLines) > 0 {
			t.Errorf("identical fields should produce no diff, got: %v", diffLines)
		}
	})
}
