package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"

	"github.com/hidu/mysql-schema-sync/internal"
)

var configPath = flag.String("conf", "./mydb_conf.json", "path to JSON config file (supports # and // comments)")
var doSync = flag.Bool("sync", false, "execute schema changes on destination database (default: dry-run, only show diff)")
var drop = flag.Bool("drop", false, "drop columns, indexes, and foreign keys that exist only in destination")
var fieldOrder = flag.Bool("field-order", false, "sync column order via MODIFY COLUMN (may require table rebuild)")
var httpAddress = flag.String("http", "", "HTTP report server listen address, e.g. :8080 (loopback-only unless -http-allow-public)")

var source = flag.String("source", "", "source DSN, e.g. user:pass@tcp(10.10.0.1:3306)/dbname (overrides -conf)")
var dest = flag.String("dest", "", "destination DSN, e.g. user:pass@tcp(127.0.0.1:3306)/dbname (required with -source)")
var tables = flag.String("tables", "", "comma-separated table whitelist with wildcard support, e.g. product_base,order_*")
var tablesIgnore = flag.String("tables_ignore", "", "comma-separated table ignore list with wildcard support, e.g. *_log,cache_*")
var mailTo = flag.String("mail_to", "", "override config email recipients (semicolon-separated)")
var singleSchemaChange = flag.Bool("single_schema_change", false, "emit one ALTER clause per statement instead of combining into a single ALTER TABLE")
var skipTimestampToDatetime = flag.Bool("skip_timestamp_to_datetime", false, "skip timestamp→datetime type conversion (preserve destination's datetime columns)")
var debug = flag.Bool("debug", false, "enable verbose debug logging (SQL text, timing, structured comparison details)")

func init() {
	log.SetFlags(log.Lshortfile | log.Ldate)
	internal.RegisterFlags()
	df := flag.Usage
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "mysql-schema-sync %s — MySQL schema diff & sync tool\n", internal.Version)
		fmt.Fprintf(os.Stderr, "%s\n\n", internal.AppURL)
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  mysql-schema-sync -conf config.json              # dry-run: show diff only")
		fmt.Fprintln(os.Stderr, "  mysql-schema-sync -conf config.json -sync        # execute changes")
		fmt.Fprintln(os.Stderr, "  mysql-schema-sync -source DSN -dest DSN -sync    # DSN mode (no config file)")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Flags:")
		df()
	}
}

func main() {
	flag.Parse()
	internal.SetDebug(*debug)
	visitedFlags := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) {
		visitedFlags[f.Name] = true
	})
	var cfg *internal.Config
	if len(*source) == 0 {
		var err error
		cfg, err = internal.LoadConfig(*configPath)
		if err != nil {
			log.Fatalf("config error: %s", err)
		}
	} else {
		cfg = new(internal.Config)
		cfg.SourceDSN = *source
		cfg.DestDSN = *dest
		if len(*dest) == 0 {
			log.Fatal("error: -source was specified but -dest is empty. Please provide a destination DSN.")
		}
	}
	applyCLIOverrides(cfg, visitedFlags)

	defer (func() {
		if re := recover(); re != nil {
			// H1 fix: build the redacted message FIRST, then log only the redacted form.
			// Previously log.Println(re) emitted the raw panic value (which may embed
			// DSN credentials) before RedactDSNs ran.
			panicMsg := fmt.Sprintf("%s", re)
			// H2 fix: use a growable stack buffer — runtime.Stack returns the number
			// of bytes written. If the trace exceeds cap we grow until it fits,
			// so no root-cause bytes are silently truncated.
			buf := make([]byte, 16384)
			for {
				n := runtime.Stack(buf, false)
				if n < len(buf) {
					buf = buf[:n]
					break
				}
				buf = make([]byte, len(buf)*2)
			}
			trace := string(buf)
			if cfg != nil {
				panicMsg = internal.RedactDSNs(panicMsg, cfg.SourceDSN, cfg.DestDSN)
				trace = internal.RedactDSNs(trace, cfg.SourceDSN, cfg.DestDSN)
			}
			body := fmt.Sprintf("panic:%s\n trace=%s", panicMsg, trace)
			if cfg != nil {
				cfg.SendMailFail(body)
			}
			log.Printf("panic:%s\n trace=%s", panicMsg, trace)
			os.Exit(1)
		}
	})()

	if err := cfg.Check(); err != nil {
		log.Fatalf("config error: %s", err)
	}
	if err := internal.Execute(cfg); err != nil {
		log.Printf("[FATAL] schema sync failed: %s", internal.RedactDSNs(err.Error(), cfg.SourceDSN, cfg.DestDSN))
		os.Exit(1)
	}
}

func applyCLIOverrides(cfg *internal.Config, visitedFlags map[string]bool) {
	if visitedFlags["sync"] {
		cfg.Sync = *doSync
	}
	if visitedFlags["drop"] {
		cfg.Drop = *drop
	}
	if visitedFlags["field-order"] {
		cfg.FieldOrder = *fieldOrder
	}
	if visitedFlags["http"] {
		cfg.HTTPAddress = *httpAddress
	}
	if visitedFlags["single_schema_change"] {
		cfg.SingleSchemaChange = *singleSchemaChange
	}
	if visitedFlags["skip_timestamp_to_datetime"] {
		cfg.SkipTimestampToDatetime = *skipTimestampToDatetime
	}

	if visitedFlags["mail_to"] {
		if cfg.Email != nil {
			cfg.Email.To = *mailTo
		} else {
			log.Println("[WARN] -mail_to specified but no email configuration in config file; ignored")
		}
	}
	if visitedFlags["tables"] {
		cfg.Tables = nil
		cfg.SetTables(strings.Split(*tables, ","))
	}
	if visitedFlags["tables_ignore"] {
		cfg.TablesIgnore = nil
		cfg.SetTablesIgnore(strings.Split(*tablesIgnore, ","))
	}
}
