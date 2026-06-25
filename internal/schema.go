package internal

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/xanygo/anygo/ds/xmap"
)

// MySchema table schema
type MySchema struct {
	Fields     xmap.Ordered[string, string] // Legacy: field name -> field definition string
	FieldInfos map[string]*FieldInfo        // New: structured field information
	IndexAll   map[string]*DbIndex
	ForeignAll map[string]*DbIndex
	SchemaRaw  string
}

func (mys *MySchema) String() string {
	if mys.Fields.Len() == 0 {
		return "<empty schema>"
	}
	var buf bytes.Buffer
	buf.WriteString("Fields:\n")
	for name, v := range mys.Fields.Iter() {
		buf.WriteString(fmt.Sprintf(" %v : %s\n", name, v))
	}

	if len(mys.FieldInfos) > 0 {
		buf.WriteString("FieldInfos:\n")
		fiNames := make([]string, 0, len(mys.FieldInfos))
		for name := range mys.FieldInfos {
			fiNames = append(fiNames, name)
		}
		sort.Strings(fiNames)
		for _, name := range fiNames {
			info := mys.FieldInfos[name]
			buf.WriteString(fmt.Sprintf(" %s : %s (charset: %v, collation: %v)\n",
				name, info.String(), info.CharsetName, info.CollationName))
		}
	}

	buf.WriteString("Index:\n")
	idxNames := make([]string, 0, len(mys.IndexAll))
	for name := range mys.IndexAll {
		idxNames = append(idxNames, name)
	}
	sort.Strings(idxNames)
	for _, name := range idxNames {
		idx := mys.IndexAll[name]
		buf.WriteString(fmt.Sprintf(" %s : %s\n", name, idx.SQL))
	}

	buf.WriteString("ForeignKey:\n")
	fkNames := make([]string, 0, len(mys.ForeignAll))
	for name := range mys.ForeignAll {
		fkNames = append(fkNames, name)
	}
	sort.Strings(fkNames)
	for _, name := range fkNames {
		idx := mys.ForeignAll[name]
		buf.WriteString(fmt.Sprintf("  %s : %s\n", name, idx.SQL))
	}
	return buf.String()
}

// GetFieldNames table names
func (mys *MySchema) GetFieldNames() []string {
	return mys.Fields.Keys()
}

func (mys *MySchema) RelationTables() []string {
	tbs := make(map[string]int)
	for _, idx := range mys.ForeignAll {
		for _, tb := range idx.RelationTables {
			tbs[tb] = 1
		}
	}
	tables := make([]string, 0, len(tbs))
	for tb := range tbs {
		tables = append(tables, tb)
	}
	sort.Strings(tables)
	return tables
}

// extractQuotedIdentifier extracts an identifier from a line that starts with the
// given quote character. Handles doubled quotes (e.g., `` `col``name` `` → "col`name").
// Returns the identifier without quotes, or "" if the line is malformed.
func extractQuotedIdentifier(line string, quote byte) string {
	// line starts with the quote character; skip it
	var name []byte
	i := 1
	for i < len(line) {
		if line[i] == quote {
			if i+1 < len(line) && line[i+1] == quote {
				// Doubled quote → literal quote character in the identifier
				name = append(name, quote)
				i += 2
				continue
			}
			// Single quote → end of identifier
			return string(name)
		}
		name = append(name, line[i])
		i++
	}
	return "" // no closing quote found
}

// ParseSchema parse table's schema
func ParseSchema(schema string) *MySchema {
	schema = strings.TrimSpace(schema)
	lines := strings.Split(schema, "\n")
	mys := &MySchema{
		SchemaRaw:  schema,
		FieldInfos: make(map[string]*FieldInfo),
		IndexAll:   make(map[string]*DbIndex),
		ForeignAll: make(map[string]*DbIndex),
	}

	for i := 1; i < len(lines)-1; i++ {
		line := strings.TrimSpace(lines[i])
		if len(line) == 0 {
			continue
		}

		line = strings.TrimRight(line, ",")
		switch line[0] {
		case '`':
			name := extractQuotedIdentifier(line, '`')
			if name == "" {
				continue // malformed line: no closing backtick
			}
			mys.Fields.Set(name, line)

		case '"':
			name := extractQuotedIdentifier(line, '"')
			if name == "" {
				continue // malformed line: no closing double quote
			}
			mys.Fields.Set(name, line)

		default:
			idx := parseDbIndexLine(line)
			if idx == nil {
				continue
			}
			switch idx.IndexType {
			case indexTypeForeignKey:
				mys.ForeignAll[idx.Name] = idx
			default:
				mys.IndexAll[idx.Name] = idx
			}
		}
	}
	return mys
}

type SchemaDiff struct {
	Source *MySchema
	Dest   *MySchema
	Table  string
}

func newSchemaDiff(table, source, dest string) *SchemaDiff {
	return &SchemaDiff{
		Table:  table,
		Source: ParseSchema(source),
		Dest:   ParseSchema(dest),
	}
}

// NewSchemaWithFieldInfos creates a MySchema with structured field information
func NewSchemaWithFieldInfos(schema string, fieldInfos map[string]*FieldInfo) *MySchema {
	mys := ParseSchema(schema)
	if mys != nil {
		mys.FieldInfos = fieldInfos
	}
	return mys
}

// NewSchemaDiffWithFieldInfos creates a SchemaDiff with structured field information
func NewSchemaDiffWithFieldInfos(table, sourceSchema, destSchema string, sourceFields, destFields map[string]*FieldInfo) *SchemaDiff {
	return &SchemaDiff{
		Table:  table,
		Source: NewSchemaWithFieldInfos(sourceSchema, sourceFields),
		Dest:   NewSchemaWithFieldInfos(destSchema, destFields),
	}
}

func (sdiff *SchemaDiff) RelationTables() []string {
	return sdiff.Source.RelationTables()
}
