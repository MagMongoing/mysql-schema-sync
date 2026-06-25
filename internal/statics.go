package internal

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type statics struct {
	timer    *myTimer
	Config   *Config
	tables   []*tableStatics
	fatalErr error // set when a fatal error prevents any table processing
}

type tableStatics struct {
	timer       *myTimer
	table       string
	alter       *TableAlterData
	alterRet    error
	schemaAfter string
}

func newStatics(cfg *Config) *statics {
	return &statics{
		timer:  newMyTimer(),
		tables: make([]*tableStatics, 0),
		Config: cfg,
	}
}

func (s *statics) newTableStatics(table string, sd *TableAlterData, index int) *tableStatics {
	ts := &tableStatics{
		timer: newMyTimer(),
		table: table,
		alter: sd,
	}
	if sd.Type == alterTypeNo {
		return ts
	}
	if s.Config.SingleSchemaChange {
		sds := sd.Split()
		nts := &tableStatics{}
		*nts = *ts
		nts.alter = sds[index]
		s.tables = append(s.tables, nts)
		return nts // return the one stored in report, so caller can set alterRet/schemaAfter on it
	}
	s.tables = append(s.tables, ts)
	return ts
}

// sanitizeAnchorID replaces non-alphanumeric characters with underscores so the
// result is safe for use in both HTML href fragments and <a name> attributes,
// ensuring consistent anchor navigation regardless of browser entity decoding.
func sanitizeAnchorID(s string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, s)
}

func (s *statics) toHTML() string {
	code := "<h2>运行结果</h2>\n"
	code += "<h3>Tables</h3>\n"
	code += `<table class='tb_1'>
		<thead>
			<tr>
			<th width="60px">序号</th>
			<th>Table </th>
			<th>同步(alter) 结果</th>
			<th>耗时</th>
			</tr>
		</thead><tbody>
		`
	for idx, tb := range s.tables {
		code += "<tr>"
		code += "<td>" + strconv.Itoa(idx+1) + "</td>\n"
		escapedTable := html.EscapeString(tb.table)
		anchorID := sanitizeAnchorID(tb.table)
		code += "<td><a href='#detail_" + anchorID + "'>" + escapedTable + "</a></td>\n"
		code += "<td>"
		if s.Config.Sync {
			if tb.alterRet == nil {
				code += "<span style=\"color:green\">成功</span>"
			} else {
				code += "<span style=\"color:red\">失败：" + html.EscapeString(tb.alterRet.Error()) + "</span>"
			}
		} else {
			code += "未同步"
		}
		code += "</td>\n"
		code += "<td>" + tb.timer.usedSecond() + "</td>\n"
		code += "</tr>\n"
	}
	code += "</tbody></table>\n<h3>SQLs</h3>\n<pre>"
	for _, tb := range s.tables {
		code += "<a name='detail_" + sanitizeAnchorID(tb.table) + "'></a>"
		code += html.EscapeString(tb.alter.String()) + "\n\n"
	}
	code += "</pre>\n\n"

	code += "<h3>详情</h3>\n"
	code += `<table class='tb_1'>
		<thead>
			<tr>
			<th width="40px">序号</th>
			<th width="80px">Table</th>
			<th>&nbsp;</th>
			<th>&nbsp;</th>
			</tr>
		</thead><tbody>
		`
	for idx, tb := range s.tables {
		code += "<tr>"
		code += "<th rowspan=2>" + strconv.Itoa(idx+1) + "</th>\n"
		code += "<td rowspan=2>" + html.EscapeString(tb.table) + "<br/><br/>"
		if s.Config.Sync {
			if tb.alterRet == nil {
				code += "<span style=\"color:green\">成功</span>"
			} else {
				code += "<span style=\"color:red\">失败：" + html.EscapeString(tb.alterRet.Error()) + "</span>"
			}
		} else {
			code += "未同步"
		}
		code += "</td>\n"
		code += "<td valign=top><b>数据源 Schema:</b><br/>"
		if len(tb.alter.SchemaDiff.Source.SchemaRaw) == 0 {
			code += "<span style=\"color:red\">在源数据源不存在，在目标数据库存在</span>"
		} else {
			code += htmlPre(tb.alter.SchemaDiff.Source.SchemaRaw)
		}
		code += "</td>\n"

		code += "<td valign=top><b>目标 Schema:</b><br/>"
		if len(tb.alter.SchemaDiff.Dest.SchemaRaw) == 0 {
			code += "不存在"
		} else {
			code += htmlPre(tb.alter.SchemaDiff.Dest.SchemaRaw)
		}
		code += "</td>\n"
		code += "</tr>\n"

		code += "<tr>\n"
		code += "<td valign=top><b>请在目标库执行如下 SQL:</b><br/>"
		code += htmlPre(strings.Join(tb.alter.SQL, ","))
		code += "</td>\n"
		code += "<td valign=top>"
		if s.Config.Sync {
			code += "<b>执行后:</b><br/>" + htmlPre(tb.schemaAfter)
		}
		code += "</td>\n"
		code += "</tr>\n"
	}
	code += "</tbody></table>\n"
	return code
}

func (s *statics) alterFailedNum() int {
	n := 0
	for _, tb := range s.tables {
		if tb.alterRet != nil {
			n++
		}
	}
	return n
}

func (s *statics) sendMailNotice(cfg *Config) {
	alterTotal := len(s.tables)
	if alterTotal < 1 {
		if s.fatalErr != nil {
			errMsg := fmt.Sprintf("fatal error: %s", s.fatalErr)
			writeHTMLResult(errMsg)
			log.Println("fatal error, skip send mail:", s.fatalErr)
			if cfg.Email != nil {
				cfg.SendMailFail(errMsg)
			}
			return
		}
		writeHTMLResult("no table change")
		log.Println("no table change, skip send mail")
		return
	}
	title := "[mysql_schema_sync][" + dsnShort(cfg.DestDSN) + "]" + strconv.Itoa(alterTotal) + "张表发生变化"
	body := `
<style>
.tb_1,.tb_1 td,.tb_1 th{border: 1px solid;border-collapse: collapse;}
.tb_1 thead{ background-color: #e0e0e0;}
</style>`

	if !s.Config.Sync {
		title += "[preview]"
		body += "<span style=\"color:red\">所有 SQL 均未执行!</span>\n"
	}

	hostName, _ := os.Hostname()
	if hostName == "" {
		hostName = "unknown"
	}
	body += "<h2>任务信息</h2>\n<pre>"
	body += " 数据源：" + dsnShort(cfg.SourceDSN) + "\n"
	body += "   目标：" + dsnShort(cfg.DestDSN) + "\n"
	body += " 有变化：" + strconv.Itoa(len(s.tables)) + " 张表/条语句\n"
	body += "<span style=\"color:green\">是否同步：" + fmt.Sprintf("%t", s.Config.Sync) + "</span>\n"
	if s.Config.Sync {
		fn := s.alterFailedNum()
		body += "<span style=\"color:red\">失败数 : " + strconv.Itoa(fn) + "</span>\n"
		if fn > 0 {
			title += " [失败-" + strconv.Itoa(fn) + "]"
		}
	}
	body += "\n"
	body += "  主机名： " + html.EscapeString(hostName) + "\n"
	body += "开始时间： " + s.timer.start.Format(timeFormatStd) + "\n"
	body += "截止时间： " + s.timer.end.Format(timeFormatStd) + "\n"
	body += "运行耗时： " + s.timer.usedSecond() + "\n"

	body += "</pre>\n"
	body += s.toHTML()

	writeHTMLResult(body)
	if cfg.Email != nil {
		cfg.Email.SendMail(title, body)
	}
	if cfg.HTTPAddress != "" {
		startWebServer(cfg.HTTPAddress)
	}
}

func startWebServer(addr string) {
	fp := filepath.Join(os.TempDir(), "mysql-schema-sync_last.html")
	if len(htmlResultPath) > 0 {
		fp = htmlResultPath
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[HTTP] %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		bf, err := os.ReadFile(fp)
		if err != nil {
			http.NotFoundHandler().ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		_, _ = w.Write(bf)
	})
	log.Printf("[WARN] HTTP report server starting on %s — no authentication configured, schema details may be exposed", addr)
	log.Println("Press Ctrl-C to terminate the program")
	ser := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Graceful shutdown on SIGINT/SIGTERM
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		defer signal.Stop(sigCh)
		sig := <-sigCh
		log.Printf("[HTTP] received signal %s, shutting down...", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := ser.Shutdown(ctx); err != nil {
			log.Printf("[HTTP] shutdown error: %s", err)
		}
	}()

	if err := ser.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("[HTTP] server error: %s", err)
	}
}

func writeHTMLResult(str string) {
	fp := filepath.Join(os.TempDir(), "mysql-schema-sync_last.html")
	if len(htmlResultPath) > 0 {
		fp = htmlResultPath
	}
	err := os.WriteFile(fp, []byte(str), 0600)
	log.Println("html result:", fp, err)
}

func init() {
	flag.StringVar(&htmlResultPath, "html", "", "html result file path")
}

var htmlResultPath string
