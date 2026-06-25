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

var configPath = flag.String("conf", "./mydb_conf.json", "json config file path")
var doSync = flag.Bool("sync", false, "sync schema changes to dest's db\non default, only show difference")
var drop = flag.Bool("drop", false, "drop fields,index,foreign key only on dest's table")
var fieldOrder = flag.Bool("field-order", false, "sync field order (may require table rebuild, affecting performance)")
var httpAddress = flag.String("http", "", "HTTP service address, eg. :8080")

var source = flag.String("source", "", "sync from, eg: test@(10.10.0.1:3306)/my_online_db_name\nwhen it is not empty,[-conf] while ignore")
var dest = flag.String("dest", "", "sync to, eg: test@(127.0.0.1:3306)/my_local_db_name")
var tables = flag.String("tables", "", "tables to sync\neg : product_base,order_*")
var tablesIgnore = flag.String("tables_ignore", "", "tables ignore sync\neg : product_base,order_*")
var mailTo = flag.String("mail_to", "", "overwrite config's email.to")
var singleSchemaChange = flag.Bool("single_schema_change", false, "single schema changes ddl command a single schema change")
var debug = flag.Bool("debug", false, "enable verbose debug logging")

func init() {
	log.SetFlags(log.Lshortfile | log.Ldate)
	df := flag.Usage
	flag.Usage = func() {
		df()
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "mysql schema sync tools "+internal.Version)
		fmt.Fprint(os.Stderr, internal.AppURL+"\n\n")
	}
}

func main() {
	flag.Parse()
	internal.SetDebug(*debug)
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
	cfg.Sync = *doSync
	cfg.Drop = *drop
	cfg.FieldOrder = *fieldOrder
	cfg.HTTPAddress = *httpAddress
	cfg.SingleSchemaChange = *singleSchemaChange

	if len(*mailTo) != 0 {
		if cfg.Email != nil {
			cfg.Email.To = *mailTo
		} else {
			log.Println("[WARN] -mail_to specified but no email configuration in config file; ignored")
		}
	}
	cfg.SetTables(strings.Split(*tables, ","))
	cfg.SetTablesIgnore(strings.Split(*tablesIgnore, ","))

	defer (func() {
		if re := recover(); re != nil {
			log.Println(re)
			bf := make([]byte, 4096)
			n := runtime.Stack(bf, false)
			cfg.SendMailFail(fmt.Sprintf("panic:%s\n trace=%s", re, bf[:n]))
			log.Fatalln("panic:", string(bf[:n]))
		}
	})()

	if err := cfg.Check(); err != nil {
		log.Fatalf("config error: %s", err)
	}
	internal.Execute(cfg)
}
