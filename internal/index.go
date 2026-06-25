package internal

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
)

// DbIndex db index
type DbIndex struct {
	IndexType indexType
	Name      string
	SQL       string

	// 相关联的表
	RelationTables []string

	// 外键引用的父表字段，顺序与 REFERENCES 子句一致
	ReferencedColumns []string
}

type indexType string

const (
	indexTypePrimary    indexType = "PRIMARY"
	indexTypeIndex      indexType = "INDEX"
	indexTypeForeignKey indexType = "FOREIGN KEY"
	indexTypeCheck      indexType = "CHECK"
)

func (idx *DbIndex) alterAddSQL(drop bool) []string {
	var alterSQL []string
	if drop {
		dropSQL := idx.alterDropSQL()
		if len(dropSQL) != 0 {
			alterSQL = append(alterSQL, dropSQL)
		}
	}

	switch idx.IndexType {
	case indexTypePrimary:
		alterSQL = append(alterSQL, "ADD "+idx.SQL)
	case indexTypeIndex, indexTypeForeignKey:
		alterSQL = append(alterSQL, fmt.Sprintf("ADD %s", idx.SQL))
	case indexTypeCheck:
		alterSQL = append(alterSQL, fmt.Sprintf("ADD %s", idx.SQL))
	default:
		log.Printf("[WARN] unknown indexType in alterAddSQL: %s", idx.IndexType)
	}
	return alterSQL
}

func (idx *DbIndex) String() string {
	bs, err := json.MarshalIndent(idx, "  ", " ")
	if err != nil {
		log.Printf("[WARN] DbIndex.String() marshal failed: %v", err)
		return fmt.Sprintf("{Name:%q, Type:%s, SQL:%q}", idx.Name, idx.IndexType, idx.SQL)
	}
	return string(bs)
}

func (idx *DbIndex) alterDropSQL() string {
	switch idx.IndexType {
	case indexTypePrimary:
		return "DROP PRIMARY KEY"
	case indexTypeIndex:
		return fmt.Sprintf("DROP INDEX %s", quoteIdentifier(idx.Name))
	case indexTypeForeignKey:
		return fmt.Sprintf("DROP FOREIGN KEY %s", quoteIdentifier(idx.Name))
	case indexTypeCheck:
		return fmt.Sprintf("DROP CHECK %s", quoteIdentifier(idx.Name))
	default:
		log.Printf("[WARN] unknown indexType in alterDropSQL: %s", idx.IndexType)
	}
	return ""
}

func (idx *DbIndex) addRelationTable(table string) {
	table = strings.TrimSpace(table)
	if len(table) != 0 {
		idx.RelationTables = append(idx.RelationTables, table)
	}
}

// 匹配索引字段
// L6: (?i) flag to tolerate MariaDB / dump tools that emit lowercase keywords.
var indexReg = regexp.MustCompile(`(?i)^([A-Z]+\s)?KEY\s+` + "`")

// 匹配外键 — H5: regex no longer captures identifier names directly; we use
// extractQuotedIdentifier for doubled-backtick safety instead.
var foreignKeyReg = regexp.MustCompile(`(?i)^CONSTRAINT\s+`)

// Check约束 — H5: same approach as foreignKeyReg.
var checkConstraintReg = regexp.MustCompile(`(?i)^CONSTRAINT\s+`)

func parseDbIndexLine(line string) *DbIndex {
	line = strings.TrimSpace(line)
	upperLine := strings.ToUpper(line)
	idx := &DbIndex{
		SQL:               line,
		RelationTables:    []string{},
		ReferencedColumns: []string{},
	}
	if strings.HasPrefix(upperLine, "PRIMARY") {
		idx.IndexType = indexTypePrimary
		idx.Name = "PRIMARY KEY"
		return idx
	}

	// UNIQUE KEY `idx_a` (`a`) USING HASH COMMENT '注释',
	// FULLTEXT KEY `c` (`c`)
	// PRIMARY KEY (`d`)
	// KEY `idx_e` (`e`),
	if indexReg.MatchString(line) {
		// Use extractQuotedIdentifier to handle doubled backticks in index names
		idx.IndexType = indexTypeIndex
		name, ok := extractQuotedIdentifier(line[strings.IndexByte(line, '`'):], '`')
		if !ok || name == "" {
			log.Printf("[WARN] db_index parse skipped: KEY line without backticks: %s", line)
			return nil
		}
		idx.Name = name
		return idx
	}

	// CONSTRAINT `busi_table_ibfk_1` FOREIGN KEY (`repo_id`) REFERENCES `repo_table` (`repo_id`)
	// H5: use extractQuotedIdentifier for doubled-backtick safety in constraint
	// and referenced-table names.
	if foreignKeyReg.MatchString(line) && strings.Contains(upperLine, "FOREIGN KEY") {
		constraintStart := strings.IndexByte(line, '`')
		if constraintStart < 0 {
			log.Printf("[WARN] db_index parse skipped: CONSTRAINT line without backticks: %s", line)
			return nil
		}
		constraintName, ok := extractQuotedIdentifier(line[constraintStart:], '`')
		if !ok || constraintName == "" {
			log.Printf("[WARN] db_index parse skipped: CONSTRAINT FK with unclosed name: %s", line)
			return nil
		}
		idx.IndexType = indexTypeForeignKey
		idx.Name = constraintName
		// Extract referenced table name from REFERENCES `<tbl>`
		refIdx := strings.Index(upperLine, "REFERENCES `")
		if refIdx >= 0 {
			referencePart := line[refIdx+len("REFERENCES "):]
			refTable, refOk := extractQuotedIdentifier(referencePart, '`')
			if refOk && refTable != "" {
				idx.addRelationTable(refTable)
				if tableEnd := quotedIdentifierEnd(referencePart); tableEnd > 0 {
					idx.ReferencedColumns = parseQuotedIdentifierList(referencePart[tableEnd:])
				}
			}
		}
		return idx
	}

	// CONSTRAINT `chk_xx_1` CHECK ((`x` >= 0 and `y` <= 100))
	if checkConstraintReg.MatchString(line) && strings.Contains(upperLine, "CHECK") {
		constraintStart := strings.IndexByte(line, '`')
		if constraintStart < 0 {
			log.Printf("[WARN] db_index parse skipped: CONSTRAINT line without backticks: %s", line)
			return nil
		}
		constraintName, ok := extractQuotedIdentifier(line[constraintStart:], '`')
		if !ok || constraintName == "" {
			log.Printf("[WARN] db_index parse skipped: CONSTRAINT CHECK with unclosed name: %s", line)
			return nil
		}
		idx.IndexType = indexTypeCheck
		idx.Name = constraintName
		return idx
	}

	log.Printf("[WARN] db_index parse skipped, unsupported line: %s", line)
	return nil
}

func parseQuotedIdentifierList(s string) []string {
	open := strings.IndexByte(s, '(')
	close := strings.IndexByte(s, ')')
	if open < 0 || close <= open {
		return nil
	}
	var names []string
	rest := s[open+1 : close]
	for {
		rest = strings.TrimSpace(rest)
		if rest == "" {
			break
		}
		if rest[0] != '`' {
			return nil
		}
		name, ok := extractQuotedIdentifier(rest, '`')
		if !ok || name == "" {
			return nil
		}
		names = append(names, name)
		end := quotedIdentifierEnd(rest)
		if end < 0 {
			return nil
		}
		rest = strings.TrimSpace(rest[end:])
		if rest == "" {
			break
		}
		if rest[0] != ',' {
			return nil
		}
		rest = rest[1:]
	}
	return names
}
