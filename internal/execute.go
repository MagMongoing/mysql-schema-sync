//  Copyright(C) 2025 github.com/hidu  All Rights Reserved.
//  Author: hidu <duv123+git@gmail.com>
//  Date: 2025-10-21

package internal

import (
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/xanygo/anygo/cli/xcolor"
)

func Execute(cfg *Config) {
	scs := newStatics(cfg)
	defer func() {
		scs.timer.stop()
		scs.sendMailNotice(cfg)
	}()

	sc, err := NewSchemaSync(cfg)
	if err != nil {
		log.Printf("[FATAL] failed to initialize schema sync: %s", err)
		scs.fatalErr = err
		return // let deferred sendMailNotice and timer.run
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
		return // let deferred sendMailNotice and db close run
	}
	// log.Println("source db table total:", len(allTables))

	changedTables := make(map[string][]*TableAlterData)

	for _, table := range allTables {
		xcolor.Green("start checking table %q ...", table)
		if !cfg.CheckMatchTables(table) {
			xcolor.Cyan("table %q skipped by not match", table)
			continue
		}

		if cfg.CheckMatchIgnoreTables(table) {
			xcolor.Cyan("table %q skipped by ignore", table)
			continue
		}

		sd, err := sc.getAlterDataByTable(table, cfg)
		if err != nil {
			log.Printf("[ERROR] skip table %q: %s", table, err)
			continue
		}

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

	var countSuccess int
	var countFailed int
	// 先执行单个表的，再执行多表关联的
	for _, canRunTypePref := range []string{"single", "multi"} {
		// Sort group keys for deterministic execution order within each prefix
		groupKeys := make([]string, 0, len(changedTables))
		for typeName := range changedTables {
			if strings.HasPrefix(typeName, canRunTypePref) {
				groupKeys = append(groupKeys, typeName)
			}
		}
		sort.Strings(groupKeys)

		for _, typeName := range groupKeys {
			sds := changedTables[typeName]
			log.Println("runSyncType:", typeName)
			var sqls []string
			var sts []*tableStatics
			for _, sd := range sds {
				for index := range sd.SQL {
					sql := strings.TrimRight(sd.SQL[index], ";")
					sqls = append(sqls, sql)

					st := scs.newTableStatics(sd.Table, sd, index)
					sts = append(sts, st)
				}
			}

			sql := strings.Join(sqls, ";\n") + ";"
			var ret error

			if sc.Config.Sync {
				ret = sc.SyncSQL4Dest(sql, sqls)
				sqlCount := len(sqls)
				if ret == nil {
					countSuccess += sqlCount
				} else {
					countFailed += sqlCount
				}
			}
			for _, st := range sts {
				st.alterRet = ret
				if sc.Config.Sync {
					var getErr error
					st.schemaAfter, getErr = sc.DestDb.GetTableSchema(st.table)
					if getErr != nil {
						log.Printf("[WARN] get schema after sync for %q failed: %s", st.table, getErr)
					}
				}
				st.timer.stop()
			}
		}
	}

	if sc.Config.Sync {
		log.Println("execute_all_sql_done, success_total:", countSuccess, "failed_total:", countFailed)
	}
}
