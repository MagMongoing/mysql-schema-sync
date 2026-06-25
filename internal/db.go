package internal

import (
	"database/sql"
	"fmt"
	"log"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql" // mysql driver
	"github.com/xanygo/anygo/cli/xcolor"
)

// FieldInfo represents detailed field information from INFORMATION_SCHEMA.COLUMNS
type FieldInfo struct {
	ColumnName             string  `json:"column_name"`
	OrdinalPosition        int     `json:"ordinal_position"`
	ColumnDefault          *string `json:"column_default"`
	IsNullAble             string  `json:"is_nullable"`
	DataType               string  `json:"data_type"`
	CharacterMaximumLength *int64  `json:"character_maximum_length"`
	NumericPrecision       *int64  `json:"numeric_precision"`
	NumericScale           *int64  `json:"numeric_scale"`
	CharsetName            *string `json:"character_set_name"`
	CollationName          *string `json:"collation_name"`
	ColumnType             string  `json:"column_type"`
	ColumnComment          string  `json:"column_comment"`
	Extra                  string  `json:"extra"`
	GenerationExpression   string  `json:"generation_expression"`
}

// stringTypesNeedQuoted lists data types that require quoted default values in SQL
var stringTypesNeedQuoted = []string{
	"char", "varchar", "binary", "varbinary",
	"tinyblob", "blob", "mediumblob", "longblob",
	"tinytext", "text", "mediumtext", "longtext",
	"enum", "set", "json",
}

// currentTimestampReg matches CURRENT_TIMESTAMP with optional fractional seconds precision
var currentTimestampReg = regexp.MustCompile(`(?i)^CURRENT_TIMESTAMP(\(\d+\))?$`)

// needsQuotedDefault returns true if the field type requires quoted default values
func (f *FieldInfo) needsQuotedDefault() bool {
	dataType := strings.ToLower(f.DataType)
	return slices.Contains(stringTypesNeedQuoted, dataType)
}

// isCharacterType returns true if the field has a character-based data type that supports charset/collation
func (f *FieldInfo) isCharacterType() bool {
	dataType := strings.ToLower(f.DataType)
	// Character types that support CHARACTER SET / COLLATE
	charTypes := []string{"char", "varchar", "tinytext", "text", "mediumtext", "longtext", "enum", "set"}
	return slices.Contains(charTypes, dataType)
}

// isExpressionDefault returns true if the default value is a MySQL 8.0.13+ expression.
// Expression defaults are wrapped in parentheses, e.g. (UUID()), (JSON_OBJECT(...)).
// Hex literal defaults (0x...) are also treated as expressions.
func isExpressionDefault(val string) bool {
	if len(val) >= 2 && val[0] == '(' && val[len(val)-1] == ')' {
		return true
	}
	if len(val) >= 2 && val[0] == '0' && (val[1] == 'x' || val[1] == 'X') {
		return true
	}
	return false
}

// String returns the full column definition as used in CREATE TABLE
func (f *FieldInfo) String() string {
	var parts []string

	// Column name and type
	parts = append(parts, fmt.Sprintf("%s %s", quoteIdentifier(f.ColumnName), f.ColumnType))

	// CHARACTER SET and COLLATE:
	// Not emitted here intentionally. MySQL's SHOW CREATE TABLE only includes
	// CHARACTER SET/COLLATE when they differ from the table default. Since String()
	// doesn't know the table default, emitting them would produce incorrect DDL.
	// The raw SHOW CREATE TABLE text (which already contains correct charset clauses)
	// is used directly for CHANGE/MODIFY statements wherever available.

	// Generated column expression (GENERATED ALWAYS AS ...)
	if f.GenerationExpression != "" {
		parts = append(parts, fmt.Sprintf("GENERATED ALWAYS AS (%s)", f.GenerationExpression))
	}

	// NULL/NOT NULL
	if strings.ToUpper(f.IsNullAble) == "NO" {
		parts = append(parts, "NOT NULL")
	} else {
		parts = append(parts, "NULL")
	}

	// Default value (MySQL 8.0.13+ allows DEFAULT on generated columns)
	if f.ColumnDefault != nil {
		defaultValue := *f.ColumnDefault
		upperDefault := strings.ToUpper(defaultValue)

		// Special keywords that don't need quotes
		if upperDefault == "NULL" {
			parts = append(parts, "DEFAULT NULL")
		} else if currentTimestampReg.MatchString(defaultValue) {
			parts = append(parts, fmt.Sprintf("DEFAULT %s", strings.ToUpper(defaultValue)))
		} else if isExpressionDefault(defaultValue) {
			// MySQL 8.0.13+ expression defaults (e.g., (UUID()), hex literals)
			// are emitted as-is without additional quoting
			parts = append(parts, fmt.Sprintf("DEFAULT %s", defaultValue))
		} else if f.needsQuotedDefault() {
			// String types need quotes; escape backslashes first, then single quotes
			escapedDefault := strings.ReplaceAll(defaultValue, "\\", "\\\\")
			escapedDefault = strings.ReplaceAll(escapedDefault, "'", "''")
			parts = append(parts, fmt.Sprintf("DEFAULT '%s'", escapedDefault))
		} else {
			// Numeric types don't need quotes
			parts = append(parts, fmt.Sprintf("DEFAULT %s", defaultValue))
		}
	}

	// Extra: filter out generation-related tokens handled separately below.
	// Uses token-based filtering to avoid accidentally stripping unrelated keywords.
	if f.Extra != "" {
		strippedTokens := []string{"DEFAULT_GENERATED", "VIRTUAL", "STORED", "GENERATED"}
		tokens := strings.Fields(f.Extra)
		var kept []string
		for _, tok := range tokens {
			keep := true
			for _, s := range strippedTokens {
				if strings.EqualFold(tok, s) {
					keep = false
					break
				}
			}
			if keep {
				kept = append(kept, tok)
			}
		}
		if len(kept) > 0 {
			parts = append(parts, strings.ToUpper(strings.Join(kept, " ")))
		}
	}

	// Virtual/Stored keyword for generated columns (after NULL/NOT NULL)
	if f.GenerationExpression != "" {
		if strings.Contains(strings.ToUpper(f.Extra), "STORED") {
			parts = append(parts, "STORED")
		} else {
			parts = append(parts, "VIRTUAL")
		}
	}

	// Comment
	if f.ColumnComment != "" {
		// Escape backslashes first, then single quotes by doubling them
		escapedComment := strings.ReplaceAll(f.ColumnComment, "\\", "\\\\")
		escapedComment = strings.ReplaceAll(escapedComment, "'", "''")
		parts = append(parts, fmt.Sprintf("COMMENT '%s'", escapedComment))
	}

	return strings.Join(parts, " ")
}

// Equals compares two FieldInfo instances for semantic equality
func (f *FieldInfo) Equals(other *FieldInfo) bool {
	if f == nil || other == nil {
		return f == other
	}

	// Compare basic properties
	if f.ColumnName != other.ColumnName ||
		f.IsNullAble != other.IsNullAble ||
		f.DataType != other.DataType ||
		f.ColumnComment != other.ColumnComment ||
		normalizeExtra(f.Extra) != normalizeExtra(other.Extra) {
		return false
	}

	// Compare ColumnType with normalization for integer display width
	// MySQL 8.0.19+ removed display width for integer types (int(11) -> int)
	normalizedSourceType := normalizeIntegerType(f.ColumnType)
	normalizedDestType := normalizeIntegerType(other.ColumnType)
	if normalizedSourceType != normalizedDestType {
		return false
	}

	// Compare default values
	if (f.ColumnDefault == nil && other.ColumnDefault != nil) ||
		(f.ColumnDefault != nil && other.ColumnDefault == nil) {
		return false
	}
	if f.ColumnDefault != nil && other.ColumnDefault != nil {
		if *f.ColumnDefault != *other.ColumnDefault {
			return false
		}
	}

	// Compare character set and collation (handle NULL values gracefully)
	// For charset and collation, we consider them equal if:
	// 1. Both are NULL, or
	// 2. One is NULL and the other uses the default/collation, or
	// 3. Both are set and equal
	if !f.charsetEquals(other) || !f.collationEquals(other) {
		return false
	}

	// Compare generation expression for generated columns
	if f.GenerationExpression != other.GenerationExpression {
		return false
	}

	return true
}

// normalizeExtra strips MySQL-version-specific markers from Extra for cross-version comparison
func normalizeExtra(extra string) string {
	extra = strings.ReplaceAll(extra, "DEFAULT_GENERATED", "")
	return strings.TrimSpace(extra)
}

// isTimestampDatetimeEquivalent checks if two fields only differ in timestamp vs datetime type.
// This is used when SkipTimestampToDatetime is enabled, to avoid overwriting datetime fields
// in the destination database with timestamp fields from the source database.
func isTimestampDatetimeEquivalent(source, dest *FieldInfo) bool {
	srcType := strings.ToLower(source.DataType)
	dstType := strings.ToLower(dest.DataType)

	// Only handle timestamp → datetime conversion
	if srcType != "timestamp" || dstType != "datetime" {
		return false
	}

	// Create a copy of dest with timestamp type to compare everything else
	destCopy := *dest
	destCopy.DataType = "timestamp"
	// Normalize to lowercase before replacing to handle any case variation
	lowerColType := strings.ToLower(destCopy.ColumnType)
	if len(lowerColType) < len("datetime") {
		return false
	}
	destCopy.ColumnType = "timestamp" + lowerColType[len("datetime"):]

	return source.Equals(&destCopy)
}

// isTextTimestampDatetimeSkip checks if the source field text uses timestamp
// and the dest field text uses datetime, with everything else being equivalent.
// Used in the legacy text-based comparison path.
func isTextTimestampDatetimeSkip(sourceText, destText string) bool {
	src := strings.TrimSpace(sourceText)
	dst := strings.TrimSpace(destText)

	srcLower := strings.ToLower(src)
	dstLower := strings.ToLower(dst)

	// Quick check: source must contain "timestamp" and dest must contain "datetime"
	if !strings.Contains(srcLower, "timestamp") || !strings.Contains(dstLower, "datetime") {
		return false
	}

	// Normalize: replace the type portion to compare the rest
	// Strip backtick-quoted column name prefix if present
	srcRest := stripFieldNamePrefix(src)
	dstRest := stripFieldNamePrefix(dst)

	// Replace type keyword for comparison
	srcNorm := replaceTimestampType(srcRest)
	dstNorm := replaceDatetimeType(dstRest)

	return srcNorm == dstNorm
}

// stripFieldNamePrefix removes the backtick-quoted column name from a field definition line.
// Handles doubled backticks correctly via extractQuotedIdentifier.
// e.g., "`created_at` timestamp NOT NULL" → "timestamp NOT NULL"
func stripFieldNamePrefix(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 0 && s[0] == '`' {
		// Find end of identifier (handles doubled backticks)
		i := 1
		for i < len(s) {
			if s[i] == '`' {
				if i+1 < len(s) && s[i+1] == '`' {
					i += 2 // skip doubled backtick
					continue
				}
				// Single closing backtick — identifier ends here
				return strings.TrimSpace(s[i+1:])
			}
			i++
		}
	}
	return s
}

// replaceTimestampType replaces the timestamp type keyword with a normalized placeholder
func replaceTimestampType(s string) string {
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "timestamp") {
		return "NORMALIZED_TYPE" + s[len("timestamp"):]
	}
	return s
}

// replaceDatetimeType replaces the datetime type keyword with a normalized placeholder
func replaceDatetimeType(s string) string {
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "datetime") {
		return "NORMALIZED_TYPE" + s[len("datetime"):]
	}
	return s
}

// charsetEquals checks if character sets are semantically equal.
// When one side is nil (inherits table default) and the other is explicit,
// we treat them as equal to avoid false-positive diffs across MySQL versions.
func (f *FieldInfo) charsetEquals(other *FieldInfo) bool {
	// Both NULL - equal
	if f.CharsetName == nil && other.CharsetName == nil {
		return true
	}
	// One NULL, one set - treat as equal (nil inherits table default, which we can't determine)
	if (f.CharsetName == nil) != (other.CharsetName == nil) {
		return true
	}
	// Both not NULL, compare values
	return *f.CharsetName == *other.CharsetName
}

// collationEquals checks if collations are semantically equal.
// When one side is nil (inherits table default) and the other is explicit,
// we treat them as equal to avoid false-positive diffs across MySQL versions.
func (f *FieldInfo) collationEquals(other *FieldInfo) bool {
	// Both NULL - equal
	if f.CollationName == nil && other.CollationName == nil {
		return true
	}
	// One NULL, one set - treat as equal (nil inherits table default, which we can't determine)
	if (f.CollationName == nil) != (other.CollationName == nil) {
		return true
	}
	// Both not NULL, compare values
	return *f.CollationName == *other.CollationName
}

type dbType string

const (
	dbTypeSource = "source"
	dbTypeDest   = "dest"
)

// MyDb db struct
type MyDb struct {
	sqlDB     *sql.DB
	dbType    dbType
	dbName    string // 数据库名称
	closeOnce sync.Once
}

// NewMyDb creates a new MyDb connection, verifies it with Ping, and returns the database name
func NewMyDb(dsn string, dbType dbType) (*MyDb, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("connect to db [%s] failed: %w", dsnShort(dsn), err)
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping db [%s] failed: %w", dsnShort(dsn), err)
	}
	dbName, err := getDatabaseName(db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("get database name failed: %w", err)
	}
	return &MyDb{
		sqlDB:  db,
		dbType: dbType,
		dbName: dbName,
	}, nil
}

// getDatabaseName extracts database name from the current database connection.
// Returns an error if no default schema is configured (DATABASE() is NULL),
// since the rest of the tool keys off db.dbName for INFORMATION_SCHEMA queries.
func getDatabaseName(db *sql.DB) (string, error) {
	var dbName sql.NullString
	const query = "SELECT DATABASE()"
	if err := db.QueryRow(query).Scan(&dbName); err != nil {
		log.Printf("QueryRow %q, Err=%v", query, err)
		return "", err
	}
	if !dbName.Valid || dbName.String == "" {
		return "", fmt.Errorf("DSN must specify a default database (got NULL from SELECT DATABASE())")
	}
	return dbName.String, nil
}

// GetTableNames returns all table names (excluding views) from the database
func (db *MyDb) GetTableNames() ([]string, error) {
	rs, err := db.Query("show table status")
	if err != nil {
		return nil, fmt.Errorf("show tables failed: %w", err)
	}
	defer rs.Close()

	var tables []string
	columns, err := rs.Columns()
	if err != nil {
		return nil, fmt.Errorf("show tables columns: %w", err)
	}
	for rs.Next() {
		var values = make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range columns {
			valuePtrs[i] = &values[i]
		}
		if err := rs.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("show tables scan failed: %w", err)
		}
		var valObj = make(map[string]any)
		for i, col := range columns {
			var v any
			val := values[i]
			b, ok := val.([]byte)
			if ok {
				v = string(b)
			} else {
				v = val
			}
			valObj[col] = v
		}
		// Filter out views and broken/recovery-state entries:
		// - views typically return Engine = NULL
		// - some MySQL versions return an empty string instead of NULL
		// Only treat rows with a non-empty Engine string as base tables.
		engineStr, _ := valObj["Engine"].(string)
		if engineStr != "" {
			name, ok := valObj["Name"].(string)
			if ok {
				tables = append(tables, name)
			}
		}
	}
	if err := rs.Err(); err != nil {
		return nil, fmt.Errorf("iterate table status: %w", err)
	}
	return tables, nil
}

// GetTableSchema returns the CREATE TABLE statement for the given table
func (db *MyDb) GetTableSchema(name string) (string, error) {
	rs, err := db.Query(fmt.Sprintf("show create table %s", quoteIdentifier(name)))
	if err != nil {
		return "", fmt.Errorf("show create table %q failed: %w", name, err)
	}
	defer rs.Close()
	var schema string
	for rs.Next() {
		var vname string
		if err := rs.Scan(&vname, &schema); err != nil {
			return "", fmt.Errorf("get table %q schema scan failed: %w", name, err)
		}
	}
	if err := rs.Err(); err != nil {
		return "", fmt.Errorf("iterate table %q schema: %w", name, err)
	}
	if schema == "" {
		return "", fmt.Errorf("show create table %q returned no data", name)
	}
	return schema, nil
}

// TableFieldsFromInformationSchema retrieves detailed field information from INFORMATION_SCHEMA.COLUMNS
func (db *MyDb) TableFieldsFromInformationSchema(tableName string) (map[string]*FieldInfo, error) {
	const query = `
		SELECT
			COLUMN_NAME,
			ORDINAL_POSITION,
			COLUMN_DEFAULT,
			IS_NULLABLE,
			DATA_TYPE,
			CHARACTER_MAXIMUM_LENGTH,
			NUMERIC_PRECISION,
			NUMERIC_SCALE,
			CHARACTER_SET_NAME,
			COLLATION_NAME,
			COLUMN_TYPE,
			COLUMN_COMMENT,
			EXTRA,
			IFNULL(GENERATION_EXPRESSION, '')
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION`

	rows, err := db.Query(query, db.dbName, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to query INFORMATION_SCHEMA.COLUMNS for table %q: %w", tableName, err)
	}
	defer rows.Close()

	fields := make(map[string]*FieldInfo)

	for rows.Next() {
		field := &FieldInfo{}
		var charMaxLen, numericPrecision, numericScale sql.NullInt64
		var charset, collation, columnDefault sql.NullString

		err := rows.Scan(
			&field.ColumnName,
			&field.OrdinalPosition,
			&columnDefault,
			&field.IsNullAble,
			&field.DataType,
			&charMaxLen,
			&numericPrecision,
			&numericScale,
			&charset,
			&collation,
			&field.ColumnType,
			&field.ColumnComment,
			&field.Extra,
			&field.GenerationExpression,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan field information for table %q: %w", tableName, err)
		}

		// Handle nullable fields
		if columnDefault.Valid {
			field.ColumnDefault = &columnDefault.String
		}
		if charMaxLen.Valid {
			val := charMaxLen.Int64
			field.CharacterMaximumLength = &val
		}
		if numericPrecision.Valid {
			val := numericPrecision.Int64
			field.NumericPrecision = &val
		}
		if numericScale.Valid {
			val := numericScale.Int64
			field.NumericScale = &val
		}
		if charset.Valid {
			field.CharsetName = &charset.String
		}
		if collation.Valid {
			field.CollationName = &collation.String
		}

		fields[field.ColumnName] = field
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating field information for table %q: %w", tableName, err)
	}

	if len(fields) == 0 {
		return nil, fmt.Errorf("no fields found for table %q in database %q", tableName, db.dbName)
	}

	return fields, nil
}

// Query execute sql query
func (db *MyDb) Query(query string, args ...any) (rows *sql.Rows, err error) {
	if debugEnabled {
		txt := fmt.Sprintf("[%-6s: %s] [Query] Start SQL=%s Args=%s\n",
			db.dbType,
			db.dbName,
			xcolor.GreenString("%s", strings.TrimSpace(query)),
			xcolor.GreenString("%v", args),
		)
		log.Output(2, txt)
	}
	start := time.Now()
	defer func() {
		if debugEnabled {
			cost := time.Since(start)
			txt := fmt.Sprintf("[%-6s: %s] [Query] End   Cost=%s Err=%s\n", db.dbType, db.dbName, cost.String(), errString(err))
			log.Output(3, txt)
		}
	}()
	return db.sqlDB.Query(query, args...)
}

// Exec executes a SQL statement with debug logging and timing
func (db *MyDb) Exec(query string, args ...any) (result sql.Result, err error) {
	if debugEnabled {
		txt := fmt.Sprintf("[%-6s: %s] [Exec]  Start SQL=%s Args=%s\n",
			db.dbType,
			db.dbName,
			xcolor.GreenString("%s", strings.TrimSpace(query)),
			xcolor.GreenString("%v", args),
		)
		log.Output(2, txt)
	}
	start := time.Now()
	defer func() {
		if debugEnabled {
			cost := time.Since(start)
			txt := fmt.Sprintf("[%-6s: %s] [Exec]  End   Cost=%s Err=%s\n", db.dbType, db.dbName, cost.String(), errString(err))
			log.Output(3, txt)
		}
	}()
	return db.sqlDB.Exec(query, args...)
}

// Close closes the database connection pool. L5: uses sync.Once for
// idempotency across concurrent close calls.
func (db *MyDb) Close() error {
	var closeErr error
	db.closeOnce.Do(func() {
		if db.sqlDB != nil {
			closeErr = db.sqlDB.Close()
		}
	})
	return closeErr
}
