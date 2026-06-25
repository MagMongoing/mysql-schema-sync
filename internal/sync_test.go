// Copyright(C) 2022 github.com/fsgo  All Rights Reserved.
// Author: hidu <duv123@gmail.com>
// Date: 2022/9/25

package internal

import (
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
		name string
		sc   *SchemaSync
		args args
		want string
	}{
		{
			name: "user 0-1",
			args: args{
				table:   "user",
				sSchema: testLoadFile("testdata/user/user_0.sql"),
				dSchema: testLoadFile("testdata/user/user_1.sql"),
				cfg:     &Config{},
			},
			sc: &SchemaSync{
				Config: &Config{},
			},
			want: testLoadFile("testdata/user/result_1.sql"),
		},
		{
			name: "user 0-1 ssc",
			args: args{
				table:   "user",
				sSchema: testLoadFile("testdata/user/user_0.sql"),
				dSchema: testLoadFile("testdata/user/user_1.sql"),
				cfg: &Config{
					SingleSchemaChange: true,
				},
			},
			sc: &SchemaSync{
				Config: &Config{},
			},
			want: testLoadFile("testdata/user/result_2.sql"),
		},
		{
			name: "user 1-0 ssc",
			args: args{
				table:   "user",
				sSchema: testLoadFile("testdata/user/user_1.sql"),
				dSchema: testLoadFile("testdata/user/user_0.sql"),
				cfg: &Config{
					SingleSchemaChange: true,
				},
			},
			sc: &SchemaSync{
				Config: &Config{},
			},
			want: testLoadFile("testdata/user/result_3.sql"),
		},
		{
			name: "user 2-0 ssc",
			args: args{
				table:   "user",
				sSchema: testLoadFile("testdata/user/user_2.sql"),
				dSchema: testLoadFile("testdata/user/user_0.sql"),
				cfg:     &Config{},
			},
			sc: &SchemaSync{
				Config: &Config{},
			},
			want: testLoadFile("testdata/user/result_4.sql"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.sc.getAlterDataBySchema(tt.args.table, tt.args.sSchema, tt.args.dSchema, tt.args.cfg)
			t.Log("got alter:\n", got.String())
			xt.Equal(t, tt.want, got.String())
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
		for _, sql := range got.SQL {
			if strings.Contains(sql, "drop") {
				t.Errorf("Drop disabled, should not generate DROP, got: %s", sql)
			}
		}
	})

	t.Run("Drop enabled - extra field is dropped", func(t *testing.T) {
		sc := &SchemaSync{Config: &Config{Drop: true}}
		cfg := &Config{Drop: true}
		got := sc.getAlterDataBySchema("user", sourceSchema, destSchema, cfg)
		t.Logf("alter result: %s", got.String())
		hasDrop := false
		for _, sql := range got.SQL {
			if strings.Contains(sql, "drop `extra_field`") {
				hasDrop = true
			}
		}
		if !hasDrop {
			t.Error("Drop enabled, expected DROP for extra_field")
		}
	})
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
		// Should contain CHANGE statements for timestamp fields
		hasChange := false
		for _, sql := range got.SQL {
			if len(sql) > 0 {
				hasChange = true
			}
		}
		if !hasChange {
			t.Error("expected CHANGE SQL to be generated")
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
	})
}
