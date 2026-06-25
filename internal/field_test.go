// Copyright(C) 2022 github.com/fsgo  All Rights Reserved.
// Author: hidu <duv123@gmail.com>
// Date: 2022/9/25

package internal

import (
	"testing"

	"github.com/xanygo/anygo/xt"
)

func TestFieldInfo_Equals(t *testing.T) {
	tests := []struct {
		name   string
		field1 *FieldInfo
		field2 *FieldInfo
		equal  bool
	}{
		{
			name: "identical fields",
			field1: &FieldInfo{
				ColumnName:    "name",
				ColumnType:    "varchar(64)",
				IsNullAble:    "NO",
				CharsetName:   stringPtr("utf8mb4"),
				CollationName: stringPtr("utf8mb4_general_ci"),
			},
			field2: &FieldInfo{
				ColumnName:    "name",
				ColumnType:    "varchar(64)",
				IsNullAble:    "NO",
				CharsetName:   stringPtr("utf8mb4"),
				CollationName: stringPtr("utf8mb4_general_ci"),
			},
			equal: true,
		},
		{
			name: "same field with and without explicit charset/collation",
			field1: &FieldInfo{
				ColumnName:    "name",
				ColumnType:    "varchar(64)",
				IsNullAble:    "NO",
				CharsetName:   nil,
				CollationName: nil,
			},
			field2: &FieldInfo{
				ColumnName:    "name",
				ColumnType:    "varchar(64)",
				IsNullAble:    "NO",
				CharsetName:   stringPtr("utf8mb4"),
				CollationName: stringPtr("utf8mb4_general_ci"),
			},
			equal: true,
		},
		{
			name: "different field type",
			field1: &FieldInfo{
				ColumnName:    "name",
				ColumnType:    "varchar(64)",
				IsNullAble:    "NO",
				CharsetName:   stringPtr("utf8mb4"),
				CollationName: stringPtr("utf8mb4_general_ci"),
			},
			field2: &FieldInfo{
				ColumnName:    "name",
				ColumnType:    "varchar(128)",
				IsNullAble:    "NO",
				CharsetName:   stringPtr("utf8mb4"),
				CollationName: stringPtr("utf8mb4_general_ci"),
			},
			equal: false,
		},
		{
			name: "different nullable",
			field1: &FieldInfo{
				ColumnName:    "name",
				ColumnType:    "varchar(64)",
				IsNullAble:    "NO",
				CharsetName:   stringPtr("utf8mb4"),
				CollationName: stringPtr("utf8mb4_general_ci"),
			},
			field2: &FieldInfo{
				ColumnName:    "name",
				ColumnType:    "varchar(64)",
				IsNullAble:    "YES",
				CharsetName:   stringPtr("utf8mb4"),
				CollationName: stringPtr("utf8mb4_general_ci"),
			},
			equal: false,
		},
		{
			name: "nil charset and collation vs explicit utf8mb4_unicode_ci — both tolerated",
			field1: &FieldInfo{
				ColumnName:    "name",
				ColumnType:    "varchar(64)",
				IsNullAble:    "NO",
				CharsetName:   nil,
				CollationName: nil,
			},
			field2: &FieldInfo{
				ColumnName:    "name",
				ColumnType:    "varchar(64)",
				IsNullAble:    "NO",
				CharsetName:   stringPtr("utf8mb4"),
				CollationName: stringPtr("utf8mb4_unicode_ci"),
			},
			equal: true,
		},
		// Integer type display width tests (MySQL 5.7 vs 8.0 compatibility)
		{
			name: "int(11) vs int should be equal",
			field1: &FieldInfo{
				ColumnName: "id",
				ColumnType: "int(11)",
				DataType:   "int",
				IsNullAble: "NO",
			},
			field2: &FieldInfo{
				ColumnName: "id",
				ColumnType: "int",
				DataType:   "int",
				IsNullAble: "NO",
			},
			equal: true,
		},
		{
			name: "bigint(20) vs bigint should be equal",
			field1: &FieldInfo{
				ColumnName: "user_id",
				ColumnType: "bigint(20)",
				DataType:   "bigint",
				IsNullAble: "NO",
			},
			field2: &FieldInfo{
				ColumnName: "user_id",
				ColumnType: "bigint",
				DataType:   "bigint",
				IsNullAble: "NO",
			},
			equal: true,
		},
		{
			name: "tinyint(1) vs tinyint should be equal",
			field1: &FieldInfo{
				ColumnName: "is_active",
				ColumnType: "tinyint(1)",
				DataType:   "tinyint",
				IsNullAble: "NO",
			},
			field2: &FieldInfo{
				ColumnName: "is_active",
				ColumnType: "tinyint",
				DataType:   "tinyint",
				IsNullAble: "NO",
			},
			equal: true,
		},
		{
			name: "tinyint(4) vs tinyint should be equal",
			field1: &FieldInfo{
				ColumnName: "status",
				ColumnType: "tinyint(4)",
				DataType:   "tinyint",
				IsNullAble: "NO",
			},
			field2: &FieldInfo{
				ColumnName: "status",
				ColumnType: "tinyint",
				DataType:   "tinyint",
				IsNullAble: "NO",
			},
			equal: true,
		},
		{
			name: "int(11) unsigned vs int unsigned should be equal",
			field1: &FieldInfo{
				ColumnName: "count",
				ColumnType: "int(11) unsigned",
				DataType:   "int",
				IsNullAble: "NO",
			},
			field2: &FieldInfo{
				ColumnName: "count",
				ColumnType: "int unsigned",
				DataType:   "int",
				IsNullAble: "NO",
			},
			equal: true,
		},
		{
			name: "bigint(20) unsigned vs bigint unsigned should be equal",
			field1: &FieldInfo{
				ColumnName: "total",
				ColumnType: "bigint(20) unsigned",
				DataType:   "bigint",
				IsNullAble: "NO",
			},
			field2: &FieldInfo{
				ColumnName: "total",
				ColumnType: "bigint unsigned",
				DataType:   "bigint",
				IsNullAble: "NO",
			},
			equal: true,
		},
		{
			name: "int(10) zerofill vs int zerofill should be equal",
			field1: &FieldInfo{
				ColumnName: "order_id",
				ColumnType: "int(10) zerofill",
				DataType:   "int",
				IsNullAble: "NO",
			},
			field2: &FieldInfo{
				ColumnName: "order_id",
				ColumnType: "int zerofill",
				DataType:   "int",
				IsNullAble: "NO",
			},
			equal: true,
		},
		{
			name: "int(10) unsigned zerofill vs int unsigned zerofill should be equal",
			field1: &FieldInfo{
				ColumnName: "code",
				ColumnType: "int(10) unsigned zerofill",
				DataType:   "int",
				IsNullAble: "NO",
			},
			field2: &FieldInfo{
				ColumnName: "code",
				ColumnType: "int unsigned zerofill",
				DataType:   "int",
				IsNullAble: "NO",
			},
			equal: true,
		},
		{
			name: "int vs bigint should not be equal",
			field1: &FieldInfo{
				ColumnName: "value",
				ColumnType: "int",
				DataType:   "int",
				IsNullAble: "NO",
			},
			field2: &FieldInfo{
				ColumnName: "value",
				ColumnType: "bigint",
				DataType:   "bigint",
				IsNullAble: "NO",
			},
			equal: false,
		},
		{
			name: "int unsigned vs int should not be equal (unsigned modifier difference)",
			field1: &FieldInfo{
				ColumnName: "amount",
				ColumnType: "int unsigned",
				DataType:   "int",
				IsNullAble: "NO",
			},
			field2: &FieldInfo{
				ColumnName: "amount",
				ColumnType: "int",
				DataType:   "int",
				IsNullAble: "NO",
			},
			equal: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.field1.Equals(tt.field2)
			xt.Equal(t, tt.equal, got)
		})
	}
}

func TestFieldInfo_String(t *testing.T) {
	tests := []struct {
		name  string
		field *FieldInfo
		want  string
	}{
		{
			name: "simple varchar field",
			field: &FieldInfo{
				ColumnName:    "name",
				ColumnType:    "varchar(64)",
				IsNullAble:    "NO",
				DataType:      "varchar",
				CharsetName:   stringPtr("utf8mb4"),
				CollationName: stringPtr("utf8mb4_general_ci"),
			},
			want: "`name` varchar(64) NOT NULL",
		},
		{
			name: "field with default value",
			field: &FieldInfo{
				ColumnName:    "status",
				ColumnType:    "tinyint",
				IsNullAble:    "NO",
				DataType:      "tinyint",
				ColumnDefault: stringPtr("0"),
				CharsetName:   nil,
				CollationName: nil,
			},
			want: "`status` tinyint NOT NULL DEFAULT 0",
		},
		{
			name: "varchar field with string default value",
			field: &FieldInfo{
				ColumnName:    "f_status",
				ColumnType:    "varchar(32)",
				IsNullAble:    "NO",
				DataType:      "varchar",
				ColumnDefault: stringPtr("queue"),
				CharsetName:   stringPtr("utf8mb3"),
				CollationName: stringPtr("utf8mb3_general_ci"),
			},
			want: "`f_status` varchar(32) NOT NULL DEFAULT 'queue'",
		},
		{
			name: "char field with string default value",
			field: &FieldInfo{
				ColumnName:    "type",
				ColumnType:    "char(10)",
				IsNullAble:    "NO",
				DataType:      "char",
				ColumnDefault: stringPtr("active"),
			},
			want: "`type` char(10) NOT NULL DEFAULT 'active'",
		},
		{
			name: "text field with string default value",
			field: &FieldInfo{
				ColumnName:    "description",
				ColumnType:    "text",
				IsNullAble:    "YES",
				DataType:      "text",
				ColumnDefault: stringPtr("default text"),
				CharsetName:   stringPtr("utf8mb4"),
				CollationName: stringPtr("utf8mb4_general_ci"),
			},
			want: "`description` text NULL DEFAULT 'default text'",
		},
		{
			name: "int field with numeric default value",
			field: &FieldInfo{
				ColumnName:    "count",
				ColumnType:    "int",
				IsNullAble:    "NO",
				DataType:      "int",
				ColumnDefault: stringPtr("100"),
			},
			want: "`count` int NOT NULL DEFAULT 100",
		},
		{
			name: "nullable field",
			field: &FieldInfo{
				ColumnName:    "description",
				ColumnType:    "text",
				IsNullAble:    "YES",
				DataType:      "text",
				CharsetName:   stringPtr("utf8mb4"),
				CollationName: stringPtr("utf8mb4_general_ci"),
			},
			want: "`description` text NULL",
		},
		{
			name: "timestamp with auto update",
			field: &FieldInfo{
				ColumnName:    "updated_at",
				ColumnType:    "timestamp",
				IsNullAble:    "NO",
				DataType:      "timestamp",
				ColumnDefault: stringPtr("CURRENT_TIMESTAMP"),
				Extra:         "on update CURRENT_TIMESTAMP",
				CharsetName:   nil,
				CollationName: nil,
			},
			want: "`updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP",
		},
		{
			name: "generated column VIRTUAL",
			field: &FieldInfo{
				ColumnName:           "full_name",
				ColumnType:           "varchar(201)",
				IsNullAble:           "YES",
				DataType:             "varchar",
				GenerationExpression: "concat(`first_name`,' ',`last_name`)",
				Extra:                "VIRTUAL GENERATED",
			},
			want: "`full_name` varchar(201) GENERATED ALWAYS AS (concat(`first_name`,' ',`last_name`)) NULL VIRTUAL",
		},
		{
			name: "generated column STORED NOT NULL",
			field: &FieldInfo{
				ColumnName:           "total",
				ColumnType:           "decimal(12,2)",
				IsNullAble:           "NO",
				DataType:             "decimal",
				GenerationExpression: "`price` * `quantity`",
				Extra:                "STORED GENERATED",
			},
			want: "`total` decimal(12,2) GENERATED ALWAYS AS (`price` * `quantity`) NOT NULL STORED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.field.String()
			xt.Equal(t, tt.want, got)
		})
	}
}

// Helper function to create string pointers
func stringPtr(s string) *string {
	return &s
}

func TestIsTimestampDatetimeEquivalent(t *testing.T) {
	tests := []struct {
		name   string
		source *FieldInfo
		dest   *FieldInfo
		want   bool
	}{
		{
			name: "timestamp vs datetime, same everything else - should skip",
			source: &FieldInfo{
				ColumnName: "created_at",
				ColumnType: "timestamp",
				DataType:   "timestamp",
				IsNullAble: "NO",
			},
			dest: &FieldInfo{
				ColumnName: "created_at",
				ColumnType: "datetime",
				DataType:   "datetime",
				IsNullAble: "NO",
			},
			want: true,
		},
		{
			name: "datetime vs timestamp (reverse direction) - should NOT skip",
			source: &FieldInfo{
				ColumnName: "created_at",
				ColumnType: "datetime",
				DataType:   "datetime",
				IsNullAble: "NO",
			},
			dest: &FieldInfo{
				ColumnName: "created_at",
				ColumnType: "timestamp",
				DataType:   "timestamp",
				IsNullAble: "NO",
			},
			want: false,
		},
		{
			name: "timestamp vs datetime with same default and extra - should skip",
			source: &FieldInfo{
				ColumnName:    "updated_at",
				ColumnType:    "timestamp",
				DataType:      "timestamp",
				IsNullAble:    "NO",
				ColumnDefault: stringPtr("CURRENT_TIMESTAMP"),
				Extra:         "on update CURRENT_TIMESTAMP",
			},
			dest: &FieldInfo{
				ColumnName:    "updated_at",
				ColumnType:    "datetime",
				DataType:      "datetime",
				IsNullAble:    "NO",
				ColumnDefault: stringPtr("CURRENT_TIMESTAMP"),
				Extra:         "on update CURRENT_TIMESTAMP",
			},
			want: true,
		},
		{
			name: "timestamp vs datetime with different nullability - should NOT skip",
			source: &FieldInfo{
				ColumnName: "created_at",
				ColumnType: "timestamp",
				DataType:   "timestamp",
				IsNullAble: "NO",
			},
			dest: &FieldInfo{
				ColumnName: "created_at",
				ColumnType: "datetime",
				DataType:   "datetime",
				IsNullAble: "YES",
			},
			want: false,
		},
		{
			name: "timestamp vs datetime with different defaults - should NOT skip",
			source: &FieldInfo{
				ColumnName:    "created_at",
				ColumnType:    "timestamp",
				DataType:      "timestamp",
				IsNullAble:    "NO",
				ColumnDefault: stringPtr("CURRENT_TIMESTAMP"),
			},
			dest: &FieldInfo{
				ColumnName:    "created_at",
				ColumnType:    "datetime",
				DataType:      "datetime",
				IsNullAble:    "NO",
				ColumnDefault: stringPtr("2024-01-01 00:00:00"),
			},
			want: false,
		},
		{
			name: "varchar vs varchar - should NOT skip (not timestamp/datetime)",
			source: &FieldInfo{
				ColumnName: "name",
				ColumnType: "varchar(64)",
				DataType:   "varchar",
				IsNullAble: "NO",
			},
			dest: &FieldInfo{
				ColumnName: "name",
				ColumnType: "varchar(128)",
				DataType:   "varchar",
				IsNullAble: "NO",
			},
			want: false,
		},
		{
			name: "timestamp(3) vs datetime(3) with same precision - should skip",
			source: &FieldInfo{
				ColumnName: "created_at",
				ColumnType: "timestamp(3)",
				DataType:   "timestamp",
				IsNullAble: "NO",
			},
			dest: &FieldInfo{
				ColumnName: "created_at",
				ColumnType: "datetime(3)",
				DataType:   "datetime",
				IsNullAble: "NO",
			},
			want: true,
		},
		{
			name: "timestamp vs datetime(3) with different precision - should NOT skip",
			source: &FieldInfo{
				ColumnName: "created_at",
				ColumnType: "timestamp",
				DataType:   "timestamp",
				IsNullAble: "NO",
			},
			dest: &FieldInfo{
				ColumnName: "created_at",
				ColumnType: "datetime(3)",
				DataType:   "datetime",
				IsNullAble: "NO",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTimestampDatetimeEquivalent(tt.source, tt.dest)
			xt.Equal(t, tt.want, got)
		})
	}
}

func TestFieldInfo_String_CurrentTimestampPrecision(t *testing.T) {
	tests := []struct {
		name  string
		field *FieldInfo
		want  string
	}{
		{
			name: "CURRENT_TIMESTAMP(3) with fractional seconds",
			field: &FieldInfo{
				ColumnName:    "created_at",
				ColumnType:    "timestamp(3)",
				IsNullAble:    "NO",
				DataType:      "timestamp",
				ColumnDefault: stringPtr("CURRENT_TIMESTAMP(3)"),
				Extra:         "on update CURRENT_TIMESTAMP(3)",
			},
			want: "`created_at` timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3)",
		},
		{
			name: "CURRENT_TIMESTAMP(6) microsecond precision",
			field: &FieldInfo{
				ColumnName:    "created_at",
				ColumnType:    "datetime(6)",
				IsNullAble:    "YES",
				DataType:      "datetime",
				ColumnDefault: stringPtr("CURRENT_TIMESTAMP(6)"),
			},
			want: "`created_at` datetime(6) NULL DEFAULT CURRENT_TIMESTAMP(6)",
		},
		{
			name: "DEFAULT_GENERATED filtered from Extra (MySQL 8.0)",
			field: &FieldInfo{
				ColumnName:    "updated_at",
				ColumnType:    "timestamp",
				IsNullAble:    "NO",
				DataType:      "timestamp",
				ColumnDefault: stringPtr("CURRENT_TIMESTAMP"),
				Extra:         "DEFAULT_GENERATED on update CURRENT_TIMESTAMP",
			},
			want: "`updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP",
		},
		{
			name: "DEFAULT_GENERATED alone in Extra is filtered out completely",
			field: &FieldInfo{
				ColumnName:    "created_at",
				ColumnType:    "timestamp",
				IsNullAble:    "NO",
				DataType:      "timestamp",
				ColumnDefault: stringPtr("CURRENT_TIMESTAMP"),
				Extra:         "DEFAULT_GENERATED",
			},
			want: "`created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP",
		},
		{
			name: "field with comment containing single quote",
			field: &FieldInfo{
				ColumnName:    "status",
				ColumnType:    "tinyint",
				IsNullAble:    "NO",
				DataType:      "tinyint",
				ColumnDefault: stringPtr("0"),
				ColumnComment: "user's status",
			},
			want: "`status` tinyint NOT NULL DEFAULT 0 COMMENT 'user''s status'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.field.String()
			xt.Equal(t, tt.want, got)
		})
	}
}

func TestIsTextTimestampDatetimeSkip(t *testing.T) {
	tests := []struct {
		name       string
		sourceText string
		destText   string
		want       bool
	}{
		{
			name:       "simple timestamp vs datetime",
			sourceText: "`created_at` timestamp NOT NULL",
			destText:   "`created_at` datetime NOT NULL",
			want:       true,
		},
		{
			name:       "timestamp with default vs datetime with default",
			sourceText: "`updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP",
			destText:   "`updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP",
			want:       true,
		},
		{
			name:       "reverse: datetime vs timestamp - should NOT skip",
			sourceText: "`created_at` datetime NOT NULL",
			destText:   "`created_at` timestamp NOT NULL",
			want:       false,
		},
		{
			name:       "different nullability - should NOT skip",
			sourceText: "`created_at` timestamp NOT NULL",
			destText:   "`created_at` datetime NULL",
			want:       false,
		},
		{
			name:       "varchar vs varchar - should NOT skip",
			sourceText: "`name` varchar(64) NOT NULL",
			destText:   "`name` varchar(128) NOT NULL",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTextTimestampDatetimeSkip(tt.sourceText, tt.destText)
			xt.Equal(t, tt.want, got)
		})
	}
}

// TestFieldInfo_Equals_SameCharsetDifferentCollation verifies that fields
// with matching charset but different collation are NOT equal. M5.
func TestFieldInfo_Equals_SameCharsetDifferentCollation(t *testing.T) {
	field1 := &FieldInfo{
		ColumnName:    "name",
		ColumnType:    "varchar(64)",
		DataType:      "varchar",
		IsNullAble:    "NO",
		CharsetName:   stringPtr("utf8mb4"),
		CollationName: stringPtr("utf8mb4_general_ci"),
	}
	field2 := &FieldInfo{
		ColumnName:    "name",
		ColumnType:    "varchar(64)",
		DataType:      "varchar",
		IsNullAble:    "NO",
		CharsetName:   stringPtr("utf8mb4"),
		CollationName: stringPtr("utf8mb4_unicode_ci"),
	}
	if field1.Equals(field2) {
		t.Error("fields with same charset but different collation should NOT be equal")
	}
	if field2.Equals(field1) {
		t.Error("symmetry: fields with same charset but different collation should NOT be equal")
	}
}

// TestFieldInfo_Equals_DifferentComment verifies that fields differing
// only in ColumnComment are NOT equal. L13.
func TestFieldInfo_Equals_DifferentComment(t *testing.T) {
	field1 := &FieldInfo{
		ColumnName:    "status",
		ColumnType:    "tinyint",
		DataType:      "tinyint",
		IsNullAble:    "NO",
		ColumnComment: "old comment",
	}
	field2 := &FieldInfo{
		ColumnName:    "status",
		ColumnType:    "tinyint",
		DataType:      "tinyint",
		IsNullAble:    "NO",
		ColumnComment: "new comment",
	}
	if field1.Equals(field2) {
		t.Error("fields differing only in ColumnComment should NOT be equal")
	}

	// Same comment should be equal
	field3 := &FieldInfo{
		ColumnName:    "status",
		ColumnType:    "tinyint",
		DataType:      "tinyint",
		IsNullAble:    "NO",
		ColumnComment: "same comment",
	}
	field4 := &FieldInfo{
		ColumnName:    "status",
		ColumnType:    "tinyint",
		DataType:      "tinyint",
		IsNullAble:    "NO",
		ColumnComment: "same comment",
	}
	if !field3.Equals(field4) {
		t.Error("fields with same ColumnComment should be equal")
	}
}
