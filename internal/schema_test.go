// Copyright(C) 2022 github.com/fsgo  All Rights Reserved.
// Author: hidu <duv123@gmail.com>
// Date: 2022/9/25

package internal

import (
	"os"
	"testing"

	"github.com/xanygo/anygo/ds/xmap"
	"github.com/xanygo/anygo/xt"
)

// testLoadFile reads a test fixture file. H9: uses t.Helper() + t.Fatalf
// so failures point at the calling test (not the helper) and only fail
// the affected subtest rather than aborting the whole test binary.
func testLoadFile(t *testing.T, name string) string {
	t.Helper()
	bf, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s failed: %v", name, err)
	}
	return string(bf)
}

func TestParseSchema(t *testing.T) {
	type args struct {
		schema string
	}
	tests := []struct {
		name string
		args args
		want *MySchema
	}{
		{
			name: "case 1",
			args: args{
				schema: testLoadFile(t, "testdata/user/user_0.sql"),
			},
			want: &MySchema{
				Fields: (func() xmap.Ordered[string, string] {
					m := xmap.Ordered[string, string]{}
					m.Set("id", "`id` bigint unsigned NOT NULL AUTO_INCREMENT")
					m.Set("email", "`email` varchar(1000) NOT NULL DEFAULT ''")
					m.Set("register_time", "`register_time` timestamp NOT NULL")
					m.Set("password", "`password` varchar(1000) NOT NULL DEFAULT ''")
					m.Set("status", "`status` tinyint unsigned NOT NULL DEFAULT '0'")
					return m
				})(),
				IndexAll: map[string]*DbIndex{
					"PRIMARY KEY": {
						Name:      "PRIMARY KEY",
						SQL:       "PRIMARY KEY (`id`)",
						IndexType: indexTypePrimary,
					},
				},
			},
		},
		{
			name: "case 2",
			args: args{
				schema: testLoadFile(t, "testdata/user/user_4.sql"),
			},
			want: &MySchema{
				Fields: (func() xmap.Ordered[string, string] {
					m := xmap.Ordered[string, string]{}
					m.Set("id", "\"id\" bigint unsigned NOT NULL AUTO_INCREMENT")
					m.Set("email", "\"email\" varchar(1000) NOT NULL DEFAULT \"\"")
					m.Set("register_time", "\"register_time\" timestamp NOT NULL")
					m.Set("password", "\"password\" varchar(1000) NOT NULL DEFAULT \"\"")
					m.Set("status", "\"status\" tinyint unsigned NOT NULL DEFAULT \"0\"")
					return m
				})(),
				IndexAll: map[string]*DbIndex{
					"PRIMARY KEY": {
						Name:      "PRIMARY KEY",
						SQL:       "PRIMARY KEY (\"id\")",
						IndexType: indexTypePrimary,
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSchema(tt.args.schema)
			gs := got.String()
			ws := tt.want.String()
			xt.Equal(t, ws, gs)
			// M21: also verify structural equality (field count, index count).
			if got.Fields.Len() != tt.want.Fields.Len() {
				t.Errorf("Fields count: got %d, want %d", got.Fields.Len(), tt.want.Fields.Len())
			}
			if len(got.IndexAll) != len(tt.want.IndexAll) {
				t.Errorf("IndexAll count: got %d, want %d", len(got.IndexAll), len(tt.want.IndexAll))
			}
		})
	}
}

func TestExtractQuotedIdentifier(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		quote  byte
		want   string
		wantOK bool
	}{
		{"simple backtick", "`name` varchar(255)", '`', "name", true},
		{"doubled backtick", "`col``name` int NOT NULL", '`', "col`name", true},
		{"multiple doubled", "`a``b``c` text", '`', "a`b`c", true},
		{"no closing quote", "`unclosed", '`', "", false},
		{"double quote simple", "\"id\" bigint", '"', "id", true},
		{"double quote doubled", "\"col\"\"name\" text", '"', "col\"name", true},
		// M16: empty identifier `` is valid (ok=true), distinct from malformed (ok=false).
		{"empty identifier", "`` int", '`', "", true},
		// Additional edge cases
		{"empty input string", "", '`', "", false},
		{"only single quote char", "`", '`', "", false},
		{"trailing doubled quote no close", "`a``", '`', "", false},
		{"double-quote type on backtick", "\"name\" int", '"', "name", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := extractQuotedIdentifier(tt.line, tt.quote)
			if got != tt.want {
				t.Errorf("extractQuotedIdentifier(%q, %q) = %q, want %q", tt.line, tt.quote, got, tt.want)
			}
			if ok != tt.wantOK {
				t.Errorf("extractQuotedIdentifier(%q, %q) ok = %v, want %v", tt.line, tt.quote, ok, tt.wantOK)
			}
		})
	}
}

// TestParseSchema_NonNil asserts ParseSchema never returns nil (even for empty input).
func TestParseSchema_NonNil(t *testing.T) {
	s := ParseSchema("")
	if s == nil {
		t.Fatal("ParseSchema(\"\") returned nil; expected non-nil *MySchema")
	}
	s2 := ParseSchema("CREATE TABLE t (\n  `id` int\n) ENGINE=InnoDB")
	if s2 == nil {
		t.Fatal("ParseSchema returned nil for valid input")
	}
}

func TestParseDbIndexLineLowercaseKeywords(t *testing.T) {
	fk := parseDbIndexLine("constraint `fk_parent` foreign key (`parent_id`) references `parent` (`id`)")
	if fk == nil || fk.IndexType != indexTypeForeignKey || fk.Name != "fk_parent" {
		t.Fatalf("lowercase foreign key was not parsed: %#v", fk)
	}
	if len(fk.RelationTables) != 1 || fk.RelationTables[0] != "parent" {
		t.Fatalf("lowercase REFERENCES table was not parsed: %#v", fk.RelationTables)
	}
	if len(fk.ReferencedColumns) != 1 || fk.ReferencedColumns[0] != "id" {
		t.Fatalf("referenced columns were not parsed: %#v", fk.ReferencedColumns)
	}

	check := parseDbIndexLine("constraint `chk_positive` check ((`value` > 0))")
	if check == nil || check.IndexType != indexTypeCheck || check.Name != "chk_positive" {
		t.Fatalf("lowercase check constraint was not parsed: %#v", check)
	}
}

func TestParseDbIndexLineCompositeReferencedColumns(t *testing.T) {
	fk := parseDbIndexLine(
		"CONSTRAINT `fk_composite` FOREIGN KEY (`tenant_id`,`parent_id`) REFERENCES `parent` (`tenant_id`,`id`)",
	)
	if fk == nil {
		t.Fatal("composite foreign key was not parsed")
	}
	if len(fk.ReferencedColumns) != 2 ||
		fk.ReferencedColumns[0] != "tenant_id" ||
		fk.ReferencedColumns[1] != "id" {
		t.Fatalf("composite referenced columns were not parsed: %#v", fk.ReferencedColumns)
	}
}
