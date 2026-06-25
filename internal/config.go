package internal

import (
	"encoding/json"
	"fmt"
	"html"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Config  config struct
type Config struct {
	// AlterIgnore 忽略配置， eg:   "tb1*":{"column":["aaa","a*"],"index":["aa"],"foreign":[]}
	AlterIgnore map[string]*AlterIgnoreTable `json:"alter_ignore"`

	// Email 完成同步后发送同步信息的邮件账号信息
	Email *EmailStruct `json:"email"`

	// SourceDSN 同步的源头
	SourceDSN string `json:"source"`

	// DestDSN 将被同步
	DestDSN string `json:"dest"`

	ConfigPath string

	// Tables 同步表的白名单，若为空，则同步全库
	Tables []string `json:"tables"`

	// TablesIgnore 不同步的表
	TablesIgnore []string `json:"tables_ignore"`

	// Sync 是否真正的执行同步操作
	Sync bool `json:"-"`

	// Drop 若目标数据库表比源头多了字段、索引，是否删除
	Drop bool `json:"-"`

	// FieldOrder 是否同步字段顺序（需要重建表，可能影响性能）
	FieldOrder bool `json:"-"`

	// HTTPAddress 生成站点报告的地址，如 :8080
	HTTPAddress string `json:"-"`

	// SingleSchemaChange 生成sql ddl语言每条命令只会进行单个修改操作
	SingleSchemaChange bool `json:"single_schema_change"`

	// SkipTimestampToDatetime 当源库字段为 timestamp、目标库为 datetime 时，跳过类型变更（不覆盖目标库的 datetime）
	SkipTimestampToDatetime bool `json:"skip_timestamp_to_datetime"`
}

func (cfg *Config) String() string {
	// Mask passwords to avoid credential leakage in logs
	masked := *cfg
	masked.SourceDSN = maskDSNPassword(masked.SourceDSN)
	masked.DestDSN = maskDSNPassword(masked.DestDSN)
	if masked.Email != nil {
		emailCopy := *masked.Email
		emailCopy.Password = "***"
		masked.Email = &emailCopy
	}
	// L3: log the MarshalIndent error instead of silently discarding it.
	ds, err := json.MarshalIndent(&masked, "  ", "  ")
	if err != nil {
		log.Printf("[WARN] Config.String() marshal failed: %s", err)
		return "<marshal error>"
	}
	return string(ds)
}

// maskDSNPassword replaces the password portion of a MySQL DSN with ***
// DSN format: user:password@tcp(host:port)/dbname
// Note: Uses LastIndex because go-sql-driver parses from the last '@',
// allowing passwords to contain '@' characters.
func maskDSNPassword(dsn string) string {
	atIdx := strings.LastIndex(dsn, "@")
	if atIdx < 0 {
		return dsn
	}
	userPart := dsn[:atIdx]
	rest := dsn[atIdx:]
	colonIdx := strings.Index(userPart, ":")
	if colonIdx < 0 {
		return dsn // no password
	}
	return userPart[:colonIdx] + ":***" + rest
}

// AlterIgnoreTable table's ignore info
type AlterIgnoreTable struct {
	Column []string `json:"column"`
	Index  []string `json:"index"`

	// 外键
	ForeignKey []string `json:"foreign"`
}

// IsIgnoreField isIgnore
func (cfg *Config) IsIgnoreField(table string, name string) bool {
	for tableName, dit := range cfg.AlterIgnore {
		if dit == nil {
			continue
		}
		if simpleMatch(tableName, table, "IsIgnoreField_table") {
			for _, col := range dit.Column {
				if simpleMatch(col, name, "IsIgnoreField_colum") {
					return true
				}
			}
		}
	}
	return false
}

// CheckMatchTables check table is match
func (cfg *Config) CheckMatchTables(name string) bool {
	// 若没有指定表，则意味对全库进行同步
	if len(cfg.Tables) == 0 {
		return true
	}
	for _, tableName := range cfg.Tables {
		if simpleMatch(tableName, name, "CheckMatchTables") {
			return true
		}
	}
	return false
}

func (cfg *Config) SetTables(tables []string) {
	for _, name := range tables {
		name = strings.TrimSpace(name)
		if len(name) > 0 {
			cfg.Tables = append(cfg.Tables, name)
		}
	}
}

// SetTablesIgnore 设置忽略
func (cfg *Config) SetTablesIgnore(tables []string) {
	for _, name := range tables {
		name = strings.TrimSpace(name)
		if len(name) > 0 {
			cfg.TablesIgnore = append(cfg.TablesIgnore, name)
		}
	}
}

// CheckMatchIgnoreTables check table_Ignore is match
func (cfg *Config) CheckMatchIgnoreTables(name string) bool {
	if len(cfg.TablesIgnore) == 0 {
		return false
	}
	for _, tableName := range cfg.TablesIgnore {
		if simpleMatch(tableName, name, "CheckMatchIgnoreTables") {
			return true
		}
	}
	return false
}

// Check validates the config and returns an error if invalid
func (cfg *Config) Check() error {
	if len(cfg.SourceDSN) == 0 {
		return fmt.Errorf("source DSN is empty")
	}
	if len(cfg.DestDSN) == 0 {
		return fmt.Errorf("dest DSN is empty")
	}
	return nil
}

// IsIgnoreIndex is index ignore
func (cfg *Config) IsIgnoreIndex(table string, name string) bool {
	for tableName, dit := range cfg.AlterIgnore {
		if dit == nil {
			continue
		}
		if simpleMatch(tableName, table, "IsIgnoreIndex_table") {
			for _, index := range dit.Index {
				if simpleMatch(index, name, "IsIgnoreIndex_name") {
					return true
				}
			}
		}
	}
	return false
}

// IsIgnoreForeignKey 检查外键是否忽略掉
func (cfg *Config) IsIgnoreForeignKey(table string, name string) bool {
	for tableName, dit := range cfg.AlterIgnore {
		if dit == nil {
			continue
		}
		if simpleMatch(tableName, table, "IsIgnoreForeignKey_table") {
			for _, foreignName := range dit.ForeignKey {
				if simpleMatch(foreignName, name, "IsIgnoreForeignKey_name") {
					return true
				}
			}
		}
	}
	return false
}

// SendMailFail send fail mail
func (cfg *Config) SendMailFail(errStr string) {
	if cfg.Email == nil {
		log.Println("email conf is empty,skip send mail")
		return
	}
	_host, _ := os.Hostname()
	if _host == "" {
		_host = "unknown"
	}
	title := "[mysql-schema-sync][" + _host + "]failed"
	body := "error:<font color=red>" + html.EscapeString(errStr) + "</font><br/>"
	body += "host:" + html.EscapeString(_host) + "<br/>"
	body += "config-file:" + html.EscapeString(cfg.ConfigPath) + "<br/>"
	body += "dest_dsn:" + html.EscapeString(maskDSNPassword(cfg.DestDSN)) + "<br/>"
	// L9: use filepath.Base to avoid leaking the operator's home directory
	// path in failure mail. The project directory name is usually sufficient.
	pwd, _ := os.Getwd()
	body += "pwd:" + html.EscapeString(filepath.Base(pwd)) + "<br/>"
	cfg.Email.SendMail(title, body)
}

// LoadConfig load config file
func LoadConfig(confPath string) (*Config, error) {
	var cfg *Config
	err := loadJSONFile(confPath, &cfg)
	if err != nil {
		return nil, fmt.Errorf("load json conf %q: %w", confPath, err)
	}
	if cfg == nil {
		return nil, fmt.Errorf("config file %q contains null or invalid JSON", confPath)
	}
	cfg.ConfigPath = confPath
	return cfg, nil
}
