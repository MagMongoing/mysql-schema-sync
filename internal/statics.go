package internal

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"html"
	"log"
	"net"
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
	skipped     bool
	skipReason  error
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
	// A table can now have multiple statements even without
	// SingleSchemaChange (for example CREATE TABLE followed by deferred foreign
	// keys). Report the exact statement associated with this execution item.
	if len(sd.SQL) > 1 {
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
// L14: append idx to avoid collisions when distinct table names collapse to
// the same sanitized form (e.g. "a-b" vs "a_b").
func sanitizeAnchorID(s string, idx int) string {
	base := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, s)
	return fmt.Sprintf("%s_%d", base, idx)
}

func (s *statics) toHTML() string {
	cfg := s.Config
	// alterErrHTML formats a per-table error for HTML output: redact DSNs
	// (driver errors may embed credentials) THEN HTML-escape the result.
	alterErrHTML := func(err error) string {
		msg := err.Error()
		if cfg != nil {
			msg = RedactDSNs(msg, cfg.SourceDSN, cfg.DestDSN)
		}
		return html.EscapeString(msg)
	}
	code := "<h2>运行结果</h2>\n"
	if s.fatalErr != nil {
		msg := RedactDSNs(s.fatalErr.Error(), cfg.SourceDSN, cfg.DestDSN)
		code += "<div style=\"border:2px solid #c00;padding:10px;color:#c00\">" +
			"<b>任务失败：</b>" + html.EscapeString(msg) + "</div>\n"
	}
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
		anchorID := sanitizeAnchorID(tb.table, idx)
		code += "<td><a href='#detail_" + anchorID + "'>" + escapedTable + "</a></td>\n"
		code += "<td>"
		if s.Config.Sync {
			if tb.skipped {
				code += "<span style=\"color:#b36b00\">未执行：" + alterErrHTML(tb.skipReason) + "</span>"
			} else if tb.alterRet == nil {
				code += "<span style=\"color:green\">成功</span>"
			} else {
				code += "<span style=\"color:red\">失败：" + alterErrHTML(tb.alterRet) + "</span>"
			}
		} else {
			code += "未同步"
		}
		code += "</td>\n"
		code += "<td>" + tb.timer.usedSecond() + "</td>\n"
		code += "</tr>\n"
	}
	code += "</tbody></table>\n<h3>SQLs</h3>\n<pre>"
	for idx, tb := range s.tables {
		code += "<a name='detail_" + sanitizeAnchorID(tb.table, idx) + "'></a>"
		if tb.alter == nil {
			code += "<span style=\"color:red\">no alter data</span>\n\n"
			continue
		}
		code += html.EscapeString(tb.alter.String()) + "\n\n"
	}
	code += "</pre>\n\n"

	code += "<h3>详情</h3>\n"
	code += `<table class='tb_1'>
		<thead>
			<tr>
			<th width="40px">序号</th>
			<th width="80px">Table</th>
			<th>Schema 对比</th>
			<th>SQL / 执行结果</th>
			</tr>
		</thead><tbody>
		`
	for idx, tb := range s.tables {
		code += "<tr>"
		code += "<th>" + strconv.Itoa(idx+1) + "</th>\n"
		code += "<td>" + html.EscapeString(tb.table) + "<br/>"
		if s.Config.Sync {
			if tb.skipped {
				code += "<span style=\"color:#b36b00\">未执行：" + alterErrHTML(tb.skipReason) + "</span>"
			} else if tb.alterRet == nil {
				code += "<span style=\"color:green\">成功</span>"
			} else {
				code += "<span style=\"color:red\">失败：" + alterErrHTML(tb.alterRet) + "</span>"
			}
		} else {
			code += "未同步"
		}
		code += "</td>\n"
		code += "<td valign=top><b>数据源 Schema:</b><br/>"
		// Defensive: tb.alter / SchemaDiff / Source / Dest may theoretically be nil if
		// upstream code paths skip initialization. Guard each access so an unexpected nil
		// inside a deferred toHTML() call does not panic and mask the original fatal error.
		if tb.alter == nil || tb.alter.SchemaDiff == nil {
			code += "<span style=\"color:red\">no diff data</span></td>\n"
			code += "<td valign=top>n/a</td>\n"
			code += "</tr>\n"
			continue
		}
		if len(tb.alter.SchemaDiff.Source.SchemaRaw) == 0 {
			code += "<span style=\"color:red\">在源数据源不存在，在目标数据库存在</span>"
		} else {
			code += htmlPre(tb.alter.SchemaDiff.Source.SchemaRaw)
		}
		code += "<br/><b>目标 Schema:</b><br/>"
		if len(tb.alter.SchemaDiff.Dest.SchemaRaw) == 0 {
			code += "不存在"
		} else {
			code += htmlPre(tb.alter.SchemaDiff.Dest.SchemaRaw)
		}
		code += "</td>\n"

		code += "<td valign=top><b>请在目标库执行如下 SQL:</b><br/>"
		code += htmlPre(strings.Join(tb.alter.SQL, ";\n"))
		if s.Config.Sync {
			code += "<br/><b>执行后:</b><br/>" + htmlPre(tb.schemaAfter)
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
	if cfg.HTTPAddress != "" {
		defer startWebServer(cfg.HTTPAddress, cfg)
	}
	alterTotal := len(s.tables)
	if alterTotal < 1 {
		if s.fatalErr != nil {
			// Sanitize: strip credentials from the fatal error before persisting / mailing.
			rawMsg := s.fatalErr.Error()
			rawMsg = RedactDSNs(rawMsg, cfg.SourceDSN, cfg.DestDSN)
			// HTML-escape the error before writing it to disk: writeHTMLResult is
			// served by the HTTP report endpoint as text/html, so unescaped error
			// text containing attacker-controlled bytes (e.g. driver-echoed table
			// or column names from a hostile source DB) could otherwise execute
			// arbitrary script in the operator's browser.
			plainMsg := fmt.Sprintf("fatal error: %s", rawMsg)
			htmlMsg := "<pre>" + html.EscapeString(plainMsg) + "</pre>"
			writeHTMLResult(htmlMsg)
			log.Println("fatal error, skip send mail:", rawMsg)
			if cfg.Email != nil {
				cfg.SendMailFail(plainMsg)
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
	if s.fatalErr != nil {
		title += " [任务失败]"
	}

	hostName, _ := os.Hostname()
	if hostName == "" {
		hostName = "unknown"
	}
	body += "<h2>任务信息</h2>\n<pre>"
	body += " 数据源：" + html.EscapeString(dsnShort(cfg.SourceDSN)) + "\n"
	body += "   目标：" + html.EscapeString(dsnShort(cfg.DestDSN)) + "\n"
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
	body += "开始时间： " + formatTimeOrNA(s.timer.start) + "\n"
	body += "截止时间： " + formatTimeOrNA(s.timer.end) + "\n"
	body += "运行耗时： " + s.timer.usedSecond() + "\n"

	body += "</pre>\n"
	body += s.toHTML()

	writeHTMLResult(body)
	if cfg.Email != nil && cfg.Email.SendMailAble {
		cfg.Email.SendMail(title, body)
	}
}

// RedactDSNs replaces any occurrence of the configured DSNs (which may contain
// credentials) inside an arbitrary error message with a masked form. This
// prevents driver-formatted errors from leaking passwords into logs/email/HTML.
//
// M1/M3: In addition to full-DSN substring replacement, we also extract and
// redact just the password and username substrings independently. This catches
// cases where the driver emits only the password (e.g. "Access denied for
// user 'admin'@'host'") or URL-encodes it, bypassing the whole-DSN match.
//
// Exported so callers outside this package (e.g. main.go panic recovery) can
// apply the same redaction policy.
func RedactDSNs(msg string, dsns ...string) string {
	for _, dsn := range dsns {
		if dsn == "" {
			continue
		}
		// Full-DSN replacement (original behavior).
		if strings.Contains(msg, dsn) {
			msg = strings.ReplaceAll(msg, dsn, maskDSNPassword(dsn))
		}
		// M1/M3: also redact just the password substring. This catches
		// errors that echo only the password without the full DSN.
		pw := extractDSNPassword(dsn)
		if pw != "" {
			if len(pw) >= 10 {
				msg = strings.ReplaceAll(msg, pw, "***")
			} else {
				// M2: for short passwords, only redact if the password appears as
				// a standalone word (not as a substring of the DSN or other text).
				// This prevents corrupting diagnostic messages while still catching
				// driver errors that echo the password verbatim.
				searchFrom := 0
				for searchFrom <= len(msg)-len(pw) {
					relative := strings.Index(msg[searchFrom:], pw)
					if relative < 0 {
						break
					}
					idx := searchFrom + relative
					before := idx == 0 || !isAlphaNum(msg[idx-1])
					after := idx+len(pw) >= len(msg) || !isAlphaNum(msg[idx+len(pw)])
					if before && after {
						msg = msg[:idx] + "***" + msg[idx+len(pw):]
						searchFrom = idx + len("***")
					} else {
						searchFrom = idx + len(pw)
					}
				}
			}
		}
	}
	return msg
}

// extractDSNPassword extracts the password portion from a MySQL DSN.
// DSN format: user:password@tcp(host:port)/dbname
// Uses LastIndex for '@' (passwords may contain '@').
func extractDSNPassword(dsn string) string {
	atIdx := strings.LastIndex(dsn, "@")
	if atIdx < 0 {
		return ""
	}
	userPart := dsn[:atIdx]
	colonIdx := strings.Index(userPart, ":")
	if colonIdx < 0 {
		return ""
	}
	return userPart[colonIdx+1:]
}

// redactDSNs is kept as an unexported alias for in-package callers; new code
// should prefer the exported RedactDSNs.
func redactDSNs(msg string, dsns ...string) string {
	return RedactDSNs(msg, dsns...)
}

func startWebServer(addr string, cfg *Config) {
	// H3: refuse to bind to non-loopback addresses by default. The report
	// page contains raw schema SQL and may carry credentials in error messages.
	// Operators who genuinely need public access must set HTTPAllowPublic.
	safeAddr, addrErr := safeHTTPListenAddress(addr, httpAllowPublic)
	if addrErr != nil {
		log.Printf("[WARN] refusing HTTP report server address %q: %s", addr, addrErr)
		return
	}
	addr = safeAddr
	fp := filepath.Join(os.TempDir(), "mysql-schema-sync_last.html")
	if len(htmlResultPath) > 0 {
		fp = htmlResultPath
	}
	mux := http.NewServeMux()
	// H4: wrap all HTTP handlers with panic recovery that redacts DSN
	// credentials before logging, preventing leaks from handler panics.
	recoverHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if re := recover(); re != nil {
				panicMsg := fmt.Sprintf("%s", re)
				if cfg != nil {
					panicMsg = RedactDSNs(panicMsg, cfg.SourceDSN, cfg.DestDSN)
				}
				log.Printf("[HTTP] panic (redacted): %s", panicMsg)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		mux.ServeHTTP(w, r)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Use %q to Go-quote unsafe runes (\r, \n, ANSI escapes) and prevent log forging
		// from attacker-controlled URL path / RemoteAddr values.
		log.Printf("[HTTP] %s %q from %q", r.Method, r.URL.Path, r.RemoteAddr)
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
		Handler:           recoverHandler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Graceful shutdown on SIGINT/SIGTERM
	// M21: use a done channel so the signal goroutine exits if ListenAndServe
	// fails (preventing goroutine leak and signal stealing).
	done := make(chan struct{})
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		defer signal.Stop(sigCh)
		select {
		case sig := <-sigCh:
			log.Printf("[HTTP] received signal %s, shutting down...", sig)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := ser.Shutdown(ctx); err != nil {
				log.Printf("[HTTP] shutdown error: %s", err)
			}
		case <-done:
		}
	}()

	if listenErr := ser.ListenAndServe(); listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
		log.Printf("[HTTP] server error: %s", listenErr)
	}
	// L6: always close done so the signal goroutine exits after either
	// clean shutdown (ErrServerClosed) or listen failure.
	close(done)
	_ = ser.Close() // close idle keep-alive connections
}

func safeHTTPListenAddress(addr string, allowPublic bool) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("invalid listen address: %w", err)
	}
	if allowPublic {
		return addr, nil
	}
	if host == "" {
		return net.JoinHostPort("127.0.0.1", port), nil
	}
	ip := net.ParseIP(host)
	if host == "localhost" || (ip != nil && ip.IsLoopback()) {
		return addr, nil
	}
	return "", fmt.Errorf("non-loopback address requires -http-allow-public")
}

// isAlphaNum returns true if byte b is an ASCII alphanumeric character.
// Used by RedactDSNs for standalone-word boundary detection.
func isAlphaNum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// formatTimeOrNA formats a time.Time or returns "N/A" if zero.
// L29: prevents Y0001 timestamps in email body when timer hasn't stopped.
func formatTimeOrNA(t time.Time) string {
	if t.IsZero() {
		return "N/A"
	}
	return t.Format(timeFormatStd)
}

func writeHTMLResult(str string) {
	fp := filepath.Join(os.TempDir(), "mysql-schema-sync_last.html")
	if len(htmlResultPath) > 0 {
		fp = htmlResultPath
	}
	dir := filepath.Dir(fp)
	base := filepath.Base(fp)

	// M8: clean up stale .tmp files from prior crashes (older than 1 hour).
	// os.CreateTemp + Rename is atomic, so any leftover .tmp in the same
	// directory is a crash artifact.
	if matches, err := filepath.Glob(filepath.Join(dir, base+".*.tmp")); err == nil {
		for _, m := range matches {
			if info, statErr := os.Stat(m); statErr == nil && time.Since(info.ModTime()) > time.Hour {
				_ = os.Remove(m)
			}
		}
	}

	// Atomic write with symlink-attack defense:
	// Use os.CreateTemp to obtain a freshly-created, randomly-named file in the
	// SAME directory as the destination. This avoids a predictable ".tmp" suffix
	// in /tmp that an attacker could pre-create as a symlink to a victim file.
	tmpFile, err := os.CreateTemp(dir, base+".*.tmp")
	if err != nil {
		log.Println("html result create tmp:", err)
		return
	}
	tmpName := tmpFile.Name()
	// M9: cleanup logs the close error instead of swallowing it — on NFS,
	// deferred write errors surface only at Close.
	cleanup := func() {
		if cerr := tmpFile.Close(); cerr != nil {
			log.Println("html result cleanup close:", tmpName, cerr)
		}
		_ = os.Remove(tmpName)
	}
	// L4: os.CreateTemp already creates files with mode 0600 on POSIX.
	// The explicit Chmod is retained as defense-in-depth (e.g. non-standard umask).
	if err := tmpFile.Chmod(0600); err != nil {
		log.Println("html result chmod tmp:", tmpName, err)
		cleanup()
		return
	}
	if _, err := tmpFile.WriteString(str); err != nil {
		log.Println("html result write tmp:", tmpName, err)
		cleanup()
		return
	}
	if err := tmpFile.Close(); err != nil {
		log.Println("html result close tmp:", tmpName, err)
		_ = os.Remove(tmpName)
		return
	}
	if err := os.Rename(tmpName, fp); err != nil {
		log.Println("html result rename:", tmpName, "->", fp, err)
		_ = os.Remove(tmpName)
		return
	}
	log.Println("html result:", fp)
}

// RegisterFlags registers the -html and -http-allow-public flags with the
// default flag.CommandLine. Call this from main() (before flag.Parse) instead
// of relying on init() to keep flag surface scoped to the binary that imports
// this package.
func RegisterFlags() {
	flag.StringVar(&htmlResultPath, "html", "", "write HTML diff report to this file path (default: $TMPDIR/mysql-schema-sync_last.html)")
	flag.BoolVar(&httpAllowPublic, "http-allow-public", false, "allow -http to bind non-loopback addresses (WARNING: no authentication, schema details exposed)")
}

var htmlResultPath string
var httpAllowPublic bool
