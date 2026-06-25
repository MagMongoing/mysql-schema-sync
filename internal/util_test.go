package internal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeIntegerType(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Basic integer types with display width
		{
			name:     "int with display width",
			input:    "int(11)",
			expected: "int",
		},
		{
			name:     "bigint with display width",
			input:    "bigint(20)",
			expected: "bigint",
		},
		{
			name:     "tinyint with display width",
			input:    "tinyint(1)",
			expected: "tinyint",
		},
		{
			name:     "tinyint(4) with display width",
			input:    "tinyint(4)",
			expected: "tinyint",
		},
		{
			name:     "smallint with display width",
			input:    "smallint(5)",
			expected: "smallint",
		},
		{
			name:     "mediumint with display width",
			input:    "mediumint(8)",
			expected: "mediumint",
		},

		// Integer types with unsigned modifier
		{
			name:     "int(11) unsigned",
			input:    "int(11) unsigned",
			expected: "int unsigned",
		},
		{
			name:     "bigint(20) unsigned",
			input:    "bigint(20) unsigned",
			expected: "bigint unsigned",
		},
		{
			name:     "tinyint(1) unsigned",
			input:    "tinyint(1) unsigned",
			expected: "tinyint unsigned",
		},

		// Integer types with zerofill modifier
		{
			name:     "int(11) zerofill",
			input:    "int(11) zerofill",
			expected: "int zerofill",
		},
		{
			name:     "int(10) unsigned zerofill",
			input:    "int(10) unsigned zerofill",
			expected: "int unsigned zerofill",
		},

		// Integer types without display width (already normalized)
		{
			name:     "int without display width",
			input:    "int",
			expected: "int",
		},
		{
			name:     "bigint without display width",
			input:    "bigint",
			expected: "bigint",
		},
		{
			name:     "int unsigned without display width",
			input:    "int unsigned",
			expected: "int unsigned",
		},

		// Non-integer types (should not be affected)
		{
			name:     "varchar with length",
			input:    "varchar(255)",
			expected: "varchar(255)",
		},
		{
			name:     "char with length",
			input:    "char(10)",
			expected: "char(10)",
		},
		{
			name:     "decimal with precision",
			input:    "decimal(10,2)",
			expected: "decimal(10,2)",
		},
		{
			name:     "text type",
			input:    "text",
			expected: "text",
		},
		{
			name:     "timestamp",
			input:    "timestamp",
			expected: "timestamp",
		},

		// Case insensitive matching
		{
			name:     "INT(11) uppercase",
			input:    "INT(11)",
			expected: "INT",
		},
		{
			name:     "BIGINT(20) UNSIGNED uppercase",
			input:    "BIGINT(20) UNSIGNED",
			expected: "BIGINT UNSIGNED",
		},
		{
			name:     "TinyInt(1) mixed case",
			input:    "TinyInt(1)",
			expected: "TinyInt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeIntegerType(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeIntegerType(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSimpleMatch(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		str     string
		want    bool
	}{
		{"exact match", "users", "users", true},
		{"no match", "users", "orders", false},
		{"wildcard suffix", "order_*", "order_items", true},
		{"wildcard suffix no match", "order_*", "user_items", false},
		{"wildcard prefix", "*_log", "access_log", true},
		{"wildcard both", "*order*", "my_order_table", true},
		{"star matches all", "*", "anything", true},
		{"empty pattern", "", "", true},
		{"empty pattern vs non-empty", "", "x", false},
		{"spaces trimmed", " users ", "users", true},
		{"multiple wildcards", "t_*_*_log", "t_access_2024_log", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := simpleMatch(tt.pattern, tt.str)
			if got != tt.want {
				t.Errorf("simpleMatch(%q, %q) = %v, want %v", tt.pattern, tt.str, got, tt.want)
			}
		})
	}
}

func TestDsnShort(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{"normal DSN", "user:pass@tcp(127.0.0.1:3306)/db", "tcp(127.0.0.1:3306)/db"},
		{"empty DSN", "", ""},
		{"no @ sign", "invalid-dsn", "<invalid DSN>"},
		{"@ at start", "@tcp(host)/db", "<invalid DSN>"},
		{"multiple @", "user@host@extra", "extra"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dsnShort(tt.dsn)
			if got != tt.want {
				t.Errorf("dsnShort(%q) = %q, want %q", tt.dsn, got, tt.want)
			}
		})
	}
}

func TestLoadJSONFile(t *testing.T) {
	t.Run("valid JSON with comments", func(t *testing.T) {
		dir := t.TempDir()
		fp := filepath.Join(dir, "test.json")
		content := `# This is a comment
// Another comment
{
  "name": "test",
  "value": 42
}`
		if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		var result struct {
			Name  string `json:"name"`
			Value int    `json:"value"`
		}
		if err := loadJSONFile(fp, &result); err != nil {
			t.Fatalf("loadJSONFile() error = %v", err)
		}
		if result.Name != "test" || result.Value != 42 {
			t.Errorf("loadJSONFile() got %+v, want {Name:test Value:42}", result)
		}
	})

	t.Run("file not found", func(t *testing.T) {
		err := loadJSONFile("/nonexistent/path.json", &struct{}{})
		if err == nil {
			t.Error("loadJSONFile() expected error for missing file")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		dir := t.TempDir()
		fp := filepath.Join(dir, "bad.json")
		if err := os.WriteFile(fp, []byte(`{not valid`), 0644); err != nil {
			t.Fatal(err)
		}
		err := loadJSONFile(fp, &struct{}{})
		if err == nil {
			t.Error("loadJSONFile() expected error for invalid JSON")
		}
	})
}

func TestQuoteIdentifier(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"simple name", "users", "`users`"},
		{"with backtick", "col`name", "`col``name`"},
		{"multiple backticks", "a`b`c", "`a``b``c`"},
		{"empty string", "", "``"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := quoteIdentifier(tt.in)
			if got != tt.want {
				t.Errorf("quoteIdentifier(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestMaskDSNPassword(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{"no password", "user@tcp(host:3306)/db", "user@tcp(host:3306)/db"},
		{"simple password", "user:pass@tcp(host:3306)/db", "user:***@tcp(host:3306)/db"},
		{"password with @", "user:p@ssword@tcp(host:3306)/db", "user:***@tcp(host:3306)/db"},
		{"password with multiple @", "user:a@b@c@tcp(host:3306)/db", "user:***@tcp(host:3306)/db"},
		{"empty DSN", "", ""},
		{"no @ sign", "invalid-dsn", "invalid-dsn"},
		{"colon but no @", "user:pass", "user:pass"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maskDSNPassword(tt.dsn)
			if got != tt.want {
				t.Errorf("maskDSNPassword(%q) = %q, want %q", tt.dsn, got, tt.want)
			}
		})
	}
}
