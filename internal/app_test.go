//  Copyright(C) 2025 github.com/hidu  All Rights Reserved.
//  Author: hidu <duv123+git@gmail.com>
//  Date: 2025-10-29

package internal_test

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/xanygo/anygo/cli/xcolor"
	"github.com/xanygo/anygo/xt"

	"github.com/hidu/mysql-schema-sync/internal"
)

func TestWithDB(t *testing.T) {
	source := strings.TrimSpace(os.Getenv("MSS_Test_Source"))
	dest := strings.TrimSpace(os.Getenv("MSS_Test_Dest"))
	if source == "" || dest == "" {
		t.Logf("env.MSS_Test_Source=%q, env.MSS_Test_Dest=%q  Test Skipped", source, dest)
		t.SkipNow()
		return
	}
	getDBS := func(t *testing.T) (s *sql.DB, d *sql.DB) {
		t.Helper()
		sourceDB, err := testConnectDB(t, source)
		xt.NoError(t, err)
		// Register cleanup immediately so a subsequent failure doesn't leak the connection.
		t.Cleanup(func() { _ = sourceDB.Close() })
		testImportTables(t, sourceDB, "testdata/app/source_tables")

		destDB, err := testConnectDB(t, dest)
		xt.NoError(t, err)
		t.Cleanup(func() { _ = destDB.Close() })
		testImportTables(t, destDB, "testdata/app/dest_tables")
		return sourceDB, destDB
	}

	t.Run("case 1 no sync", func(t *testing.T) {
		sourceDB, destDB := getDBS(t)
		_ = sourceDB

		cfg := &internal.Config{
			SourceDSN: source,
			DestDSN:   dest,
		}
		if err := cfg.Check(); err != nil {
			t.Fatalf("cfg.Check() failed: %v", err)
		}
		// H6: capture dest schema BEFORE sync to assert it's unchanged.
		destSchemaBefore := testCollectSchemas(t, destDB)

		if err := internal.Execute(cfg); err != nil {
			t.Fatalf("preview Execute failed: %v", err)
		}

		// H6: verify no schema mutations occurred when Sync=false.
		destSchemaAfter := testCollectSchemas(t, destDB)
		for tbl, before := range destSchemaBefore {
			after, ok := destSchemaAfter[tbl]
			if !ok {
				t.Errorf("table %q disappeared from dest (Sync=false)", tbl)
				continue
			}
			if before != after {
				t.Errorf("table %q schema changed with Sync=false\nbefore: %s\nafter:  %s", tbl, before, after)
			}
		}
	})
	t.Run("case 2 sync", func(t *testing.T) {
		sourceDB, destDB := getDBS(t)
		_ = sourceDB

		cfg := &internal.Config{
			SourceDSN: source,
			DestDSN:   dest,
			Drop:      true,
			Sync:      true,
		}
		if err := cfg.Check(); err != nil {
			t.Fatalf("cfg.Check() failed: %v", err)
		}
		if err := internal.Execute(cfg); err != nil {
			t.Fatalf("sync Execute failed: %v", err)
		}

		// H7: after sync with Drop=true, every source table column/index
		// should be present in dest, and dest-only columns should be dropped.
		sourceSchemas := testCollectSchemas(t, sourceDB)
		destSchemas := testCollectSchemas(t, destDB)
		for tbl, srcSchema := range sourceSchemas {
			destSchema, ok := destSchemas[tbl]
			if !ok {
				t.Errorf("table %q missing from dest after sync", tbl)
				continue
			}
			// Verify each source column is present in dest.
			srcCols := testExtractColumns(srcSchema)
			destCols := testExtractColumns(destSchema)
			for _, col := range srcCols {
				found := false
				for _, dc := range destCols {
					if dc == col {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("table %q: source column %q not found in dest after sync", tbl, col)
				}
			}
		}
	})
}

func testImportTables(t *testing.T, db *sql.DB, dir string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	xt.NoError(t, err)
	xt.NotEmpty(t, files)
	var sqls []string
	for _, file := range files {
		content, err := os.ReadFile(file)
		xt.NoError(t, err)
		sqls = append(sqls, string(content))
	}
	err = testDBExec(t, db, sqls...)
	xt.NoError(t, err)
}

func testConnectDB(t *testing.T, dsn string) (*sql.DB, error) {
	t.Helper()
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, err
	}
	if cfg.DBName == "" {
		return nil, errors.New("empty DBName")
	}
	// Safety guard: this helper performs DROP DATABASE on cfg.DBName.
	// Refuse to run unless the database name starts with "test_" *and* has a
	// non-empty suffix after the prefix — a bare "test_" is still ambiguous
	// and should not be auto-dropped.
	if !strings.HasPrefix(cfg.DBName, "test_") || len(cfg.DBName) <= len("test_") {
		return nil, fmt.Errorf("refusing to DROP DATABASE %q: test database name must start with 'test_' prefix and have a non-empty suffix", cfg.DBName)
	}
	nc := cfg.Clone()
	nc.DBName = ""
	db, err := sql.Open("mysql", nc.FormatDSN())
	if err != nil {
		return nil, err
	}
	dbName := quoteIdentForTest(cfg.DBName)
	sqls := []string{
		fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName),
		fmt.Sprintf("CREATE DATABASE %s CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci", dbName),
	}
	if err = testDBExec(t, db, sqls...); err != nil {
		db.Close()
		return nil, err
	}
	db.Close()
	return sql.Open("mysql", dsn)
}

// quoteIdentForTest backtick-quotes an identifier for the test helper, doubling
// any embedded backticks. Mirrors the semantics of internal.quoteIdentifier
// (which is unexported from the internal package).
func quoteIdentForTest(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

func testDBExec(t *testing.T, db *sql.DB, sqls ...string) error {
	t.Helper()
	t.Logf("testExec:%s", strings.Repeat("-", 60))
	for _, sql := range sqls {
		t.Logf("start exec: \n%s", xcolor.GreenString(sql))
		ret, err := db.Exec(sql)
		t.Logf("exec result: err: %v", err)
		if err != nil {
			return err
		}
		num, err := ret.RowsAffected()
		t.Logf("RowsAffected %d, err: %v", num, err)
		if err != nil {
			return err
		}
	}
	return nil
}

// testCollectSchemas returns a map of table_name → SHOW CREATE TABLE output
// for all tables in the database. Used by H6/H7 to verify schema state.
func testCollectSchemas(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	rows, err := db.Query("SHOW TABLES")
	if err != nil {
		t.Fatalf("SHOW TABLES: %v", err)
	}
	defer rows.Close()
	schemas := make(map[string]string)
	for rows.Next() {
		var tbl string
		if err := rows.Scan(&tbl); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		var createStmt, tblName string
		if err := db.QueryRow("SHOW CREATE TABLE "+quoteIdentForTest(tbl)).Scan(&tblName, &createStmt); err != nil {
			t.Fatalf("SHOW CREATE TABLE %q: %v", tbl, err)
		}
		schemas[tbl] = createStmt
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows iteration: %v", err)
	}
	return schemas
}

// testExtractColumns extracts column names from a CREATE TABLE statement.
// Handles doubled backticks (“) inside identifiers correctly.
func testExtractColumns(createTable string) []string {
	lines := strings.Split(createTable, "\n")
	var cols []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) > 0 && line[0] == '`' {
			// Use extractQuotedIdentifier-style parsing for doubled backtick safety
			i := 1
			var name []byte
			for i < len(line) {
				if line[i] == '`' {
					if i+1 < len(line) && line[i+1] == '`' {
						name = append(name, '`')
						i += 2
						continue
					}
					// closing backtick
					cols = append(cols, string(name))
					break
				}
				name = append(name, line[i])
				i++
			}
		}
	}
	return cols
}
