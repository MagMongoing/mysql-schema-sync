package internal

import (
	"fmt"
	"log"
	"slices"
	"sort"
	"strings"

	"github.com/xanygo/anygo/cli/xcolor"
)

// debugEnabled controls whether [Debug] log messages are emitted.
// Off by default; enable via the -debug CLI flag.
var debugEnabled bool

// SetDebug enables or disables verbose debug logging.
func SetDebug(on bool) {
	debugEnabled = on
}

// debugf emits a [Debug] log line only when debug mode is enabled.
// Uses log.Output(2, ...) to preserve the caller's file:line information.
func debugf(format string, v ...any) {
	if debugEnabled {
		log.Output(2, fmt.Sprintf("[Debug] "+format, v...))
	}
}

// SchemaSync 配置文件
type SchemaSync struct {
	Config   *Config
	SourceDb *MyDb
	DestDb   *MyDb
}

// NewSchemaSync creates a SchemaSync with connections to both source and dest databases
func NewSchemaSync(config *Config) (*SchemaSync, error) {
	s := new(SchemaSync)
	s.Config = config
	var err error
	s.SourceDb, err = NewMyDb(config.SourceDSN, dbTypeSource)
	if err != nil {
		return nil, fmt.Errorf("source db: %w", err)
	}
	s.DestDb, err = NewMyDb(config.DestDSN, dbTypeDest)
	if err != nil {
		s.SourceDb.Close()
		return nil, fmt.Errorf("dest db: %w", err)
	}
	return s, nil
}

// AllDBTables returns the union of table names from source and dest databases
func (sc *SchemaSync) AllDBTables() ([]string, error) {
	sourceTables, err := sc.SourceDb.GetTableNames()
	if err != nil {
		return nil, err
	}
	destTables, err := sc.DestDb.GetTableNames()
	if err != nil {
		return nil, err
	}
	tables := slices.Clone(destTables)
	for _, name := range sourceTables {
		if !slices.Contains(tables, name) {
			tables = append(tables, name)
		}
	}
	return tables, nil
}

// RemoveTableSchemaConfig 删除表创建引擎信息，编码信息，分区信息，已修复同步表结构遇到分区表异常退出问题，
// 对于分区表，只会同步字段，索引，主键，外键的变更
// Uses ") ENGINE" to avoid truncating column comments that happen to contain "ENGINE"
func RemoveTableSchemaConfig(schema string) string {
	idx := strings.LastIndex(schema, ") ENGINE")
	if idx >= 0 {
		return schema[:idx+1] // keep the closing ")"
	}
	// Fallback: no ") ENGINE" found, return as-is (e.g. partitioned tables or test data)
	return schema
}

func (sc *SchemaSync) getAlterDataByTable(table string, cfg *Config) (*TableAlterData, error) {
	sSchema, err := sc.SourceDb.GetTableSchema(table)
	if err != nil {
		return nil, fmt.Errorf("get source schema for %q: %w", table, err)
	}
	dSchema, err := sc.DestDb.GetTableSchema(table)
	if err != nil {
		return nil, fmt.Errorf("get dest schema for %q: %w", table, err)
	}
	return sc.getAlterDataBySchema(table, sSchema, dSchema, cfg), nil
}

func (sc *SchemaSync) getAlterDataBySchema(table string, sSchema string, dSchema string, cfg *Config) *TableAlterData {
	alter := new(TableAlterData)
	alter.Table = table
	alter.Type = alterTypeNo

	// Early exit: if schemas are identical, no changes needed
	if sSchema == dSchema {
		alter.SchemaDiff = newSchemaDiff(table, RemoveTableSchemaConfig(sSchema), RemoveTableSchemaConfig(dSchema))
		return alter
	}

	// Try to get structured field information from INFORMATION_SCHEMA.COLUMNS
	// Only if we have database connections (not in unit tests)
	var sourceFields, destFields map[string]*FieldInfo
	var sourceFieldsErr, destFieldsErr error

	if sc.SourceDb != nil && sc.DestDb != nil {
		sourceFields, sourceFieldsErr = sc.SourceDb.TableFieldsFromInformationSchema(table)
		destFields, destFieldsErr = sc.DestDb.TableFieldsFromInformationSchema(table)
	}

	// If we can get structured field information from both databases, use it for precise comparison
	if sourceFieldsErr == nil && destFieldsErr == nil && sourceFields != nil && destFields != nil {
		debugf("Using structured field comparison for table %q", table)
		alter.SchemaDiff = NewSchemaDiffWithFieldInfos(table, RemoveTableSchemaConfig(sSchema), RemoveTableSchemaConfig(dSchema), sourceFields, destFields)
	} else {
		// Fallback to legacy text-based comparison
		if sourceFieldsErr != nil {
			debugf("Failed to get source fields for table %q: %s", table, errString(sourceFieldsErr))
		}
		if destFieldsErr != nil {
			debugf("Failed to get dest fields for table %q: %s", table, errString(destFieldsErr))
		}
		debugf("Using legacy text-based comparison for table %q", table)
		alter.SchemaDiff = newSchemaDiff(table, RemoveTableSchemaConfig(sSchema), RemoveTableSchemaConfig(dSchema))
	}

	if len(sSchema) == 0 {
		// Note: alterTypeDropTable is intentionally NOT executed in execute.go.
		// The tool does not auto-drop tables that only exist in dest for safety.
		// We still mark the type so callers can detect this case if needed.
		alter.Type = alterTypeDropTable
		alter.Comment = "源数据库不存在，目标数据库多余的表（不会自动删除）"
		return alter
	}
	if len(dSchema) == 0 {
		alter.Type = alterTypeCreate
		alter.Comment = "目标数据库不存在，创建"
		alter.SQL = append(alter.SQL, fmtTableCreateSQL(sSchema)+";")
		return alter
	}

	diffLines := sc.getSchemaDiff(alter)
	if len(diffLines) == 0 {
		return alter
	}
	alter.Type = alterTypeAlter
	if cfg.SingleSchemaChange {
		for _, line := range diffLines {
			ns := fmt.Sprintf("ALTER TABLE %s\n%s;", quoteIdentifier(table), line)
			alter.SQL = append(alter.SQL, ns)
		}
	} else {
		ns := fmt.Sprintf("ALTER TABLE %s\n%s;", quoteIdentifier(table), strings.Join(diffLines, ",\n"))
		alter.SQL = append(alter.SQL, ns)
	}

	return alter
}

func (sc *SchemaSync) getSchemaDiff(alter *TableAlterData) []string {
	sourceMyS := alter.SchemaDiff.Source
	destMyS := alter.SchemaDiff.Dest
	table := alter.Table
	var beforeFieldName string
	var alterLines []string
	var fieldCount int = 0

	// 比对字段 - Two-phase comparison strategy:
	// Phase 1: Compare text from SHOW CREATE TABLE first
	// Phase 2: Only if text differs, use INFORMATION_SCHEMA for detailed comparison
	useStructuredComparison := len(sourceMyS.FieldInfos) > 0 && len(destMyS.FieldInfos) > 0

	if useStructuredComparison {
		debugf("Using two-phase field comparison for table %s", table)
		// Use two-phase comparison
		for fieldName, value := range sourceMyS.Fields.Iter() {
			if sc.Config.IsIgnoreField(table, fieldName) {
				log.Printf("ignore column %s.%s", table, fieldName)
				// Still update position tracking if the field exists in dest,
				// so subsequent ADD statements position correctly
				if _, has := destMyS.Fields.Get(fieldName); has {
					beforeFieldName = fieldName
				}
				fieldCount++
				continue
			}
			var alterSQL string

			if destValue, has := destMyS.Fields.Get(fieldName); has {
				// Field exists in destination
				sourceFieldInfo := sourceMyS.FieldInfos[fieldName]
				destFieldInfo := destMyS.FieldInfos[fieldName]

				// Phase 1: Compare text from SHOW CREATE TABLE directly
				if value == destValue {
					// Text definitions are identical
					// Check field order if FieldOrder flag is enabled
					if sc.Config.FieldOrder && sourceFieldInfo != nil && destFieldInfo != nil {
						if sourceFieldInfo.OrdinalPosition != destFieldInfo.OrdinalPosition {
							// Field order differs — use raw text (preserves charset/collation/generation clauses)
							alterSQL = "MODIFY COLUMN " + value
							if len(beforeFieldName) > 0 {
								alterSQL += fmt.Sprintf(" AFTER %s", quoteIdentifier(beforeFieldName))
							} else {
								alterSQL += " FIRST"
							}
							debugf("field %s.%s: order differs (source pos=%d, dest pos=%d), generating MODIFY",
								table, fieldName, sourceFieldInfo.OrdinalPosition, destFieldInfo.OrdinalPosition)
						} else {
							debugf("check column.alter %s.%s not change (text identical)", table, fieldName)
						}
					} else {
						debugf("check column.alter %s.%s not change (text identical)", table, fieldName)
					}
					// Only update position tracking if no alterSQL generated (field is truly unchanged)
					if len(alterSQL) == 0 {
						beforeFieldName = fieldName
						fieldCount++
						continue
					}
				} else {
					// Phase 2: Text differs, use structured comparison to determine if change is needed
					if sourceFieldInfo != nil && destFieldInfo != nil {
						if sourceFieldInfo.Equals(destFieldInfo) {
							// Structured info shows they're semantically equal despite text difference
							// Still check field order if FieldOrder flag is enabled
							if sc.Config.FieldOrder && sourceFieldInfo.OrdinalPosition != destFieldInfo.OrdinalPosition {
								// Use source raw text (preserves charset/collation/generation clauses)
								alterSQL = "MODIFY COLUMN " + value
								if len(beforeFieldName) > 0 {
									alterSQL += fmt.Sprintf(" AFTER %s", quoteIdentifier(beforeFieldName))
								} else {
									alterSQL += " FIRST"
								}
								debugf("field %s.%s: semantically equal but order differs, generating MODIFY", table, fieldName)
							} else {
								debugf("field %s.%s: text differs but semantically equal, skipping", table, fieldName)
								debugf("source text: %s", value)
								debugf("dest text: %s", destValue)
								beforeFieldName = fieldName
								fieldCount++
								continue
							}
						} else {
							// Fields are genuinely different
							// Check if we should skip timestamp → datetime conversion
							if sc.Config.SkipTimestampToDatetime && isTimestampDatetimeEquivalent(sourceFieldInfo, destFieldInfo) {
								debugf("field %s.%s: timestamp vs datetime equivalent, skipping (SkipTimestampToDatetime enabled)", table, fieldName)
								beforeFieldName = fieldName
								fieldCount++
								continue
							}
							// Use source raw text for CHANGE (preserves charset/collation/generation clauses)
							alterSQL = fmt.Sprintf("CHANGE %s %s", quoteIdentifier(fieldName), value)
							debugf("field %s.%s: confirmed difference via structured comparison", table, fieldName)
							debugf("source: %+v", sourceFieldInfo)
							debugf("dest: %+v", destFieldInfo)
						}
					} else {
						// No structured info, use text-based CHANGE
						alterSQL = fmt.Sprintf("CHANGE %s %s", quoteIdentifier(fieldName), value)
						debugf("field %s.%s: text differs, using text-based change", table, fieldName)
					}
				}
				// Always update position tracking to reflect source table order
				beforeFieldName = fieldName
			} else {
				// Field doesn't exist in destination, ADD it
				if len(beforeFieldName) == 0 {
					if fieldCount == 0 {
						alterSQL = "ADD " + value + " FIRST"
					} else {
						alterSQL = "ADD " + value
					}
				} else {
					alterSQL = fmt.Sprintf("ADD %s AFTER %s", value, quoteIdentifier(beforeFieldName))
				}
				beforeFieldName = fieldName
			}

			if len(alterSQL) != 0 {
				debugf("check column.alter %s.%s alterSQL=%s", table, fieldName, alterSQL)
				alterLines = append(alterLines, alterSQL)
			} else {
				debugf("check column.alter %s.%s not change", table, fieldName)
			}
			fieldCount++
		}
	} else {
		// Legacy text-based comparison fallback.
		// In production, INFORMATION_SCHEMA is always available, so this path
		// is only triggered in unit tests or when the INFORMATION_SCHEMA query fails.
		// Note: This path uses raw SHOW CREATE TABLE text for SQL generation rather
		// than FieldInfo.String(). Full unification would require parsing FieldInfo
		// from raw text, which is not cost-effective given this path's limited use.
		debugf("Using legacy text-based field comparison for table %s", table)
		// Use legacy text-based comparison
		for fieldName, value := range sourceMyS.Fields.Iter() {
			if sc.Config.IsIgnoreField(table, fieldName) {
				log.Printf("ignore column %s.%s", table, fieldName)
				// Still update position tracking if the field exists in dest
				if _, has := destMyS.Fields.Get(fieldName); has {
					beforeFieldName = fieldName
				}
				fieldCount++
				continue
			}
			var alterSQL string
			if destDt, has := destMyS.Fields.Get(fieldName); has {
				if value != destDt {
					// Check if we should skip timestamp → datetime conversion
					if sc.Config.SkipTimestampToDatetime && isTextTimestampDatetimeSkip(value, destDt) {
						debugf("field %s.%s: timestamp vs datetime text skip (SkipTimestampToDatetime enabled)", table, fieldName)
					} else {
						alterSQL = fmt.Sprintf("CHANGE %s %s", quoteIdentifier(fieldName), value)
					}
				}
				beforeFieldName = fieldName
			} else {
				if len(beforeFieldName) == 0 {
					if fieldCount == 0 {
						alterSQL = "ADD " + value + " FIRST"
					} else {
						alterSQL = "ADD " + value
					}
				} else {
					alterSQL = fmt.Sprintf("ADD %s AFTER %s", value, quoteIdentifier(beforeFieldName))
				}
				beforeFieldName = fieldName
			}

			if len(alterSQL) != 0 {
				debugf("check column.alter %s.%s alterSQL=%s", table, fieldName, alterSQL)
				alterLines = append(alterLines, alterSQL)
			} else {
				debugf("check column.alter %s.%s not change", table, fieldName)
			}
			fieldCount++
		}
	}

	// 源库已经删除的字段
	if sc.Config.Drop {
		for _, name := range destMyS.Fields.Keys() {
			if sc.Config.IsIgnoreField(table, name) {
				log.Printf("ignore column %s.%s", table, name)
				continue
			}
			if _, has := sourceMyS.Fields.Get(name); !has {
				alterSQL := fmt.Sprintf("drop %s", quoteIdentifier(name))
				alterLines = append(alterLines, alterSQL)
				debugf("check column.drop %s.%s alterSQL=%s", table, name, alterSQL)
			} else {
				debugf("check column.drop %s.%s not change", table, name)
			}
		}
	}

	// 多余的字段暂不删除

	// 比对索引（sorted for deterministic output）
	for _, indexName := range sortedMapKeys(sourceMyS.IndexAll) {
		idx := sourceMyS.IndexAll[indexName]
		if sc.Config.IsIgnoreIndex(table, indexName) {
			log.Printf("ignore index %s.%s", table, indexName)
			continue
		}
		dIdx, has := destMyS.IndexAll[indexName]
		debugf("indexName---->[%s.%s] dest_has:%v\ndest_idx:%v\nsource_idx:%v", table, indexName, has, dIdx, idx)
		var alterSQLs []string
		if has {
			if idx.SQL != dIdx.SQL {
				alterSQLs = append(alterSQLs, idx.alterAddSQL(true)...)
			}
		} else {
			alterSQLs = append(alterSQLs, idx.alterAddSQL(false)...)
		}
		if len(alterSQLs) > 0 {
			alterLines = append(alterLines, alterSQLs...)
			debugf("check index.alter %s.%s alterSQL=%s", table, indexName, alterSQLs)
		} else {
			debugf("check index.alter %s.%s not change", table, indexName)
		}
	}

	// drop index
	if sc.Config.Drop {
		for _, indexName := range sortedMapKeys(destMyS.IndexAll) {
			dIdx := destMyS.IndexAll[indexName]
			if sc.Config.IsIgnoreIndex(table, indexName) {
				log.Printf("ignore index %s.%s", table, indexName)
				continue
			}
			var dropSQL string
			if _, has := sourceMyS.IndexAll[indexName]; !has {
				dropSQL = dIdx.alterDropSQL()
			}

			if len(dropSQL) != 0 {
				alterLines = append(alterLines, dropSQL)
				debugf("check index.drop %s.%s alterSQL=%s", table, indexName, dropSQL)
			} else {
				debugf("check index.drop %s.%s not change", table, indexName)
			}
		}
	}

	// 比对外键（sorted for deterministic output）
	for _, foreignName := range sortedMapKeys(sourceMyS.ForeignAll) {
		idx := sourceMyS.ForeignAll[foreignName]
		if sc.Config.IsIgnoreForeignKey(table, foreignName) {
			log.Printf("ignore foreignName %s.%s", table, foreignName)
			continue
		}
		dIdx, has := destMyS.ForeignAll[foreignName]
		debugf("foreignName---->[%s.%s] dest_has:%v\ndest_idx:%v\nsource_idx:%v", table, foreignName, has, dIdx, idx)
		var alterSQLs []string
		if has {
			if idx.SQL != dIdx.SQL {
				alterSQLs = append(alterSQLs, idx.alterAddSQL(true)...)
			}
		} else {
			alterSQLs = append(alterSQLs, idx.alterAddSQL(false)...)
		}
		if len(alterSQLs) > 0 {
			alterLines = append(alterLines, alterSQLs...)
			debugf("check foreignKey.alter %s.%s alterSQL=%s", table, foreignName, alterSQLs)
		} else {
			debugf("check foreignKey.alter %s.%s not change", table, foreignName)
		}
	}

	// drop 外键
	if sc.Config.Drop {
		for _, foreignName := range sortedMapKeys(destMyS.ForeignAll) {
			dIdx := destMyS.ForeignAll[foreignName]
			if sc.Config.IsIgnoreForeignKey(table, foreignName) {
				log.Printf("ignore foreignName %s.%s", table, foreignName)
				continue
			}
			var dropSQL string
			if _, has := sourceMyS.ForeignAll[foreignName]; !has {
				debugf("foreignName --->[%s.%s] didx:%v", table, foreignName, dIdx)
				dropSQL = dIdx.alterDropSQL()
			}
			if len(dropSQL) != 0 {
				alterLines = append(alterLines, dropSQL)
				debugf("check foreignKey.drop %s.%s alterSQL=%s", table, foreignName, dropSQL)
			} else {
				debugf("check foreignKey.drop %s.%s not change", table, foreignName)
			}
		}
	}

	return alterLines
}

// sortedMapKeys returns sorted keys from a map[string]*DbIndex for deterministic iteration
func sortedMapKeys(m map[string]*DbIndex) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// SyncSQL4Dest sync schema change
func (sc *SchemaSync) SyncSQL4Dest(sqlStr string, sqls []string) error {
	sqlStr = strings.TrimSpace(sqlStr)
	xcolor.Green(sqlStr)
	log.Print("Exec_SQL:\n>>>>>>\n", xcolor.GreenString(sqlStr), "\n<<<<<<<<\n\n")
	if len(sqlStr) == 0 {
		log.Println("sql_is_empty, skip")
		return nil
	}
	t := newMyTimer()
	ret, err := sc.DestDb.Query(sqlStr)
	if ret != nil {
		// Iterate all result sets to detect errors from 2nd+ statements in multi-statement queries
		for ret.NextResultSet() {
			// drain result sets; errors are captured below
		}
		if rsErr := ret.Err(); rsErr != nil && err == nil {
			err = fmt.Errorf("multi-statement execution error: %w", rsErr)
		}
		ret.Close()
		ret = nil
	}

	// If multi-statement query failed (e.g. allowMultiQueries not enabled), try each statement individually.
	// Note: DDL statements cause implicit commits in MySQL, so we do NOT wrap in a transaction —
	// rollback would be ineffective. Each statement is executed independently and results are tracked.
	if err != nil && len(sqls) > 1 {
		originalErr := err
		log.Println("Exec_mut_query failed, err=", errString(originalErr), ", now try exec SQLs foreach")
		var firstErr error
		var successCount int
		for i, sql := range sqls {
			_, err = sc.DestDb.Exec(sql)
			log.Println("exec_one:[", sql, "]", errString(err))
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("statement %d failed: %w", i+1, err)
				}
				break
			}
			successCount++
		}
		if firstErr != nil {
			log.Printf("[WARN] %d of %d DDL statements succeeded before failure (DDL implicit commit — partial changes may exist)", successCount, len(sqls))
			err = fmt.Errorf("fallback exec failed after original error (%w): %w", originalErr, firstErr)
		} else {
			err = nil
		}
	}
	t.stop()
	if err != nil {
		log.Println("EXEC_SQL_FAILED:", errString(err))
		return err
	}
	log.Println("EXEC_SQL_SUCCESS, used:", t.usedSecond())
	return nil
}
