package internal

import (
	"strings"
	"testing"
)

func TestDDLExecutionPhase(t *testing.T) {
	tests := []struct {
		sql  string
		want int
	}{
		{"ALTER TABLE `child` DROP FOREIGN KEY `fk_parent`", 0},
		{"CREATE TABLE `parent` (`id` int)", 1},
		{"ALTER TABLE `child` ADD COLUMN `x` int", 1},
		{"ALTER TABLE `child` ADD CONSTRAINT `fk_parent` FOREIGN KEY (`parent_id`) REFERENCES `parent` (`id`)", 2},
		{"ALTER TABLE `child`\nCHANGE `note` `note` varchar(100) COMMENT 'DROP FOREIGN KEY `fk_parent`'", 1},
		{"ALTER TABLE `child`\nCHANGE `note` `note` varchar(100) COMMENT 'ADD CONSTRAINT `fk_parent` FOREIGN KEY'", 1},
	}
	for _, tt := range tests {
		if got := ddlExecutionPhase(tt.sql); got != tt.want {
			t.Errorf("ddlExecutionPhase(%q)=%d, want %d", tt.sql, got, tt.want)
		}
	}
}

func TestInboundImpactForAlter(t *testing.T) {
	tests := []struct {
		sql        string
		wantAll    bool
		wantColumn string
	}{
		{"ALTER TABLE `parent`\nADD COLUMN `note` varchar(20)", false, ""},
		{"ALTER TABLE `parent`\nADD INDEX `idx_note` (`note`)", false, ""},
		{"ALTER TABLE `parent`\nCHANGE `id` `id` bigint NOT NULL", false, "id"},
		{"ALTER TABLE `parent`\nMODIFY COLUMN `id` bigint NOT NULL", false, "id"},
		{"ALTER TABLE `parent`\nDROP COLUMN `id`", false, "id"},
		{"ALTER TABLE `parent`\nDROP PRIMARY KEY", true, ""},
		{"ALTER TABLE `parent`\nDROP INDEX `uq_code`", true, ""},
	}
	for _, tt := range tests {
		got := inboundImpactForAlter(tt.sql)
		if got.all != tt.wantAll || (tt.wantColumn != "" && !got.columns[tt.wantColumn]) {
			t.Errorf("inboundImpactForAlter(%q)=%+v, want all=%v column=%q", tt.sql, got, tt.wantAll, tt.wantColumn)
		}
	}
}

func TestUnrelatedParentColumnChangeDoesNotTouchInboundForeignKeys(t *testing.T) {
	parent := &TableAlterData{
		Table: "parent",
		Type:  alterTypeAlter,
		SQL:   []string{"ALTER TABLE `parent`\nMODIFY COLUMN `note` varchar(100);"},
		SchemaDiff: newSchemaDiff("parent",
			"CREATE TABLE `parent` (\n `id` int,\n `note` varchar(100),\n PRIMARY KEY (`id`)\n)",
			"CREATE TABLE `parent` (\n `id` int,\n `note` varchar(20),\n PRIMARY KEY (`id`)\n)"),
	}
	childSchema := "CREATE TABLE `child` (\n" +
		" `parent_id` int NOT NULL,\n" +
		" CONSTRAINT `fk_parent` FOREIGN KEY (`parent_id`) REFERENCES `parent` (`id`)\n)"
	child := &TableAlterData{
		Table:      "child",
		Type:       alterTypeNo,
		SchemaDiff: newSchemaDiff("child", childSchema, childSchema),
	}
	items, err := addInboundForeignKeyItems(buildExecutionItems([]*TableAlterData{parent}),
		[]*TableAlterData{parent}, map[string]*TableAlterData{"parent": parent, "child": child},
		&Config{Tables: []string{"parent"}})
	if err != nil {
		t.Fatalf("unrelated parent column change should not touch excluded child: %v", err)
	}
	if len(items) != 1 || items[0].temporaryFK {
		t.Fatalf("unrelated column change generated FK operations: %+v", items)
	}
}

func TestForeignKeyNameFromAlterSQL(t *testing.T) {
	tests := []struct {
		sql  string
		want string
		ok   bool
	}{
		{"ALTER TABLE `c` DROP FOREIGN KEY `fk_parent`", "fk_parent", true},
		{"ALTER TABLE `c` ADD CONSTRAINT `fk_parent` FOREIGN KEY (`parent_id`) REFERENCES `p` (`id`)", "fk_parent", true},
		{"ALTER TABLE `c` ADD INDEX `fk_parent` (`parent_id`)", "", false},
		// A column/index using the same identifier must not be mistaken for a
		// planned FK operation.
		{"ALTER TABLE `c` DROP INDEX `fk_parent`", "", false},
	}
	for _, tt := range tests {
		got, ok := foreignKeyNameFromAlterSQL(tt.sql)
		if got != tt.want || ok != tt.ok {
			t.Errorf("foreignKeyNameFromAlterSQL(%q)=(%q,%v), want (%q,%v)", tt.sql, got, ok, tt.want, tt.ok)
		}
	}
}

func TestForeignKeyNamesFromCombinedAlter(t *testing.T) {
	sql := "ALTER TABLE `child`\n" +
		"DROP FOREIGN KEY `fk_one`,\n" +
		"DROP FOREIGN KEY `fk_two`;"
	got := foreignKeyNamesFromAlterSQL(sql)
	if len(got) != 2 || got[0] != "fk_one" || got[1] != "fk_two" {
		t.Fatalf("combined FK names not fully parsed: %v", got)
	}

	addSQL := "ALTER TABLE `child`\n" +
		"ADD CONSTRAINT `fk_one` FOREIGN KEY (`a`) REFERENCES `p` (`id`),\n" +
		"ADD CONSTRAINT `chk_a` CHECK (`a` > 0),\n" +
		"ADD CONSTRAINT `fk_two` FOREIGN KEY (`b`) REFERENCES `p` (`id`);"
	got = foreignKeyNamesFromAlterSQL(addSQL)
	if len(got) != 2 || got[0] != "fk_one" || got[1] != "fk_two" {
		t.Fatalf("combined FK additions not fully parsed: %v", got)
	}

	commentSQL := "ALTER TABLE `child`\n" +
		"CHANGE `note` `note` varchar(100) COMMENT 'DROP FOREIGN KEY `fk_fake`';"
	if got := foreignKeyNamesFromAlterSQL(commentSQL); len(got) != 0 {
		t.Fatalf("comment text was mistaken for FK operation: %v", got)
	}
}

func TestExecuteReturnsInitializationError(t *testing.T) {
	cfg := &Config{
		SourceDSN: "not-a-valid-mysql-dsn",
		DestDSN:   "also-invalid",
	}
	if err := Execute(cfg); err == nil {
		t.Fatal("Execute should return connection/DSN initialization failures")
	}
}

func TestOrderTableAlters(t *testing.T) {
	makeAlter := func(table, schema string) *TableAlterData {
		return &TableAlterData{
			Table:      table,
			SchemaDiff: newSchemaDiff(table, schema, ""),
		}
	}
	parent := makeAlter("parent", "CREATE TABLE `parent` (\n `id` int,\n PRIMARY KEY (`id`)\n)")
	child := makeAlter("child", "CREATE TABLE `child` (\n `parent_id` int,\n CONSTRAINT `fk_parent` FOREIGN KEY (`parent_id`) REFERENCES `parent` (`id`)\n)")
	got := orderTableAlters([]*TableAlterData{child, parent})
	if len(got) != 2 || got[0].Table != "parent" || got[1].Table != "child" {
		t.Fatalf("unexpected dependency order: %v, %v", got[0].Table, got[1].Table)
	}
}

func TestOrderTableAltersCycleIsDeterministic(t *testing.T) {
	a := &TableAlterData{Table: "a", SchemaDiff: newSchemaDiff("a",
		"CREATE TABLE `a` (\n `b_id` int,\n CONSTRAINT `fk_b` FOREIGN KEY (`b_id`) REFERENCES `b` (`id`)\n)", "")}
	b := &TableAlterData{Table: "b", SchemaDiff: newSchemaDiff("b",
		"CREATE TABLE `b` (\n `a_id` int,\n CONSTRAINT `fk_a` FOREIGN KEY (`a_id`) REFERENCES `a` (`id`)\n)", "")}
	got := orderTableAlters([]*TableAlterData{b, a})
	if len(got) != 2 || got[0].Table != "a" || got[1].Table != "b" {
		t.Fatalf("cyclic order should be deterministic: %v, %v", got[0].Table, got[1].Table)
	}
}

func TestSafeHTTPListenAddress(t *testing.T) {
	tests := []struct {
		addr        string
		allowPublic bool
		want        string
		wantErr     bool
	}{
		{":8080", false, "127.0.0.1:8080", false},
		{"127.0.0.1:8080", false, "127.0.0.1:8080", false},
		{"[::1]:8080", false, "[::1]:8080", false},
		{"0.0.0.0:8080", false, "", true},
		{"0.0.0.0:8080", true, "0.0.0.0:8080", false},
	}
	for _, tt := range tests {
		got, err := safeHTTPListenAddress(tt.addr, tt.allowPublic)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("safeHTTPListenAddress(%q,%v)=(%q,%v), want (%q, err=%v)",
				tt.addr, tt.allowPublic, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestCreateTableForeignKeysAreDeferred(t *testing.T) {
	source := "CREATE TABLE `child` (\n" +
		"  `id` int NOT NULL,\n" +
		"  `parent_id` int NOT NULL,\n" +
		"  PRIMARY KEY (`id`),\n" +
		"  CONSTRAINT `fk_parent` FOREIGN KEY (`parent_id`) REFERENCES `parent` (`id`)\n" +
		") ENGINE=InnoDB"
	sc := &SchemaSync{Config: &Config{}}
	got := sc.getAlterDataBySchema("child", source, "", &Config{})
	if got.Type != alterTypeCreate || len(got.SQL) != 2 {
		t.Fatalf("expected CREATE plus deferred FK, got type=%s SQL=%v", got.Type, got.SQL)
	}
	if strings.Contains(strings.ToUpper(got.SQL[0]), "FOREIGN KEY") {
		t.Fatalf("CREATE still contains foreign key: %s", got.SQL[0])
	}
	if ddlExecutionPhase(got.SQL[1]) != 2 {
		t.Fatalf("foreign key was not deferred: %s", got.SQL[1])
	}
}

func TestInboundForeignKeyIsDetachedAndRestored(t *testing.T) {
	parentSource := "CREATE TABLE `parent` (\n  `id` bigint NOT NULL,\n  PRIMARY KEY (`id`)\n)"
	parentDest := "CREATE TABLE `parent` (\n  `id` int NOT NULL,\n  PRIMARY KEY (`id`)\n)"
	childSchema := "CREATE TABLE `child` (\n" +
		"  `id` int NOT NULL,\n" +
		"  `parent_id` int NOT NULL,\n" +
		"  PRIMARY KEY (`id`),\n" +
		"  CONSTRAINT `fk_parent` FOREIGN KEY (`parent_id`) REFERENCES `parent` (`id`)\n" +
		")"

	parent := (&SchemaSync{Config: &Config{}}).getAlterDataBySchema("parent", parentSource, parentDest, &Config{})
	child := &TableAlterData{
		Table:      "child",
		Type:       alterTypeNo,
		SchemaDiff: newSchemaDiff("child", childSchema, childSchema),
	}
	items := buildExecutionItems([]*TableAlterData{parent})
	items, err := addInboundForeignKeyItems(items, []*TableAlterData{parent}, map[string]*TableAlterData{
		"parent": parent,
		"child":  child,
	}, &Config{})
	if err != nil {
		t.Fatal(err)
	}
	sortExecutionItems(items)

	if len(items) != 3 {
		t.Fatalf("expected FK drop, parent alter, FK add; got %d items: %+v", len(items), items)
	}
	if items[0].phase != 0 || !strings.Contains(items[0].sql, "DROP FOREIGN KEY `fk_parent`") {
		t.Fatalf("first item should detach inbound FK: %+v", items[0])
	}
	if !items[0].temporaryFK || items[0].fkRestore {
		t.Fatalf("detach item missing compensation metadata: %+v", items[0])
	}
	if items[1].phase != 1 || items[1].sd.Table != "parent" {
		t.Fatalf("second item should alter parent: %+v", items[1])
	}
	if items[2].phase != 2 || !strings.Contains(items[2].sql, "ADD CONSTRAINT `fk_parent`") {
		t.Fatalf("last item should restore inbound FK: %+v", items[2])
	}
	if !items[2].temporaryFK || !items[2].fkRestore || items[2].fkKey != items[0].fkKey {
		t.Fatalf("restore item is not paired with detach item: drop=%+v add=%+v", items[0], items[2])
	}
}

func TestTemporaryInboundDetachPrecedesPlannedForeignKeyDrops(t *testing.T) {
	parent := &TableAlterData{
		Table: "parent",
		Type:  alterTypeAlter,
		SQL: []string{
			"ALTER TABLE `parent` DROP FOREIGN KEY `fk_own`;",
			"ALTER TABLE `parent` MODIFY COLUMN `id` bigint NOT NULL;",
		},
		SchemaDiff: newSchemaDiff("parent",
			"CREATE TABLE `parent` (\n `id` bigint NOT NULL,\n PRIMARY KEY (`id`)\n)",
			"CREATE TABLE `parent` (\n `id` int NOT NULL,\n CONSTRAINT `fk_own` FOREIGN KEY (`id`) REFERENCES `other` (`id`)\n)"),
	}
	childSchema := "CREATE TABLE `child` (\n" +
		"  `parent_id` int NOT NULL,\n" +
		"  CONSTRAINT `fk_parent` FOREIGN KEY (`parent_id`) REFERENCES `parent` (`id`)\n" +
		")"
	child := &TableAlterData{
		Table:      "child",
		Type:       alterTypeNo,
		SchemaDiff: newSchemaDiff("child", childSchema, childSchema),
	}
	items, err := addInboundForeignKeyItems(buildExecutionItems([]*TableAlterData{parent}),
		[]*TableAlterData{parent}, map[string]*TableAlterData{
			"parent": parent,
			"child":  child,
		}, &Config{})
	if err != nil {
		t.Fatal(err)
	}
	sortExecutionItems(items)
	if len(items) < 2 || !items[0].temporaryFK || items[0].fkRestore {
		t.Fatalf("temporary detach must precede planned destructive DDL: %+v", items)
	}
	if items[1].temporaryFK || !strings.Contains(items[1].sql, "DROP FOREIGN KEY `fk_own`") {
		t.Fatalf("planned FK drop should follow temporary preflight detach: %+v", items)
	}
}

func TestInboundForeignKeyNotSuppressedBySameNamedIndex(t *testing.T) {
	parent := &TableAlterData{
		Table: "parent",
		Type:  alterTypeAlter,
		SQL:   []string{"ALTER TABLE `parent` MODIFY COLUMN `id` bigint NOT NULL;"},
		SchemaDiff: newSchemaDiff("parent",
			"CREATE TABLE `parent` (\n `id` bigint NOT NULL,\n PRIMARY KEY (`id`)\n)",
			"CREATE TABLE `parent` (\n `id` int NOT NULL,\n PRIMARY KEY (`id`)\n)"),
	}
	childSchema := "CREATE TABLE `child` (\n" +
		"  `parent_id` int NOT NULL,\n" +
		"  KEY `fk_parent` (`parent_id`),\n" +
		"  CONSTRAINT `fk_parent` FOREIGN KEY (`parent_id`) REFERENCES `parent` (`id`)\n" +
		")"
	child := &TableAlterData{
		Table:      "child",
		Type:       alterTypeNo,
		SchemaDiff: newSchemaDiff("child", childSchema, childSchema),
	}
	items := buildExecutionItems([]*TableAlterData{parent})
	items = append(items, executionItem{
		phase: 1,
		sql:   "ALTER TABLE `child` DROP INDEX `fk_parent`",
		sd:    child,
	})
	items, err := addInboundForeignKeyItems(items, []*TableAlterData{parent}, map[string]*TableAlterData{
		"parent": parent,
		"child":  child,
	}, &Config{})
	if err != nil {
		t.Fatal(err)
	}
	var detachFound bool
	for _, item := range items {
		if item.temporaryFK && !item.fkRestore && strings.Contains(item.sql, "DROP FOREIGN KEY") {
			detachFound = true
		}
	}
	if !detachFound {
		t.Fatalf("same-named index incorrectly suppressed inbound FK detach: %+v", items)
	}
}

func TestInboundForeignKeyOnDestinationOnlyTableBlocksUnsafeParentChange(t *testing.T) {
	parent := &TableAlterData{
		Table: "parent",
		Type:  alterTypeAlter,
		SQL:   []string{"ALTER TABLE `parent`\nMODIFY COLUMN `id` bigint NOT NULL;"},
		SchemaDiff: newSchemaDiff("parent",
			"CREATE TABLE `parent` (\n `id` bigint NOT NULL,\n PRIMARY KEY (`id`)\n)",
			"CREATE TABLE `parent` (\n `id` int NOT NULL,\n PRIMARY KEY (`id`)\n)"),
	}
	childDest := "CREATE TABLE `local_child` (\n" +
		"  `parent_id` int NOT NULL,\n" +
		"  CONSTRAINT `fk_local_parent` FOREIGN KEY (`parent_id`) REFERENCES `parent` (`id`)\n" +
		")"
	child := &TableAlterData{
		Table:      "local_child",
		Type:       alterTypeDropTable,
		SchemaDiff: newSchemaDiff("local_child", "", childDest),
	}

	_, err := addInboundForeignKeyItems(buildExecutionItems([]*TableAlterData{parent}),
		[]*TableAlterData{parent}, map[string]*TableAlterData{
			"parent":      parent,
			"local_child": child,
		}, &Config{})
	if err == nil {
		t.Fatal("destination-only inbound FK should block an unsafe parent change")
	}
}

func TestDestinationOnlyForeignKeyBlocksUnsafeParentChangeWhenDropDisabled(t *testing.T) {
	parent := &TableAlterData{
		Table: "parent",
		Type:  alterTypeAlter,
		SQL:   []string{"ALTER TABLE `parent`\nMODIFY COLUMN `id` bigint NOT NULL;"},
		SchemaDiff: newSchemaDiff("parent",
			"CREATE TABLE `parent` (\n `id` bigint NOT NULL,\n PRIMARY KEY (`id`)\n)",
			"CREATE TABLE `parent` (\n `id` int NOT NULL,\n PRIMARY KEY (`id`)\n)"),
	}
	childSource := "CREATE TABLE `child` (\n  `parent_id` int NOT NULL\n)"
	childDest := "CREATE TABLE `child` (\n" +
		"  `parent_id` int NOT NULL,\n" +
		"  CONSTRAINT `fk_dest_only` FOREIGN KEY (`parent_id`) REFERENCES `parent` (`id`)\n" +
		")"
	child := &TableAlterData{
		Table:      "child",
		Type:       alterTypeNo,
		SchemaDiff: newSchemaDiff("child", childSource, childDest),
	}

	_, err := addInboundForeignKeyItems(buildExecutionItems([]*TableAlterData{parent}),
		[]*TableAlterData{parent}, map[string]*TableAlterData{
			"parent": parent,
			"child":  child,
		}, &Config{})
	if err == nil {
		t.Fatal("destination-only FK should block unsafe parent changes when Drop=false")
	}
}

func TestAddOnlyParentChangeDoesNotTouchInboundForeignKeys(t *testing.T) {
	parent := &TableAlterData{
		Table: "parent",
		Type:  alterTypeAlter,
		SQL:   []string{"ALTER TABLE `parent`\nADD COLUMN `note` varchar(20);"},
		SchemaDiff: newSchemaDiff("parent",
			"CREATE TABLE `parent` (\n `id` int,\n `note` varchar(20),\n PRIMARY KEY (`id`)\n)",
			"CREATE TABLE `parent` (\n `id` int,\n PRIMARY KEY (`id`)\n)"),
	}
	childSchema := "CREATE TABLE `child` (\n" +
		" `parent_id` int NOT NULL,\n" +
		" CONSTRAINT `fk_parent` FOREIGN KEY (`parent_id`) REFERENCES `parent` (`id`)\n)"
	child := &TableAlterData{
		Table:      "child",
		Type:       alterTypeNo,
		SchemaDiff: newSchemaDiff("child", childSchema, childSchema),
	}
	cfg := &Config{Tables: []string{"parent"}}
	items, err := addInboundForeignKeyItems(buildExecutionItems([]*TableAlterData{parent}),
		[]*TableAlterData{parent}, map[string]*TableAlterData{
			"parent": parent,
			"child":  child,
		}, cfg)
	if err != nil {
		t.Fatalf("add-only parent change should not be blocked by excluded child: %v", err)
	}
	if len(items) != 1 || items[0].temporaryFK {
		t.Fatalf("add-only parent change should not touch inbound FK: %+v", items)
	}
}

func TestInboundForeignKeyOnProtectedTableBlocksPlan(t *testing.T) {
	parent := &TableAlterData{
		Table: "parent",
		Type:  alterTypeAlter,
		SQL:   []string{"ALTER TABLE `parent` MODIFY COLUMN `id` bigint NOT NULL;"},
		SchemaDiff: newSchemaDiff("parent",
			"CREATE TABLE `parent` (\n `id` bigint NOT NULL,\n PRIMARY KEY (`id`)\n)",
			"CREATE TABLE `parent` (\n `id` int NOT NULL,\n PRIMARY KEY (`id`)\n)"),
	}
	childSchema := "CREATE TABLE `child` (\n" +
		" `parent_id` int NOT NULL,\n" +
		" CONSTRAINT `fk_parent` FOREIGN KEY (`parent_id`) REFERENCES `parent` (`id`)\n)"
	child := &TableAlterData{
		Table:      "child",
		Type:       alterTypeNo,
		SchemaDiff: newSchemaDiff("child", childSchema, childSchema),
	}
	schemas := map[string]*TableAlterData{"parent": parent, "child": child}

	tests := []struct {
		name string
		cfg  *Config
	}{
		{"whitelist excluded", &Config{Tables: []string{"parent"}}},
		{"table ignored", &Config{TablesIgnore: []string{"child"}}},
		{"foreign key ignored", &Config{AlterIgnore: map[string]*AlterIgnoreTable{
			"child": {ForeignKey: []string{"fk_parent"}},
		}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := addInboundForeignKeyItems(buildExecutionItems([]*TableAlterData{parent}),
				[]*TableAlterData{parent}, schemas, tt.cfg)
			if err == nil {
				t.Fatal("expected protected inbound foreign key to block the plan")
			}
		})
	}
}

func TestDropDependencyOrder(t *testing.T) {
	source := "CREATE TABLE `child` (\n" +
		"  `id` int NOT NULL,\n" +
		"  PRIMARY KEY (`id`)\n" +
		")"
	dest := "CREATE TABLE `child` (\n" +
		"  `id` int NOT NULL,\n" +
		"  `parent_id` int NOT NULL,\n" +
		"  PRIMARY KEY (`id`),\n" +
		"  KEY `idx_parent` (`parent_id`),\n" +
		"  CONSTRAINT `fk_parent` FOREIGN KEY (`parent_id`) REFERENCES `parent` (`id`)\n" +
		")"
	cfg := &Config{Drop: true, SingleSchemaChange: true}
	sc := &SchemaSync{Config: cfg}
	got := sc.getAlterDataBySchema("child", source, dest, cfg)
	if len(got.SQL) < 3 {
		t.Fatalf("expected FK/index/column drops, got %v", got.SQL)
	}
	var phases []int
	for _, sql := range got.SQL {
		phases = append(phases, ddlExecutionPhase(sql))
	}
	if phases[0] != 0 {
		t.Fatalf("foreign key drop must execute first, phases=%v SQL=%v", phases, got.SQL)
	}
	wantFragments := []string{"DROP FOREIGN KEY", "DROP INDEX", "DROP COLUMN"}
	for i, fragment := range wantFragments {
		if !strings.Contains(strings.ToUpper(got.SQL[i]), fragment) {
			t.Fatalf("statement %d should contain %q: %v", i, fragment, got.SQL)
		}
	}
}

func TestAlterTableNameHandlesNilPlan(t *testing.T) {
	if got := alterTableName(nil); got != "<nil>" {
		t.Fatalf("alterTableName(nil) = %q", got)
	}
	if got := alterTableName(&TableAlterData{Table: "users"}); got != "users" {
		t.Fatalf("alterTableName(users) = %q", got)
	}
}
