# mysql-schema-sync

MySQL Schema 自动同步工具  

用于将 `线上` 数据库 Schema **变化**同步到 `本地测试环境`!
只同步 Schema、不同步数据。

支持功能：

1. 同步**新表**
2. 同步**字段** 变动：新增、修改
3. 同步**索引** 变动：新增、修改
4. 同步**字段顺序**：支持调整字段在表中的顺序
5. 支持**预览**（只对比不同步变动）
6. **邮件**通知变动结果
7. 支持屏蔽更新**表、字段、索引、外键**
8. 支持本地比线上额外多一些表、字段、索引、外键
9. 在该项目的基础上修复了比对过程中遇到分区表会终止后续操作的问题，支持分区表，对于分区表，会同步除了分区以外的变更。
10. 支持每条 ddl 只会执行单个的修改，目的兼容tidb ddl问题 Unsupported multi schema change，通过single_schema_change字段控制，默认关闭。

## 安装

```bash
go install github.com/hidu/mysql-schema-sync@master
```

## 配置

参考 默认配置文件  config.json 配置同步源、目的地址。  
修改邮件接收人  当运行失败或者有表结构变化的时候你可以收到邮件通知。  

默认情况不会对多出的**表、字段、索引、外键**删除。若需要删除**字段、索引、外键** 可以使用 `-drop` 参数。

默认情况不会同步字段顺序差异。若需要同步字段顺序，可以使用 `-field-order` 参数（注意：此操作可能需要重建表，影响性能）。

配置示例(config.json):  

```
cp config.json mydb_conf.json
```

```
{
      //source：同步源
      "source":"test:test@(127.0.0.1:3306)/test_0",
      //dest：待同步的数据库
      "dest":"test:test@(127.0.0.1:3306)/test_1",
      //alter_ignore： 同步时忽略的字段和索引
      "alter_ignore":{
        "tb1*":{
            "column":["aaa","a*"],
            "index":["aa"],
            "foreign":[]
        }
      },
      //  tables: table to check schema,default is all.eg :["order_*","goods"]
      "tables":[],
      //  tables_ignore: table to ignore check schema,default is Null :["order_*","goods"]
      "tables_ignore": [],
      // 每个 ALTER TABLE 是否只包含一个变更操作
      "single_schema_change": false,
      // 源库为 timestamp、目标库为 datetime 时是否保留目标库类型
      "skip_timestamp_to_datetime": false,
      //有变动或者失败时，邮件接收人
      "email":{
          "send_mail":false,
         "smtp_host":"smtp.163.com:25",
         "from":"xxx@163.com",
         "password":"xxx",
         "to":"xxx@163.com"
      }
}
```

### JSON 配置项说明

- `source`：源数据库 DSN。
- `dest`：目标数据库 DSN。
- `tables`：需要同步的表白名单；为空表示全部表，支持 `*` 通配符。
- `tables_ignore`：不参与同步的表，支持 `*` 通配符。
- `alter_ignore`：按表配置忽略的 `column`、`index` 和 `foreign`，表名及条目均支持 `*` 通配符。
- `email`：同步完成或失败时使用的邮件通知配置。
- `single_schema_change`：每条 `ALTER TABLE` 尽量只执行一个修改操作；索引替换等必须保持原子性的操作仍会放在同一条语句中。
- `skip_timestamp_to_datetime`：当源字段为 `timestamp`、目标字段为语义等价的 `datetime` 时，保留目标字段类型。

`sync`、`drop`、`field-order`、`http`、`html` 等运行行为通过命令行参数控制，不从 JSON 配置读取。命令行中明确传入的参数会覆盖配置文件对应值。

### 运行

### 直接运行

```shell
./mysql-schema-sync -conf mydb_conf.json -sync
```

### 预览并生成变更sql

```shell
./mysql-schema-sync -drop -conf mydb_conf.json 2>/dev/null >db_alter.sql

```

### 启动 HTTP 报告服务

```shell
# 监听本地 8080 端口（默认仅允许 127.0.0.1 访问）
./mysql-schema-sync -conf mydb_conf.json -http :8080

# 指定监听地址
./mysql-schema-sync -conf mydb_conf.json -http 127.0.0.1:9090

# 监听所有网卡（需要显式允许非回环地址，无鉴权，注意安全风险）
./mysql-schema-sync -conf mydb_conf.json -http 0.0.0.0:58888 -http-allow-public
```

启动后访问对应地址即可查看 schema diff 报告页面。程序持续运行，按 `Ctrl-C` 终止。

### 指定 HTML 报告输出路径

```shell
./mysql-schema-sync -conf mydb_conf.json -html ./schema-diff-report.html
```

不指定 `-html` 时，报告默认写入 `$TMPDIR/mysql-schema-sync_last.html`。

### 使用shell调度

```shell
bash check.sh
```

每个json文件配置一个目的数据库，check.sh脚本会依次运行每份配置。
log存储在当前的log目录中。

### 自动定时运行

添加crontab 任务

```shell
30 * * * *  cd /your/path/xxx/ && bash check.sh >/dev/null 2>&1
```

### 参数说明

```shell
mysql-schema-sync [参数]
```

说明：

```shell
mysql-schema-sync -help
```

```text
  -conf string
        JSON 配置文件路径（默认 ./mydb_conf.json）
  -debug
        输出详细调试日志
  -dest string
        目标数据库 DSN；与 -source 配合使用
  -drop
        删除仅存在于目标表中的字段、索引和外键；默认关闭
  -field-order
        同步字段顺序，可能触发表重建；默认关闭
  -html string
        HTML 结果文件路径；默认写入系统临时目录
  -http string
        启动结果报告服务，例如 :8080
  -http-allow-public
        允许 HTTP 服务监听非回环地址；默认仅允许本机访问
  -mail_to string
        覆盖配置文件中的 email.to；显式传入空值可清空收件人
  -single_schema_change
        每条 ALTER TABLE 尽量只包含一个修改操作
  -skip_timestamp_to_datetime
        源库 timestamp → 目标库 datetime 时跳过类型变更，保留目标库类型
  -source string
        源数据库 DSN；非空时不读取 -conf，必须同时提供 -dest
  -sync
        执行结构同步；默认仅预览差异
  -tables string
        本次同步的表白名单，逗号分隔，例如 product_base,order_*
  -tables_ignore string
        本次忽略的表，逗号分隔，例如 temp_*,audit_log
```

只有命令行中明确传入的选项才会覆盖配置文件。例如，仅执行
`-conf mydb_conf.json -sync` 不会清空配置中的 `tables`、
`tables_ignore` 或 `single_schema_change`。

HTTP 报告默认只允许监听 `localhost`、`127.0.0.1` 或 `::1`。使用
`:8080` 时会自动绑定到 `127.0.0.1:8080`；如确需对外监听，必须同时
指定 `-http-allow-public`，并自行配置访问控制。
