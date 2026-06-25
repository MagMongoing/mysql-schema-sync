package internal

import (
	"os"
	"path/filepath"
	"strings"
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
		// M7: DSN query params stripped to avoid leaking sensitive values.
		{"DSN with query params", "user:pass@tcp(127.0.0.1:3306)/db?tls=true&serverPubKey=secret", "tcp(127.0.0.1:3306)/db"},
		// H8: password containing ? should not confuse query-param stripping.
		{"password with question mark", "user:p?w@tcp(host:3306)/db?tls=true", "tcp(host:3306)/db"},
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

	// H8: size cap — files exceeding maxConfigSize must be rejected.
	t.Run("oversized file rejected", func(t *testing.T) {
		dir := t.TempDir()
		fp := filepath.Join(dir, "big.json")
		// Write maxConfigSize + 2 bytes of content
		bigContent := strings.Repeat("x", maxConfigSize+2)
		if err := os.WriteFile(fp, []byte(bigContent), 0644); err != nil {
			t.Fatal(err)
		}
		err := loadJSONFile(fp, &struct{}{})
		if err == nil {
			t.Error("loadJSONFile() expected error for oversized file")
		}
		if err != nil && !strings.Contains(err.Error(), "exceeds maximum size") {
			t.Errorf("expected size-limit error, got: %v", err)
		}
	})

	// H8: line preservation — comments should be replaced with blank lines
	// so that JSON error line numbers match the original file.
	t.Run("comment lines preserved as blank lines", func(t *testing.T) {
		dir := t.TempDir()
		fp := filepath.Join(dir, "commented.json")
		// Lines 1-2 are comments, line 3 has the JSON object.
		content := "# comment line 1\n// comment line 2\n{\"source\": \"a\", \"dest\": \"b\"}\n"
		if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		var result struct {
			Source string `json:"source"`
			Dest   string `json:"dest"`
		}
		if err := loadJSONFile(fp, &result); err != nil {
			t.Fatalf("loadJSONFile() error = %v", err)
		}
		if result.Source != "a" || result.Dest != "b" {
			t.Errorf("got %+v, want {Source:a Dest:b}", result)
		}
	})

	// H8: non-comment lines preserve leading whitespace (M22 fix).
	t.Run("non-comment lines preserve whitespace", func(t *testing.T) {
		dir := t.TempDir()
		fp := filepath.Join(dir, "ws.json")
		content := "# header\n{\n  \"key\": \"value\"\n}\n"
		if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		var result struct {
			Key string `json:"key"`
		}
		if err := loadJSONFile(fp, &result); err != nil {
			t.Fatalf("loadJSONFile() error = %v", err)
		}
		if result.Key != "value" {
			t.Errorf("got key=%q, want \"value\"", result.Key)
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

// TestConfig_IgnoreAndMatch covers the Config methods that gate which
// columns, indexes, foreign keys, and tables get ALTER statements. M2.
func TestConfig_IgnoreAndMatch(t *testing.T) {
	cfg := &Config{
		AlterIgnore: map[string]*AlterIgnoreTable{
			"user*": {
				Column:     []string{"password", "secret_*"},
				Index:      []string{"idx_old_*"},
				ForeignKey: []string{"fk_legacy*"},
			},
		},
		Tables:       []string{"user_*", "order_*"},
		TablesIgnore: []string{"order_log*"},
	}

	t.Run("IsIgnoreField exact match", func(t *testing.T) {
		if !cfg.IsIgnoreField("users", "password") {
			t.Error("expected password to be ignored in users table")
		}
	})
	t.Run("IsIgnoreField wildcard table", func(t *testing.T) {
		if !cfg.IsIgnoreField("user_profile", "secret_key") {
			t.Error("expected secret_key to be ignored via wildcard")
		}
	})
	t.Run("IsIgnoreField non-match", func(t *testing.T) {
		if cfg.IsIgnoreField("users", "email") {
			t.Error("email should NOT be ignored")
		}
	})
	t.Run("IsIgnoreField non-match table", func(t *testing.T) {
		if cfg.IsIgnoreField("orders", "password") {
			t.Error("password in orders should NOT be ignored (no matching AlterIgnore entry)")
		}
	})

	t.Run("IsIgnoreIndex wildcard match", func(t *testing.T) {
		if !cfg.IsIgnoreIndex("users", "idx_old_name") {
			t.Error("expected idx_old_name to be ignored")
		}
	})
	t.Run("IsIgnoreIndex non-match", func(t *testing.T) {
		if cfg.IsIgnoreIndex("users", "idx_email") {
			t.Error("idx_email should NOT be ignored")
		}
	})

	t.Run("IsIgnoreForeignKey wildcard match", func(t *testing.T) {
		if !cfg.IsIgnoreForeignKey("users", "fk_legacy_order") {
			t.Error("expected fk_legacy_order to be ignored")
		}
	})
	t.Run("IsIgnoreForeignKey non-match", func(t *testing.T) {
		if cfg.IsIgnoreForeignKey("users", "fk_active") {
			t.Error("fk_active should NOT be ignored")
		}
	})

	t.Run("CheckMatchTables empty list matches all", func(t *testing.T) {
		emptyCfg := &Config{}
		if !emptyCfg.CheckMatchTables("anything") {
			t.Error("empty Tables should match all tables")
		}
	})
	t.Run("CheckMatchTables wildcard", func(t *testing.T) {
		if !cfg.CheckMatchTables("user_profile") {
			t.Error("user_profile should match user_*")
		}
	})
	t.Run("CheckMatchTables non-match", func(t *testing.T) {
		if cfg.CheckMatchTables("product_base") {
			t.Error("product_base should NOT match")
		}
	})

	t.Run("CheckMatchIgnoreTables empty returns false", func(t *testing.T) {
		emptyCfg := &Config{}
		if emptyCfg.CheckMatchIgnoreTables("anything") {
			t.Error("empty TablesIgnore should return false")
		}
	})
	t.Run("CheckMatchIgnoreTables wildcard match", func(t *testing.T) {
		if !cfg.CheckMatchIgnoreTables("order_log_2024") {
			t.Error("order_log_2024 should match order_log*")
		}
	})
	t.Run("CheckMatchIgnoreTables non-match", func(t *testing.T) {
		if cfg.CheckMatchIgnoreTables("order_items") {
			t.Error("order_items should NOT match order_log*")
		}
	})
}

func TestConfigNilAlterIgnoreEntryDoesNotPanic(t *testing.T) {
	cfg := &Config{AlterIgnore: map[string]*AlterIgnoreTable{"users": nil}}
	if cfg.IsIgnoreField("users", "password") {
		t.Fatal("nil ignore entry unexpectedly ignored field")
	}
	if cfg.IsIgnoreIndex("users", "idx_name") {
		t.Fatal("nil ignore entry unexpectedly ignored index")
	}
	if cfg.IsIgnoreForeignKey("users", "fk_order") {
		t.Fatal("nil ignore entry unexpectedly ignored foreign key")
	}
}

// TestConfig_String_MasksCredentials verifies Config.String() masks
// DSN passwords and email password. M3.
func TestConfig_String_MasksCredentials(t *testing.T) {
	cfg := &Config{
		SourceDSN: "admin:supersecret@tcp(src:3306)/production",
		DestDSN:   "root:anothersecret@tcp(dst:3306)/staging",
		Email: &EmailStruct{
			SMTPHost: "smtp.example.com:587",
			From:     "bot@example.com",
			Password: "mailpassword123",
			To:       "ops@example.com",
		},
	}
	s := cfg.String()

	for _, secret := range []string{"supersecret", "anothersecret", "mailpassword123"} {
		if strings.Contains(s, secret) {
			t.Errorf("Config.String() leaked credential %q in output: %s", secret, s)
		}
	}
	if !strings.Contains(s, "***") {
		t.Error("Config.String() should contain masked markers (***)")
	}
}

// TestExtractDSNPassword covers the core credential-extraction function
// directly with edge cases. L11.
func TestExtractDSNPassword(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{"normal DSN", "user:pass@tcp(host:3306)/db", "pass"},
		{"password with @", "user:p@ssword@tcp(host:3306)/db", "p@ssword"},
		{"no password", "user@tcp(host:3306)/db", ""},
		{"empty password", "user:@tcp(host:3306)/db", ""},
		{"empty DSN", "", ""},
		{"no colon in user part", "justuser@tcp(host)/db", ""},
		{"multiple @ in password", "user:a@b@c@tcp(host)/db", "a@b@c"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDSNPassword(tt.dsn)
			if got != tt.want {
				t.Errorf("extractDSNPassword(%q) = %q, want %q", tt.dsn, got, tt.want)
			}
		})
	}
}
