package internal

import (
	"bytes"
	"encoding/json"
	"html"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/xanygo/anygo/cli/xcolor"
)

// Version 版本号，格式：更新日期(8位).更新次数(累加)
const Version = "20251021.4"

// AppURL site
const AppURL = "https://github.com/hidu/mysql-schema-sync/"

const timeFormatStd string = "2006-01-02 15:04:05"

// loadJSONFile loads a JSON config file, stripping lines that start with # or //.
// Note: Comment stripping is line-based, so a multi-line string value whose continuation
// line starts with # or // would be incorrectly removed. Standard JSON config files are
// not affected since string values do not span multiple lines.
func loadJSONFile(jsonPath string, val any) error {
	bs, err := os.ReadFile(jsonPath)
	if err != nil {
		return err
	}
	lines := strings.Split(string(bs), "\n")
	var bf bytes.Buffer
	for _, line := range lines {
		lineNew := strings.TrimSpace(line)
		if (len(lineNew) > 0 && lineNew[0] == '#') || (len(lineNew) > 1 && lineNew[0:2] == "//") {
			continue
		}
		bf.WriteString(lineNew)
	}
	return json.Unmarshal(bf.Bytes(), &val)
}

// simpleMatchCache caches compiled regex patterns for simpleMatch
var simpleMatchCache sync.Map

func simpleMatch(patternStr string, str string, msg ...string) bool {
	str = strings.TrimSpace(str)
	patternStr = strings.TrimSpace(patternStr)
	if patternStr == str {
		debugf("simple_match:suc,equal %v patternStr:%s str:%s", msg, patternStr, str)
		return true
	}

	// Build pattern string and use cached compiled regex
	parts := strings.Split(patternStr, "*")
	for i, part := range parts {
		parts[i] = regexp.QuoteMeta(part)
	}
	pattern := "^" + strings.Join(parts, `.*`) + "$"

	var re *regexp.Regexp
	if cached, ok := simpleMatchCache.Load(pattern); ok {
		re = cached.(*regexp.Regexp)
	} else {
		var err error
		re, err = regexp.Compile(pattern)
		if err != nil {
			log.Println("simple_match:error", msg, "patternStr:", patternStr, "pattern:", pattern, "str:", str, "err:", err)
			return false
		}
		simpleMatchCache.Store(pattern, re)
	}
	return re.MatchString(str)
}

func htmlPre(str string) string {
	return "<pre>" + html.EscapeString(str) + "</pre>"
}

// quoteIdentifier wraps name in backticks, doubling any embedded backticks.
// e.g., "col`name" → "`col``name`"
func quoteIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

func dsnShort(dsn string) string {
	// Use LastIndex: go-sql-driver parses from the last '@' as the credential separator,
	// allowing passwords to contain '@' characters.
	i := strings.LastIndex(dsn, "@")
	if i < 1 {
		if len(dsn) == 0 {
			return dsn
		}
		// No '@' found — DSN is malformed; return a safe placeholder
		// to avoid leaking credentials in logs or emails.
		return "<invalid DSN>"
	}
	return dsn[i+1:]
}

func errString(err error) string {
	if err == nil {
		return xcolor.YellowString("<nil>")
	}
	return xcolor.RedString("%s", err.Error())
}

// normalizeIntegerTypeReg matches integer types with display width for normalization
var normalizeIntegerTypeReg = regexp.MustCompile(`(?i)^(tinyint|smallint|mediumint|int|bigint)\(\d+\)(\s+.+)?$`)

// normalizeIntegerType removes display width from integer types for MySQL 8.0.19+ compatibility.
// MySQL 8.0.19+ deprecated display width for integer types (TINYINT, SMALLINT, MEDIUMINT, INT, BIGINT).
// This function normalizes types like "int(11)" to "int" while preserving modifiers like "unsigned" and "zerofill".
// Note: The function preserves the original case of the input (e.g., "INT(11)" → "INT").
// In practice, MySQL INFORMATION_SCHEMA always returns lowercase type names, so this is not an issue.
//
// Examples:
//   - "int(11)" -> "int"
//   - "int(11) unsigned" -> "int unsigned"
//   - "bigint(20)" -> "bigint"
//   - "tinyint(1)" -> "tinyint"
//   - "varchar(255)" -> "varchar(255)" (unchanged, not an integer type)
func normalizeIntegerType(columnType string) string {
	// Pattern matches: (tinyint|smallint|mediumint|int|bigint) followed by optional (digits)
	// Captures the type name and everything after the display width
	matches := normalizeIntegerTypeReg.FindStringSubmatch(columnType)
	if len(matches) > 0 {
		// matches[1] is the type name (e.g., "int")
		// matches[2] is the modifiers (e.g., " unsigned", " zerofill"), may be empty
		if len(matches) > 2 && matches[2] != "" {
			return matches[1] + matches[2] // e.g., "int unsigned"
		}
		return matches[1] // e.g., "int"
	}

	// Not an integer type with display width, return as-is
	return columnType
}
