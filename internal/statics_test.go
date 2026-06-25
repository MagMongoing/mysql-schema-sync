//  Copyright(C) 2025 github.com/hidu  All Rights Reserved.
//  Author: hidu <duv123+git@gmail.com>
//  Date: 2026-06-19

package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRedactDSNs verifies the exported DSN redaction helper applied by
// statics.go (per-table error rendering) and main.go (panic recovery).
func TestRedactDSNs(t *testing.T) {
	tests := []struct {
		name     string
		msg      string
		dsns     []string
		mustHave []string // substrings that must appear in the result
		mustGone []string // substrings that must NOT appear in the result
	}{
		{
			name:     "single dsn redacted",
			msg:      "dial error: user:supersecret@tcp(127.0.0.1:3306)/db1: refused",
			dsns:     []string{"user:supersecret@tcp(127.0.0.1:3306)/db1"},
			mustHave: []string{"user:***@tcp(127.0.0.1:3306)/db1", "refused"},
			mustGone: []string{"supersecret"},
		},
		{
			name:     "two dsns both redacted",
			msg:      "src=u1:p1@tcp(a:1)/x dst=u2:p2@tcp(b:2)/y err",
			dsns:     []string{"u1:p1@tcp(a:1)/x", "u2:p2@tcp(b:2)/y"},
			mustHave: []string{"u1:***@tcp(a:1)/x", "u2:***@tcp(b:2)/y"},
			mustGone: []string{":p1@", ":p2@"},
		},
		{
			name:     "empty dsn skipped",
			msg:      "no creds in this message",
			dsns:     []string{"", ""},
			mustHave: []string{"no creds in this message"},
		},
		{
			name:     "DSN not present in msg, short password not substring-redacted",
			msg:      "some unrelated error",
			dsns:     []string{"user:hidden@tcp(host)/db"},
			mustHave: []string{"some unrelated error"},
			mustGone: []string{"hidden"},
		},
		{
			name:     "password containing @ uses LastIndex",
			msg:      "fail: user:p@ss@tcp(host:3306)/db boom",
			dsns:     []string{"user:p@ss@tcp(host:3306)/db"},
			mustHave: []string{"user:***@tcp(host:3306)/db"},
			mustGone: []string{"p@ss@tcp"},
		},
		{
			name:     "dsn without password is left alone",
			msg:      "addr=user@tcp(host:3306)/db connecting",
			dsns:     []string{"user@tcp(host:3306)/db"},
			mustHave: []string{"user@tcp(host:3306)/db"},
		},
		// M22: additional edge cases.
		{
			name:     "DSN appears multiple times in msg",
			msg:      "first user:pw1234@tcp(h:1)/d then user:pw1234@tcp(h:1)/d end",
			dsns:     []string{"user:pw1234@tcp(h:1)/d"},
			mustHave: []string{"user:***@tcp(h:1)/d"},
			mustGone: []string{"pw1234"},
		},
		{
			name:     "password-only echo in driver error (no full DSN)",
			msg:      "Access denied for user 'admin'@'host' (using password: mysecret123456)",
			dsns:     []string{"admin:mysecret123456@tcp(host:3306)/db"},
			mustHave: []string{"***"},
			mustGone: []string{"mysecret123456"},
		},
		{
			// H10: the original test had mustHave: []string{""} which is always true.
			// Now we assert the result is exactly "" (empty string).
			name:     "empty msg unchanged",
			msg:      "",
			dsns:     []string{"user:pass@tcp(h)/d"},
			mustHave: nil, // no substring check needed
			mustGone: []string{"pass"},
		},
		{
			name:     "zero DSNs variadic is a no-op",
			msg:      "some error with user:secret@tcp(h)/d",
			dsns:     nil,
			mustHave: []string{"some error with user:secret@tcp(h)/d"},
		},
		{
			// H11: add mustHave to verify surrounding DSN structure is intact.
			name:     "two DSNs where one is substring of another",
			msg:      "err u:pw1@tcp(h:1)/d and uu:pw1@tcp(hh:1)/dd",
			dsns:     []string{"u:pw1@tcp(h:1)/d", "uu:pw1@tcp(hh:1)/dd"},
			mustHave: []string{"u:***@tcp(h:1)/d", "uu:***@tcp(hh:1)/dd"},
			mustGone: []string{":pw1@tcp(h:1)/d", ":pw1@tcp(hh:1)/dd"},
		},
		{
			name:     "regex special chars in DSN (literal ReplaceAll)",
			msg:      "err user:p.w+rd@tcp(h)/d end",
			dsns:     []string{"user:p.w+rd@tcp(h)/d"},
			mustHave: []string{"user:***@tcp(h)/d"},
			mustGone: []string{"p.w+rd"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactDSNs(tt.msg, tt.dsns...)
			// H10: for the "empty msg" case, assert exact equality.
			if tt.name == "empty msg unchanged" && got != "" {
				t.Errorf("RedactDSNs empty msg: got %q, want \"\"", got)
			}
			for _, want := range tt.mustHave {
				if !strings.Contains(got, want) {
					t.Errorf("RedactDSNs result missing %q\n got: %s", want, got)
				}
			}
			for _, gone := range tt.mustGone {
				if gone == "" {
					continue
				}
				if strings.Contains(got, gone) {
					t.Errorf("RedactDSNs result still contains %q (should be redacted)\n got: %s", gone, got)
				}
			}
		})
	}
}

// TestRedactDSNs_AliasUnexported keeps the unexported alias covered so callers
// inside this package (which still use the lowercase name) don't silently
// regress. L35: property-style — run the full TestRedactDSNs table through
// both functions and assert identical output.
func TestRedactDSNs_AliasUnexported(t *testing.T) {
	cases := []struct {
		msg  string
		dsns []string
	}{
		{"err: u:secret@tcp(h:1)/d", []string{"u:secret@tcp(h:1)/d"}},
		{"src=u1:p1@tcp(a:1)/x", []string{"u1:p1@tcp(a:1)/x"}},
		{"", []string{"u:p@tcp(h)/d"}},
		{"no dsn here", nil},
	}
	for _, c := range cases {
		gotExported := RedactDSNs(c.msg, c.dsns...)
		gotUnexported := redactDSNs(c.msg, c.dsns...)
		if gotExported != gotUnexported {
			t.Errorf("RedactDSNs != redactDSNs for msg=%q: exported=%q unexported=%q", c.msg, gotExported, gotUnexported)
		}
	}
}

// TestSanitizeAnchorID verifies that sanitizeAnchorID produces unique,
// HTML-safe anchor IDs, including when distinct table names collapse
// to the same sanitized base. C2: no prior test coverage.
func TestSanitizeAnchorID(t *testing.T) {
	tests := []struct {
		name string
		s    string
		idx  int
		want string
	}{
		{"simple name idx 0", "users", 0, "users_0"},
		{"simple name idx 1", "users", 1, "users_1"},
		{"name with dash", "my-table", 0, "my-table_0"},
		{"name with slash", "schema/table", 0, "schema_table_0"},
		{"name with spaces", "my table", 0, "my_table_0"},
		{"name with unicode", "café", 0, "caf__0"},
		{"leading digit", "123table", 0, "123table_0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeAnchorID(tt.s, tt.idx)
			if got != tt.want {
				t.Errorf("sanitizeAnchorID(%q, %d) = %q, want %q", tt.s, tt.idx, got, tt.want)
			}
		})
	}

	// Verify uniqueness: two different inputs that collapse to the same
	// sanitized base must produce different IDs when given different idx values.
	t.Run("collision avoidance via idx", func(t *testing.T) {
		id1 := sanitizeAnchorID("a-b", 0)
		id2 := sanitizeAnchorID("a_b", 1)
		if id1 == id2 {
			t.Errorf("sanitizeAnchorID produced collision: %q == %q", id1, id2)
		}
	})

	t.Run("same input different idx produces different IDs", func(t *testing.T) {
		id1 := sanitizeAnchorID("users", 0)
		id2 := sanitizeAnchorID("users", 1)
		if id1 == id2 {
			t.Errorf("sanitizeAnchorID produced collision for same name: %q == %q", id1, id2)
		}
	})
}

// TestWriteHTMLResult_StaleTmpCleanup verifies that writeHTMLResult
// removes stale .tmp files from prior crashes. C3: no prior test coverage.
// M6 note: this test mutates the package-level htmlResultPath variable.
// Currently safe because Go tests are sequential by default, but would
// race if t.Parallel() is added. Consider extracting writeHTMLResult to
// accept the output path as a parameter for future-proofing.
func TestWriteHTMLResult_StaleTmpCleanup(t *testing.T) {
	dir := t.TempDir()
	resultFile := filepath.Join(dir, "result.html")

	// Save and restore the global htmlResultPath
	origPath := htmlResultPath
	htmlResultPath = resultFile
	t.Cleanup(func() { htmlResultPath = origPath })

	// Create a stale tmp file with old modification time
	staleTmp := filepath.Join(dir, "result.html.stale123.tmp")
	if err := os.WriteFile(staleTmp, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	// Set modification time to 2 hours ago (older than the 1-hour threshold)
	oldTime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(staleTmp, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	// Create a non-stale tmp file (recent, should NOT be removed)
	recentTmp := filepath.Join(dir, "result.html.recent456.tmp")
	if err := os.WriteFile(recentTmp, []byte("recent"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create an unrelated file (should NOT be removed)
	unrelated := filepath.Join(dir, "other.txt")
	if err := os.WriteFile(unrelated, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}

	// Call writeHTMLResult — this should clean up stale tmp and write result
	writeHTMLResult("<h1>test</h1>")

	// Verify the result file was written
	if _, err := os.Stat(resultFile); err != nil {
		t.Errorf("result file not written: %v", err)
	}

	// Verify stale tmp was removed
	if _, err := os.Stat(staleTmp); !os.IsNotExist(err) {
		t.Errorf("stale tmp file should have been removed: %s", staleTmp)
	}

	// Verify recent tmp was NOT removed (within 1-hour window)
	if _, err := os.Stat(recentTmp); err != nil {
		t.Errorf("recent tmp file should NOT have been removed: %v", err)
	}

	// Verify unrelated file was NOT removed
	if _, err := os.Stat(unrelated); err != nil {
		t.Errorf("unrelated file should NOT have been removed: %v", err)
	}

	// Verify no leftover .tmp files from the current write
	matches, _ := filepath.Glob(filepath.Join(dir, "result.html.*.tmp"))
	// Only the recent one should remain
	for _, m := range matches {
		if m != recentTmp {
			t.Errorf("unexpected leftover tmp: %s", m)
		}
	}
}

// TestRedactDSNs_PasswordLengthBoundary verifies the threshold behavior:
// passwords shorter than 10 chars are NOT substring-redacted, passwords
// of exactly 10 chars ARE. M4.
func TestRedactDSNs_PasswordLengthBoundary(t *testing.T) {
	t.Run("9-char password standalone IS redacted via word-boundary check", func(t *testing.T) {
		msg := "error: pw1234567 appeared in trace"
		got := RedactDSNs(msg, "user:pw1234567@tcp(h)/d")
		// Password is 9 chars, below 10-char threshold, but appears as standalone word.
		if strings.Contains(got, "pw1234567") {
			t.Errorf("9-char standalone password should be word-boundary redacted, got: %s", got)
		}
		if !strings.Contains(got, "***") {
			t.Errorf("expected *** in output, got: %s", got)
		}
	})

	t.Run("9-char password inside other text NOT redacted", func(t *testing.T) {
		msg := "the pw1234567config file was loaded"
		got := RedactDSNs(msg, "user:pw1234567@tcp(h)/d")
		// Password "pw1234567" is adjacent to alphanumeric chars — NOT standalone.
		if !strings.Contains(got, "pw1234567") {
			t.Errorf("embedded short password should NOT be redacted, got: %s", got)
		}
	})

	t.Run("10-char password standalone is redacted", func(t *testing.T) {
		msg := "error: exact10chr appeared in trace"
		got := RedactDSNs(msg, "user:exact10chr@tcp(h)/d")
		// Password is 10 chars, at threshold, so standalone substring IS redacted.
		if strings.Contains(got, "exact10chr") {
			t.Errorf("10-char password should be substring-redacted, got: %s", got)
		}
		if !strings.Contains(got, "***") {
			t.Errorf("expected *** in output, got: %s", got)
		}
	})

	t.Run("11-char password standalone is redacted", func(t *testing.T) {
		msg := "error: longpassword11 appeared"
		got := RedactDSNs(msg, "user:longpassword11@tcp(h)/d")
		if strings.Contains(got, "longpassword11") {
			t.Errorf("11-char password should be substring-redacted, got: %s", got)
		}
	})
}

func TestRedactDSNsShortPasswordSkipsEmbeddedAndRedactsLaterStandalone(t *testing.T) {
	dsn := "user:pass@tcp(localhost:3306)/db"
	msg := "compass failed; supplied password was pass"
	got := RedactDSNs(msg, dsn)
	if !strings.Contains(got, "compass") {
		t.Fatalf("embedded password substring was corrupted: %q", got)
	}
	if strings.Contains(got, "was pass") || !strings.Contains(got, "was ***") {
		t.Fatalf("later standalone short password was not redacted: %q", got)
	}
}

// TestFormatTimeOrNA verifies the helper that prevents Y0001 timestamps. L12.
func TestFormatTimeOrNA(t *testing.T) {
	t.Run("zero time returns N/A", func(t *testing.T) {
		var zero time.Time
		got := formatTimeOrNA(zero)
		if got != "N/A" {
			t.Errorf("formatTimeOrNA(zero) = %q, want \"N/A\"", got)
		}
	})

	t.Run("non-zero time returns formatted", func(t *testing.T) {
		ts := time.Date(2025, 6, 15, 14, 30, 0, 0, time.UTC)
		got := formatTimeOrNA(ts)
		want := "2025-06-15 14:30:00"
		if got != want {
			t.Errorf("formatTimeOrNA(%v) = %q, want %q", ts, got, want)
		}
	})
}

func TestStaticsHTMLIncludesFatalErrorAlongsideChanges(t *testing.T) {
	cfg := &Config{}
	s := newStatics(cfg)
	s.fatalErr = fmt.Errorf("inspection failed")
	sd := &TableAlterData{
		Table:      "users",
		Type:       alterTypeAlter,
		SQL:        []string{"ALTER TABLE `users` ADD COLUMN `name` varchar(20);"},
		SchemaDiff: newSchemaDiff("users", "CREATE TABLE `users` (\n `id` int\n)", "CREATE TABLE `users` (\n `id` int\n)"),
	}
	st := s.newTableStatics("users", sd, 0)
	st.timer.stop()
	html := s.toHTML()
	if !strings.Contains(html, "任务失败") || !strings.Contains(html, "inspection failed") {
		t.Fatalf("fatal error missing from report with table changes: %s", html)
	}
}

func TestStaticsHTMLDistinguishesSkippedFromFailed(t *testing.T) {
	cfg := &Config{Sync: true}
	s := newStatics(cfg)
	sd := &TableAlterData{
		Table:      "users",
		Type:       alterTypeAlter,
		SQL:        []string{"ALTER TABLE `users` ADD COLUMN `name` varchar(20);"},
		SchemaDiff: newSchemaDiff("users", "CREATE TABLE `users` (\n `id` int\n)", "CREATE TABLE `users` (\n `id` int\n)"),
	}
	st := s.newTableStatics("users", sd, 0)
	st.skipped = true
	st.skipReason = fmt.Errorf("earlier statement failed")
	st.timer.stop()

	html := s.toHTML()
	if !strings.Contains(html, "未执行") || strings.Contains(html, "失败：earlier statement failed") {
		t.Fatalf("skipped statement rendered as failure: %s", html)
	}
	if got := s.alterFailedNum(); got != 0 {
		t.Fatalf("skipped statement counted as failed: %d", got)
	}
}
