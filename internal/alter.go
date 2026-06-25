package internal

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

type alterType int

const (
	alterTypeNo alterType = iota
	alterTypeCreate
	alterTypeDropTable
	alterTypeAlter
)

func (at alterType) String() string {
	switch at {
	case alterTypeNo:
		return "not_change"
	case alterTypeCreate:
		return "create"
	case alterTypeDropTable:
		return "drop"
	case alterTypeAlter:
		return "alter"
	default:
		return "unknown"
	}
}

// TableAlterData 表的变更情况
type TableAlterData struct {
	SchemaDiff *SchemaDiff
	Table      string
	Comment    string
	SQL        []string
	Type       alterType
}

func (ta *TableAlterData) Split() []*TableAlterData {
	rs := make([]*TableAlterData, len(ta.SQL))
	for i := 0; i < len(ta.SQL); i++ {
		rs[i] = &TableAlterData{
			SchemaDiff: ta.SchemaDiff,
			Table:      ta.Table,
			Comment:    ta.Comment,
			Type:       ta.Type,
			SQL:        []string{ta.SQL[i]},
		}
	}
	return rs
}

func (ta *TableAlterData) String() string {
	relationTables := ta.SchemaDiff.RelationTables()
	sqlTpl := `
-- Table : %s
-- Type : %s
-- RelationTables :%s
-- Comment :%s
-- SQL :
%s
`
	str := fmt.Sprintf(sqlTpl,
		ta.Table,
		ta.Type,
		strings.Join(relationTables, ","),
		strings.TrimSpace(ta.Comment),
		strings.Join(ta.SQL, "\n"),
	)
	return strings.TrimSpace(str)
}

// autoIncrReg matches the AUTO_INCREMENT=N table-option clause along with its
// leading whitespace, but intentionally does NOT consume trailing whitespace.
// This preserves any trailing newline so multi-line CREATE TABLE statements
// remain intact when the clause sits at end-of-line. Allows AUTO_INCREMENT=0
// (older MySQL dumps may legitimately emit it). The (?i) flag tolerates
// servers/dump tools that emit lowercase `auto_increment=` in CREATE output.
var autoIncrReg = regexp.MustCompile(`(?i)\s+AUTO_INCREMENT=\d+\b`)

func fmtTableCreateSQL(sql string) string {
	// L10: Only strip AUTO_INCREMENT from the table-options region (after the
	// last ')' that closes column definitions). This prevents matching inside
	// column COMMENT string literals which could corrupt the DDL.
	lastParen := strings.LastIndex(sql, ")")
	if lastParen < 0 {
		// No closing paren — fall back to applying on the whole string
		return strings.TrimRightFunc(autoIncrReg.ReplaceAllString(sql, ""), unicode.IsSpace)
	}
	prefix := sql[:lastParen+1]
	suffix := sql[lastParen+1:]
	suffix = autoIncrReg.ReplaceAllString(suffix, "")
	result := prefix + suffix
	return strings.TrimRightFunc(result, unicode.IsSpace)
}
