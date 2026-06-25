// Copyright(C) 2022 github.com/fsgo  All Rights Reserved.
// Author: hidu <duv123@gmail.com>
// Date: 2022/9/25

package internal

import (
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
