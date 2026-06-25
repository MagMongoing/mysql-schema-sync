//  Copyright(C) 2025 github.com/hidu  All Rights Reserved.
//  Author: hidu <duv123+git@gmail.com>
//  Date: 2025-10-21

package internal

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/xanygo/anygo/cli/xcolor"
)

type executionItem struct {
	phase       int
	order       int
	sql         string
	sd          *TableAlterData
	index       int
	st          *tableStatics
	temporaryFK bool
	fkKey       string
	fkRestore   bool
}

func Execute(cfg *Config) (runErr error) {
	scs := newStatics(cfg)
	defer func() {
		scs.timer.stop()
		if runErr != nil {
			scs.fatalErr = runErr
		}
		scs.sendMailNotice(cfg)
	}()

	sc, err := NewSchemaSync(cfg)
	if err != nil {
		log.Printf("[FATAL] failed to initialize schema sync: %s", err)
		scs.fatalErr = err
		return err
	}
	defer func() {
		if sc.SourceDb != nil {
			if err := sc.SourceDb.Close(); err != nil {
				log.Printf("[WARN] close SourceDb: %s", err)
			}
		}
		if sc.DestDb != nil {
			if err := sc.DestDb.Close(); err != nil {
				log.Printf("[WARN] close DestDb: %s", err)
			}
		}
	}()
	allTables, err := sc.AllDBTables()
	if err != nil {
		log.Printf("[FATAL] failed to list tables: %s", err)
		scs.fatalErr = err
		return err
	}
	// log.Println("source db table total:", len(allTables))

	changedTables := make(map[string][]*TableAlterData)
	allTableSchemas := make(map[string]*TableAlterData)
	var runErrors []error

	for _, table := range allTables {
		xcolor.Green("start checking table %q ...", table)
		if !cfg.CheckMatchTables(table) {
			xcolor.Cyan("table %q skipped by not match", table)
			snapshot, snapshotErr := sc.getDestOnlySchemaSnapshot(table)
			if snapshotErr != nil {
				runErrors = append(runErrors, fmt.Errorf("inspect destination dependencies for table %q: %w", table, snapshotErr))
			} else if snapshot != nil {
				allTableSchemas[table] = snapshot
			}
			continue
		}

		if cfg.CheckMatchIgnoreTables(table) {
			xcolor.Cyan("table %q skipped by ignore", table)
			snapshot, snapshotErr := sc.getDestOnlySchemaSnapshot(table)
			if snapshotErr != nil {
				runErrors = append(runErrors, fmt.Errorf("inspect destination dependencies for ignored table %q: %w", table, snapshotErr))
			} else if snapshot != nil {
				allTableSchemas[table] = snapshot
			}
			continue
		}

		sd, err := sc.getAlterDataByTable(table, cfg)
		if err != nil {
			log.Printf("[ERROR] skip table %q: %s", table, err)
			runErrors = append(runErrors, fmt.Errorf("inspect table %q: %w", table, err))
			continue
		}
		allTableSchemas[table] = sd

		switch sd.Type {
		case alterTypeNo:
			xcolor.Yellow("table %q not changed", table)
			continue
		case alterTypeDropTable:
			xcolor.Yellow("table %q skipped, only exists in destination's database", table)
			continue
		default:
		}

		fmt.Printf("\n%s\n\n", sd)

		relationTables := sd.SchemaDiff.RelationTables()
		log.Printf("table %q RelationTables: %q", table, relationTables)

		// 将所有有外键关联的单独放
		groupKey := "multi"
		if len(relationTables) == 0 {
			groupKey = "single_" + table
		}
		if _, has := changedTables[groupKey]; !has {
			changedTables[groupKey] = make([]*TableAlterData, 0)
		}
		changedTables[groupKey] = append(changedTables[groupKey], sd)
	}

	// Never execute a plan built from an incomplete schema snapshot. A skipped
	// table can hide dependencies that make otherwise-valid DDL destructive.
	if len(runErrors) > 0 {
		return errors.Join(runErrors...)
	}

	var countSuccess int
	var countFailed int
	var alters []*TableAlterData
	for _, group := range changedTables {
		alters = append(alters, group...)
	}
	alters = orderTableAlters(alters)

	items := buildExecutionItems(alters)
	items, err = addInboundForeignKeyItems(items, alters, allTableSchemas, cfg)
	if err != nil {
		return err
	}
	sortExecutionItems(items)

	if sc.Config.Sync {
		if err := verifyPlanSnapshots(sc, alters); err != nil {
			return err
		}
	}

	for i := range items {
		items[i].st = scs.newTableStatics(items[i].sd.Table, items[i].sd, items[i].index)
	}

	var firstExecutionErr error
	detachedFKs := make(map[string]bool)
	for i := range items {
		item := &items[i]
		if firstExecutionErr != nil {
			// Compensating restores are the sole exception to fail-fast: if this
			// run successfully detached an unchanged inbound FK before the
			// failure, make a best effort to put that exact constraint back.
			if item.temporaryFK && item.fkRestore && detachedFKs[item.fkKey] && sc.Config.Sync {
				ret := sc.SyncSQL4Dest(item.sql+";", []string{item.sql})
				item.st.alterRet = ret
				if ret != nil {
					recoveryErr := fmt.Errorf("restore inbound foreign key %q after failure: %w", item.fkKey, ret)
					runErrors = append(runErrors, recoveryErr)
				} else {
					delete(detachedFKs, item.fkKey)
				}
				item.st.timer.stop()
				continue
			}
			item.st.skipped = true
			item.st.skipReason = fmt.Errorf("an earlier DDL failed: %w", firstExecutionErr)
			item.st.timer.stop()
			continue
		}

		var ret error
		if sc.Config.Sync {
			ret = sc.SyncSQL4Dest(item.sql+";", []string{item.sql})
			if ret == nil {
				countSuccess++
				if item.temporaryFK && !item.fkRestore {
					detachedFKs[item.fkKey] = true
				}
				if item.temporaryFK && item.fkRestore {
					delete(detachedFKs, item.fkKey)
				}
			} else {
				countFailed++
				firstExecutionErr = fmt.Errorf("sync table %q: %w", item.st.table, ret)
				runErrors = append(runErrors, firstExecutionErr)
			}
		}
		item.st.alterRet = ret
		if sc.Config.Sync {
			exists, existsErr := sc.DestDb.HasTable(item.st.table)
			if existsErr != nil {
				log.Printf("[WARN] check schema after sync for %q failed: %s", item.st.table, existsErr)
			} else if exists {
				item.st.schemaAfter, existsErr = sc.DestDb.GetTableSchema(item.st.table)
				if existsErr != nil {
					log.Printf("[WARN] get schema after sync for %q failed: %s", item.st.table, existsErr)
				}
			}
		}
		item.st.timer.stop()
	}

	if sc.Config.Sync {
		log.Println("execute_all_sql_done, success_total:", countSuccess, "failed_total:", countFailed)
	}
	for key := range detachedFKs {
		runErrors = append(runErrors, fmt.Errorf("inbound foreign key %q remains detached after failed recovery", key))
	}
	if sc.Config.Sync && len(runErrors) == 0 {
		verified := make(map[string]bool)
		for _, alter := range alters {
			if verified[alter.Table] {
				continue
			}
			verified[alter.Table] = true
			remaining, verifyErr := sc.getAlterDataByTable(alter.Table, cfg)
			if verifyErr != nil {
				runErrors = append(runErrors, fmt.Errorf("verify table %q after sync: %w", alter.Table, verifyErr))
				continue
			}
			if remaining.Type == alterTypeCreate || remaining.Type == alterTypeAlter {
				runErrors = append(runErrors, fmt.Errorf(
					"verify table %q after sync: schema has not converged; remaining SQL: %s",
					alter.Table, strings.Join(remaining.SQL, "\n")))
			}
		}
	}
	return errors.Join(runErrors...)
}

// verifyPlanSnapshots refuses to execute DDL generated from stale schemas.
// Planning performs several metadata queries and may take long enough for a
// concurrent deployment or manual ALTER to change either database. Re-reading
// the relevant tables immediately before execution narrows that race window and
// prevents applying a plan whose assumptions are already known to be invalid.
func verifyPlanSnapshots(sc *SchemaSync, alters []*TableAlterData) error {
	for _, alter := range alters {
		if alter == nil || alter.SchemaDiff == nil {
			return fmt.Errorf("preflight schema verification for table %q: missing plan snapshot", alterTableName(alter))
		}
		if err := verifyTableSnapshot(sc.SourceDb, alter.Table, alter.SchemaDiff.Source.SchemaRaw); err != nil {
			return fmt.Errorf("source schema changed after planning for table %q: %w", alter.Table, err)
		}
		if err := verifyTableSnapshot(sc.DestDb, alter.Table, alter.SchemaDiff.Dest.SchemaRaw); err != nil {
			return fmt.Errorf("destination schema changed after planning for table %q: %w", alter.Table, err)
		}
	}
	return nil
}

func verifyTableSnapshot(db *MyDb, table, expectedSchema string) error {
	exists, err := db.HasTable(table)
	if err != nil {
		return err
	}
	if expectedSchema == "" {
		if exists {
			return fmt.Errorf("expected table to be absent, but it now exists")
		}
		return nil
	}
	if !exists {
		return fmt.Errorf("expected table to exist, but it is now absent")
	}
	currentSchema, err := db.GetTableSchema(table)
	if err != nil {
		return err
	}
	currentSchema = RemoveTableSchemaConfig(currentSchema)
	if currentSchema != expectedSchema {
		return fmt.Errorf("schema drift detected")
	}
	return nil
}

func alterTableName(alter *TableAlterData) string {
	if alter == nil {
		return "<nil>"
	}
	return alter.Table
}

func (sc *SchemaSync) getDestOnlySchemaSnapshot(table string) (*TableAlterData, error) {
	exists, err := sc.DestDb.HasTable(table)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	schema, err := sc.DestDb.GetTableSchema(table)
	if err != nil {
		return nil, err
	}
	return &TableAlterData{
		SchemaDiff: newSchemaDiff(table, "", RemoveTableSchemaConfig(schema)),
		Table:      table,
		Type:       alterTypeDropTable,
		Comment:    "仅用于分析目标库入向外键依赖",
	}, nil
}

func buildExecutionItems(alters []*TableAlterData) []executionItem {
	var items []executionItem
	for order, sd := range alters {
		for index, rawSQL := range sd.SQL {
			sql := strings.TrimSpace(strings.TrimRight(rawSQL, ";"))
			phase := ddlExecutionPhase(sql)
			items = append(items, executionItem{
				phase: phase,
				order: order,
				sql:   sql,
				sd:    sd,
				index: index,
			})
		}
	}
	return items
}

func sortExecutionItems(items []executionItem) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].phase != items[j].phase {
			return items[i].phase < items[j].phase
		}
		return items[i].order < items[j].order
	})
}

// addInboundForeignKeyItems temporarily removes unchanged destination foreign
// keys that reference a table being altered. MySQL can otherwise reject parent
// key/column/index changes even though the child constraint itself has no diff.
func addInboundForeignKeyItems(items []executionItem, alters []*TableAlterData, allSchemas map[string]*TableAlterData, cfg *Config) ([]executionItem, error) {
	changedParents := make(map[string]parentInboundImpact)
	detachOrder := -len(allSchemas) - 1
	restoreOrder := len(alters)
	for _, alter := range alters {
		if alter.Type != alterTypeAlter {
			continue
		}
		impact := parentInboundImpact{columns: make(map[string]bool)}
		for _, sql := range alter.SQL {
			impact.merge(inboundImpactForAlter(sql))
		}
		if impact.all || len(impact.columns) > 0 {
			changedParents[alter.Table] = impact
		}
	}

	plannedFK := make(map[string]bool)
	for _, item := range items {
		for _, fkName := range foreignKeyNamesFromAlterSQL(item.sql) {
			plannedFK[item.sd.Table+"\x00"+fkName] = true
		}
	}

	tableNames := make([]string, 0, len(allSchemas))
	for table := range allSchemas {
		tableNames = append(tableNames, table)
	}
	sort.Strings(tableNames)
	for _, childTable := range tableNames {
		snapshot := allSchemas[childTable]
		for _, fkName := range sortedMapKeys(snapshot.SchemaDiff.Dest.ForeignAll) {
			destFK := snapshot.SchemaDiff.Dest.ForeignAll[fkName]
			needsDetach := false
			for _, parent := range destFK.RelationTables {
				impact, changed := changedParents[parent]
				if changed && impact.affects(destFK.ReferencedColumns) {
					needsDetach = true
					break
				}
			}
			key := childTable + "\x00" + fkName
			if !needsDetach || plannedFK[key] {
				continue
			}
			if !cfg.CheckMatchTables(childTable) {
				return nil, fmt.Errorf(
					"cannot safely alter referenced table: inbound foreign key %s.%s belongs to a table excluded by the tables whitelist",
					childTable, fkName)
			}
			if cfg.CheckMatchIgnoreTables(childTable) {
				return nil, fmt.Errorf(
					"cannot safely alter referenced table: inbound foreign key %s.%s belongs to an ignored table",
					childTable, fkName)
			}
			if cfg.IsIgnoreForeignKey(childTable, fkName) {
				return nil, fmt.Errorf(
					"cannot safely alter referenced table: inbound foreign key %s.%s is protected by alter_ignore.foreign",
					childTable, fkName)
			}
			desiredFK := snapshot.SchemaDiff.Source.ForeignAll[fkName]
			if desiredFK == nil {
				return nil, fmt.Errorf(
					"cannot safely alter referenced table: destination-only inbound foreign key %s.%s has no source definition to restore after the parent change",
					childTable, fkName)
			}

			dropSD := singleStatementAlter(snapshot, fmt.Sprintf(
				"ALTER TABLE %s\nDROP FOREIGN KEY %s;",
				quoteIdentifier(childTable), quoteIdentifier(fkName)))
			items = append(items, executionItem{
				phase:       0,
				order:       detachOrder,
				sql:         strings.TrimRight(dropSD.SQL[0], ";"),
				sd:          dropSD,
				temporaryFK: true,
				fkKey:       key,
			})
			detachOrder++

			addSD := singleStatementAlter(snapshot, fmt.Sprintf(
				"ALTER TABLE %s\nADD %s;",
				quoteIdentifier(childTable), desiredFK.SQL))
			items = append(items, executionItem{
				phase:       2,
				order:       restoreOrder,
				sql:         strings.TrimRight(addSD.SQL[0], ";"),
				sd:          addSD,
				temporaryFK: true,
				fkKey:       key,
				fkRestore:   true,
			})
			restoreOrder++
		}
	}
	return items, nil
}

type parentInboundImpact struct {
	all     bool
	columns map[string]bool
}

func (p *parentInboundImpact) merge(other parentInboundImpact) {
	p.all = p.all || other.all
	if p.columns == nil {
		p.columns = make(map[string]bool)
	}
	for column := range other.columns {
		p.columns[column] = true
	}
}

func (p parentInboundImpact) affects(referencedColumns []string) bool {
	if p.all {
		return true
	}
	// Missing column metadata is treated conservatively.
	if len(referencedColumns) == 0 {
		return len(p.columns) > 0
	}
	for _, column := range referencedColumns {
		if p.columns[column] {
			return true
		}
	}
	return false
}

// inboundImpactForAlter identifies parent-table operations that can invalidate
// referencing constraints. Column changes are tracked precisely; key removals
// conservatively affect every inbound constraint.
func inboundImpactForAlter(sql string) parentInboundImpact {
	impact := parentInboundImpact{columns: make(map[string]bool)}
	for _, clause := range alterOperationClauses(sql) {
		upper := strings.ToUpper(clause)
		switch {
		case strings.HasPrefix(upper, "DROP PRIMARY KEY"),
			strings.HasPrefix(upper, "DROP INDEX "):
			impact.all = true
		default:
			for _, prefix := range []string{"CHANGE COLUMN ", "CHANGE ", "MODIFY COLUMN ", "MODIFY ", "DROP COLUMN "} {
				if !strings.HasPrefix(upper, prefix) {
					continue
				}
				rest := strings.TrimSpace(clause[len(prefix):])
				if name, ok := extractQuotedIdentifier(rest, '`'); ok && name != "" {
					impact.columns[name] = true
				} else {
					impact.all = true
				}
				break
			}
		}
	}
	return impact
}

func foreignKeyNameFromAlterSQL(sql string) (string, bool) {
	names := foreignKeyNamesFromAlterSQL(sql)
	if len(names) == 0 {
		return "", false
	}
	return names[0], true
}

func foreignKeyNamesFromAlterSQL(sql string) []string {
	var names []string
	for _, clause := range alterOperationClauses(sql) {
		upper := strings.ToUpper(clause)
		var rest string
		switch {
		case strings.HasPrefix(upper, "DROP FOREIGN KEY "):
			rest = strings.TrimSpace(clause[len("DROP FOREIGN KEY "):])
		case strings.HasPrefix(upper, "ADD CONSTRAINT "):
			rest = strings.TrimSpace(clause[len("ADD CONSTRAINT "):])
			if !quotedIdentifierFollowedBy(rest, "FOREIGN KEY ") {
				continue
			}
		default:
			continue
		}
		if rest != "" && rest[0] == '`' {
			if name, ok := extractQuotedIdentifier(rest, '`'); ok && name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}

// alterOperationClauses returns generated ALTER TABLE operation clauses. The
// diff builder emits one operation per line, joined by ",\n", so inspecting
// line prefixes avoids mistaking arbitrary column comments for DDL keywords.
func alterOperationClauses(sql string) []string {
	lines := strings.Split(sql, "\n")
	if len(lines) == 0 {
		return nil
	}
	firstLine := strings.TrimSpace(lines[0])
	const alterTablePrefix = "ALTER TABLE "
	if !strings.HasPrefix(strings.ToUpper(firstLine), alterTablePrefix) {
		return nil
	}
	tableAndMaybeClause := strings.TrimSpace(firstLine[len(alterTablePrefix):])
	if len(tableAndMaybeClause) == 0 || tableAndMaybeClause[0] != '`' {
		return nil
	}
	tableEnd := quotedIdentifierEnd(tableAndMaybeClause)
	if tableEnd < 0 {
		return nil
	}
	clauses := make([]string, 0, len(lines))
	if remainder := strings.TrimSpace(strings.TrimRight(tableAndMaybeClause[tableEnd:], ",;")); remainder != "" {
		clauses = append(clauses, remainder)
	}
	for _, line := range lines[1:] {
		line = strings.TrimSpace(strings.TrimRight(line, ",;"))
		if line != "" {
			clauses = append(clauses, line)
		}
	}
	return clauses
}

func quotedIdentifierFollowedBy(s, keyword string) bool {
	end := quotedIdentifierEnd(s)
	if end < 0 {
		return false
	}
	rest := strings.ToUpper(strings.TrimSpace(s[end:]))
	return strings.HasPrefix(rest, keyword)
}

func quotedIdentifierEnd(s string) int {
	if len(s) == 0 || s[0] != '`' {
		return -1
	}
	for i := 1; i < len(s); i++ {
		if s[i] != '`' {
			continue
		}
		if i+1 < len(s) && s[i+1] == '`' {
			i++
			continue
		}
		return i + 1
	}
	return -1
}

func singleStatementAlter(base *TableAlterData, sql string) *TableAlterData {
	return &TableAlterData{
		SchemaDiff: base.SchemaDiff,
		Table:      base.Table,
		Comment:    "为安全修改被引用表，临时调整入向外键",
		Type:       alterTypeAlter,
		SQL:        []string{sql},
	}
}

// ddlExecutionPhase enforces dependency-safe DDL ordering globally:
// foreign keys are removed first and installed only after all table/column/index
// changes have completed.
func ddlExecutionPhase(sql string) int {
	clauses := alterOperationClauses(sql)
	if len(clauses) == 0 {
		return 1
	}
	first := strings.ToUpper(clauses[0])
	if strings.HasPrefix(first, "DROP FOREIGN KEY ") {
		return 0
	}
	if strings.HasPrefix(first, "ADD CONSTRAINT ") {
		rest := strings.TrimSpace(clauses[0][len("ADD CONSTRAINT "):])
		if quotedIdentifierFollowedBy(rest, "FOREIGN KEY ") {
			return 2
		}
	}
	return 1
}

// orderTableAlters topologically orders changed tables by their referenced
// tables. Cycles are appended deterministically; they are safe because foreign
// key installation is deferred to the final execution phase.
func orderTableAlters(alters []*TableAlterData) []*TableAlterData {
	byTable := make(map[string]*TableAlterData, len(alters))
	for _, alter := range alters {
		byTable[alter.Table] = alter
	}
	indegree := make(map[string]int, len(byTable))
	children := make(map[string][]string, len(byTable))
	for table, alter := range byTable {
		indegree[table] = 0
		for _, parent := range alter.SchemaDiff.RelationTables() {
			if parent == table || byTable[parent] == nil {
				continue
			}
			children[parent] = append(children[parent], table)
			indegree[table]++
		}
	}
	var ready []string
	for table, degree := range indegree {
		if degree == 0 {
			ready = append(ready, table)
		}
	}
	sort.Strings(ready)
	var ordered []*TableAlterData
	seen := make(map[string]bool, len(byTable))
	for len(ready) > 0 {
		table := ready[0]
		ready = ready[1:]
		if seen[table] {
			continue
		}
		seen[table] = true
		ordered = append(ordered, byTable[table])
		sort.Strings(children[table])
		for _, child := range children[table] {
			indegree[child]--
			if indegree[child] == 0 {
				ready = append(ready, child)
				sort.Strings(ready)
			}
		}
	}
	if len(ordered) != len(byTable) {
		var cyclic []string
		for table := range byTable {
			if !seen[table] {
				cyclic = append(cyclic, table)
			}
		}
		sort.Strings(cyclic)
		log.Printf("[WARN] cyclic table dependencies detected: %v; foreign keys will be added in the final phase", cyclic)
		for _, table := range cyclic {
			ordered = append(ordered, byTable[table])
		}
	}
	return ordered
}
