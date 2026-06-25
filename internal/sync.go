package internal

import (
	"fmt"
	"log"
	"regexp"
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
		return nil, fmt.Errorf("source: %w", err)
	}
	destTables, err := sc.DestDb.GetTableNames()
	if err != nil {
		return nil, fmt.Errorf("dest: %w", err)
	}
	tables := slices.Clone(destTables)
	for _, name := range sourceTables {
		if !slices.Contains(tables, name) {
			tables = append(tables, name)
		}
	}
	sort.Strings(tables)
	return tables, nil
}

// RemoveTableSchemaConfig 删除表创建引擎信息，编码信息，分区信息，已修复同步表结构遇到分区表异常退出问题，
// 对于分区表，只会同步字段，索引，主键，外键的变更
// Uses case-insensitive match on ") ENGINE" to also tolerate MariaDB / dump tools
// that emit ")engine=" or ") engine=".
var engineReg = regexp.MustCompile(`(?i)\)\s*ENGINE\b`)

func RemoveTableSchemaConfig(schema string) string {
	loc := engineReg.FindStringIndex(schema)
	if loc != nil {
		// loc[0] is the byte offset of the ')'; keep everything up to and
		// including it, discarding the ENGINE clause and partition info.
		return schema[:loc[0]+1]
	}
	// Fallback: no ") ENGINE" found, return as-is (e.g. partitioned tables or test data)
	return schema
}

func (sc *SchemaSync) getAlterDataByTable(table string, cfg *Config) (*TableAlterData, error) {
	sourceExists, err := sc.SourceDb.HasTable(table)
	if err != nil {
		return nil, fmt.Errorf("check source table %q: %w", table, err)
	}
	destExists, err := sc.DestDb.HasTable(table)
	if err != nil {
		return nil, fmt.Errorf("check dest table %q: %w", table, err)
	}

	var sSchema, dSchema string
	if sourceExists {
		sSchema, err = sc.SourceDb.GetTableSchema(table)
		if err != nil {
			return nil, fmt.Errorf("get source schema for %q: %w", table, err)
		}
	}
	if destExists {
		dSchema, err = sc.DestDb.GetTableSchema(table)
		if err != nil {
			return nil, fmt.Errorf("get dest schema for %q: %w", table, err)
		}
	}
	if sourceExists && destExists && sSchema != dSchema {
		sourceFields, fieldsErr := sc.SourceDb.TableFieldsFromInformationSchema(table)
		if fieldsErr != nil {
			return nil, fmt.Errorf("get source field metadata for %q: %w", table, fieldsErr)
		}
		destFields, fieldsErr := sc.DestDb.TableFieldsFromInformationSchema(table)
		if fieldsErr != nil {
			return nil, fmt.Errorf("get dest field metadata for %q: %w", table, fieldsErr)
		}
		return sc.getAlterDataBySchemaWithFields(table, sSchema, dSchema, cfg, sourceFields, destFields), nil
	}
	return sc.getAlterDataBySchema(table, sSchema, dSchema, cfg), nil
}

func (sc *SchemaSync) getAlterDataBySchema(table string, sSchema string, dSchema string, cfg *Config) *TableAlterData {
	return sc.getAlterDataBySchemaWithFields(table, sSchema, dSchema, cfg, nil, nil)
}

func (sc *SchemaSync) getAlterDataBySchemaWithFields(
	table string,
	sSchema string,
	dSchema string,
	cfg *Config,
	sourceFields map[string]*FieldInfo,
	destFields map[string]*FieldInfo,
) *TableAlterData {
	alter := new(TableAlterData)
	alter.Table = table
	alter.Type = alterTypeNo

	// Early exit: if schemas are identical, no changes needed
	if sSchema == dSchema {
		alter.SchemaDiff = newSchemaDiff(table, RemoveTableSchemaConfig(sSchema), RemoveTableSchemaConfig(dSchema))
		return alter
	}

	if sourceFields != nil && destFields != nil {
		debugf("Using structured field comparison for table %q", table)
		alter.SchemaDiff = NewSchemaDiffWithFieldInfos(table, RemoveTableSchemaConfig(sSchema), RemoveTableSchemaConfig(dSchema), sourceFields, destFields)
	} else {
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
		createSQL, foreignKeys := splitCreateTableForeignKeys(sSchema)
		alter.SQL = append(alter.SQL, fmtTableCreateSQL(createSQL)+";")
		for _, foreignKey := range foreignKeys {
			alter.SQL = append(alter.SQL,
				fmt.Sprintf("ALTER TABLE %s\nADD %s;", quoteIdentifier(table), foreignKey))
		}
		return alter
	}

	diffLines := sc.getSchemaDiff(alter)
	if len(diffLines) == 0 {
		return alter
	}
	alter.Type = alterTypeAlter
	var foreignDrops, regularLines, foreignAdds []string
	for _, line := range diffLines {
		switch {
		case isForeignDropLine(line):
			foreignDrops = append(foreignDrops, line)
		case isForeignAddLine(line):
			foreignAdds = append(foreignAdds, line)
		default:
			regularLines = append(regularLines, line)
		}
	}
	orderedGroups := [][]string{foreignDrops, regularLines, foreignAdds}
	if cfg.SingleSchemaChange {
		for _, line := range slices.Concat(orderedGroups...) {
			ns := fmt.Sprintf("ALTER TABLE %s\n%s;", quoteIdentifier(table), line)
			alter.SQL = append(alter.SQL, ns)
		}
	} else {
		for _, lines := range orderedGroups {
			if len(lines) == 0 {
				continue
			}
			ns := fmt.Sprintf("ALTER TABLE %s\n%s;", quoteIdentifier(table), strings.Join(lines, ",\n"))
			alter.SQL = append(alter.SQL, ns)
		}
	}

	return alter
}

func isForeignDropLine(line string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(line)), "DROP FOREIGN KEY ")
}

func isForeignAddLine(line string) bool {
	upper := strings.ToUpper(strings.TrimSpace(line))
	return strings.HasPrefix(upper, "ADD ") && strings.Contains(upper, " FOREIGN KEY ")
}

// splitCreateTableForeignKeys removes foreign-key constraint lines from CREATE
// TABLE and returns them separately. This lets all tables be created before any
// cross-table constraints are installed, including cyclic relationships.
func splitCreateTableForeignKeys(schema string) (string, []string) {
	lines := strings.Split(schema, "\n")
	if len(lines) < 3 {
		return schema, nil
	}
	var kept []string
	var foreignKeys []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimRight(line, ","))
		upper := strings.ToUpper(trimmed)
		if strings.HasPrefix(upper, "CONSTRAINT ") && strings.Contains(upper, " FOREIGN KEY ") {
			foreignKeys = append(foreignKeys, trimmed)
			continue
		}
		kept = append(kept, line)
	}
	for i := len(kept) - 2; i >= 0; i-- {
		trimmed := strings.TrimSpace(kept[i])
		if trimmed == "" {
			continue
		}
		kept[i] = strings.TrimRight(kept[i], " \t,")
		break
	}
	return strings.Join(kept, "\n"), foreignKeys
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
				// Only use an ignored field as an AFTER anchor if it actually
				// exists in the destination. Ignored source-only fields are not
				// created, so referencing them would produce invalid DDL.
				if _, exists := destMyS.Fields.Get(fieldName); exists {
					beforeFieldName = fieldName
					fieldCount++
				}
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
		// Production comparisons fail closed when INFORMATION_SCHEMA cannot be
		// queried, so this path is limited to direct schema-based unit tests.
		// Note: This path uses raw SHOW CREATE TABLE text for SQL generation rather
		// than FieldInfo.String(). Full unification would require parsing FieldInfo
		// from raw text, which is not cost-effective given this path's limited use.
		debugf("Using legacy text-based field comparison for table %s", table)
		// Use legacy text-based comparison
		for fieldName, value := range sourceMyS.Fields.Iter() {
			if sc.Config.IsIgnoreField(table, fieldName) {
				log.Printf("ignore column %s.%s", table, fieldName)
				if _, exists := destMyS.Fields.Get(fieldName); exists {
					beforeFieldName = fieldName
					fieldCount++
				}
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

	var dropColumnLines []string
	// 源库已经删除的字段
	if sc.Config.Drop {
		for _, name := range destMyS.Fields.Keys() {
			if sc.Config.IsIgnoreField(table, name) {
				log.Printf("ignore column %s.%s", table, name)
				continue
			}
			if _, has := sourceMyS.Fields.Get(name); !has {
				alterSQL := fmt.Sprintf("DROP COLUMN %s", quoteIdentifier(name))
				dropColumnLines = append(dropColumnLines, alterSQL)
				debugf("check column.drop %s.%s alterSQL=%s", table, name, alterSQL)
			} else {
				debugf("check column.drop %s.%s not change", table, name)
			}
		}
	}

	// 多余的字段暂不删除

	var dropIndexLines, replaceIndexLines, addIndexLines []string
	// 比对索引（sorted for deterministic output）
	for _, indexName := range sortedMapKeys(sourceMyS.IndexAll) {
		idx := sourceMyS.IndexAll[indexName]
		if sc.Config.IsIgnoreIndex(table, indexName) {
			log.Printf("ignore index %s.%s", table, indexName)
			continue
		}
		dIdx, has := destMyS.IndexAll[indexName]
		debugf("indexName---->[%s.%s] dest_has:%v\ndest_idx:%v\nsource_idx:%v", table, indexName, has, dIdx, idx)
		if has {
			if idx.SQL != dIdx.SQL {
				dropSQL := dIdx.alterDropSQL()
				addSQLs := idx.alterAddSQL(false)
				if dropSQL != "" && len(addSQLs) > 0 {
					// Keep replacement in one ALTER TABLE even when
					// SingleSchemaChange is enabled. This avoids permanently
					// losing the old index if the replacement cannot be added,
					// and is required for AUTO_INCREMENT primary keys.
					replaceIndexLines = append(replaceIndexLines, dropSQL+", "+strings.Join(addSQLs, ", "))
				}
				debugf("check index.alter %s.%s changed", table, indexName)
			} else {
				debugf("check index.alter %s.%s not change", table, indexName)
			}
		} else {
			addIndexLines = append(addIndexLines, idx.alterAddSQL(false)...)
			debugf("check index.add %s.%s", table, indexName)
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
				dropIndexLines = append(dropIndexLines, dropSQL)
				debugf("check index.drop %s.%s alterSQL=%s", table, indexName, dropSQL)
			} else {
				debugf("check index.drop %s.%s not change", table, indexName)
			}
		}
	}

	var dropForeignLines, addForeignLines []string
	// 比对外键（sorted for deterministic output）
	for _, foreignName := range sortedMapKeys(sourceMyS.ForeignAll) {
		idx := sourceMyS.ForeignAll[foreignName]
		if sc.Config.IsIgnoreForeignKey(table, foreignName) {
			log.Printf("ignore foreignName %s.%s", table, foreignName)
			continue
		}
		dIdx, has := destMyS.ForeignAll[foreignName]
		debugf("foreignName---->[%s.%s] dest_has:%v\ndest_idx:%v\nsource_idx:%v", table, foreignName, has, dIdx, idx)
		if has {
			if idx.SQL != dIdx.SQL {
				if dropSQL := dIdx.alterDropSQL(); dropSQL != "" {
					dropForeignLines = append(dropForeignLines, dropSQL)
				}
				addForeignLines = append(addForeignLines, idx.alterAddSQL(false)...)
				debugf("check foreignKey.alter %s.%s changed", table, foreignName)
			} else {
				debugf("check foreignKey.alter %s.%s not change", table, foreignName)
			}
		} else {
			addForeignLines = append(addForeignLines, idx.alterAddSQL(false)...)
			debugf("check foreignKey.add %s.%s", table, foreignName)
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
				dropForeignLines = append(dropForeignLines, dropSQL)
				debugf("check foreignKey.drop %s.%s alterSQL=%s", table, foreignName, dropSQL)
			} else {
				debugf("check foreignKey.drop %s.%s not change", table, foreignName)
			}
		}
	}

	// Destructive dependencies must be removed before columns; indexes are
	// restored after column changes, and foreign keys are always installed last.
	return slices.Concat(
		dropForeignLines,
		dropIndexLines,
		dropColumnLines,
		alterLines,
		replaceIndexLines,
		addIndexLines,
		addForeignLines,
	)
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
	var successCount int
	for i, statement := range sqls {
		statement = strings.TrimSpace(strings.TrimRight(statement, ";"))
		if statement == "" {
			continue
		}
		_, err := sc.DestDb.Exec(statement)
		log.Println("exec_one:[", statement, "]", errString(err))
		if err != nil {
			t.stop()
			retErr := fmt.Errorf("DDL execution stopped after %d/%d succeeded; statement %d failed: %w",
				successCount, len(sqls), i+1, err)
			log.Println("EXEC_SQL_FAILED:", errString(retErr))
			return retErr
		}
		successCount++
	}
	t.stop()
	log.Println("EXEC_SQL_SUCCESS, used:", t.usedSecond())
	return nil
}
