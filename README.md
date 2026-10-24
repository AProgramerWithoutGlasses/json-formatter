# JSON Studio

让 JSON，井井有条。H5 前端 + Go 后端 + MySQL 转换历史。CodeMirror 编辑器已本地打包，前端内嵌到单个可执行文件；运行时无需 Node.js 或外部 CDN。

## 功能

- 单编辑框，粘贴立即自动整理；普通输入停止 450ms 后整理，结果直接写回原框；语法错误保留原文并提示位置。
- 2 空格、4 空格、Tab 缩进；格式化和压缩；自动换行作用于当前编辑框。
- 支持编辑器键盘复制；大整数、数字写法、字段顺序和重复字段保持不变。
- 有效输入自动保存到 MySQL；历史搜索、恢复、删除和内容去重。
- 历史侧栏默认隐藏，点击左侧贴边箭头展开/收起；支持手机布局，无外部字体或脚本依赖。
- 点击“修复 JSON”手动修复缺失的逗号、冒号、引号、闭合括号，以及多余逗号、冒号、结束括号；支持单引号和未加引号的键名。补入/替换字符以绿色下划线标记，删除显示数量，支持撤销；粘贴或普通输入不会自动修复语法错误。

修复属于语法推断，请核对结果。缺失字段值、未知字面量等不能可靠修复的输入会保持原文并报错。修复不改变大整数、重复键或字段顺序。当前修复前原文暂存在页面内存供撤销；历史保存修复结果，不保留标记。继续手动编辑会清除旧标记，切换缩进或压缩保留标记。

每个非空对象 `{}`、数组 `[]` 的起始行左侧都有小三角，可分别收起/展开，支持嵌套；也可点击“全部收起 / 全部展开”。折叠只影响显示，键盘复制和保存历史仍保留完整内容，修复的绿色标记在展开后继续显示。

## Windows 一键本地启动

依赖：Go 1.25+，MySQL Server 8.0+（`mysqld.exe` 在 PATH）。当前已在 Go 1.25.10 / MySQL 9.7 上验证。

```powershell
./scripts/start-local.ps1
# 打开 http://127.0.0.1:8080
```

脚本生成随机凭据，在项目 `.local/mysql-data` 初始化独立 MySQL，监听 `127.0.0.1:13307`，创建数据库及受限应用账号，编译并启动 Go。不会修改系统 MySQL 服务。数据、凭据、日志、二进制均在 Git 忽略的 `.local/` 中。重复运行保留数据并重新构建网站。

```powershell
# 网站端口被占用时
./scripts/start-local.ps1 -Port 8081
# 停止本项目服务，保留数据库
./scripts/stop-local.ps1
```

独立 MySQL 端口初始化后固定，后续启动应使用相同 `-MySQLPort`。不要删除或提交 `.local/` 凭据。运行窗口隐藏；日志见 `.local/server-error.log`、`.local/mysql-error.log`。前端使用 `go:embed`，改动后也需要重启脚本。

## 使用已有 MySQL（Windows / Linux / macOS）

由数据库管理员执行，替换密码及允许的应用主机：

```sql
CREATE DATABASE json_formatter CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;
CREATE USER 'json_formatter'@'127.0.0.1' IDENTIFIED BY 'replace-with-a-strong-password';
GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, INDEX ON json_formatter.* TO 'json_formatter'@'127.0.0.1';
```

配置 MySQL 使用 UTC。Go 首次启动自动创建 `conversion_history` 表，不创建数据库本身。PowerShell：

```powershell
$env:MYSQL_DSN='json_formatter:replace-with-a-strong-password@tcp(127.0.0.1:3306)/json_formatter?charset=utf8mb4'
$env:ADDR='127.0.0.1:8080'
go run ./cmd/server
```

Linux / macOS：

```sh
export MYSQL_DSN='json_formatter:replace-with-a-strong-password@tcp(127.0.0.1:3306)/json_formatter?charset=utf8mb4'
export ADDR='127.0.0.1:8080'
go run ./cmd/server
```

`go build -o json-studio ./cmd/server` 构建独立二进制。`.env.example` 为参考模板；程序读取进程环境，不自动加载 `.env`。启动数据库连接失败会退出，不降级为内存或浏览器存储。

## API

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/health` | 检查真实 MySQL 连接 |
| POST | `/api/format` | 整理或压缩，可保存历史 |
| GET | `/api/history` | 当前浏览器最近 100 条记录摘要 |
| GET | `/api/history/{id}` | 恢复原文及选项 |
| DELETE | `/api/history/{id}` | 删除当前会话的一条记录 |

请求样例（Content-Type: application/json）：

```json
{"input":"{\"id\":9007199254740993}","indent":"2","mode":"format","save":true,"repair":true}
```

`indent` 支持 `2` / `4` / `tab`，默认 `2`；`mode` 支持 `format` / `compact`，默认 `format`。`save` 默认 `false`，前端真实输入时传 `true`。结果包含 `output`、`bytes`、`lines`、`saved`，保存失败时含 `warning`；错误含 `error`，语法错误另含 `line` / `column`。输入原文最多 1 MiB。

`repair` 在 API 默认 `false`，页面仅在点击“修复 JSON”时传 `true`，其余操作均传 `false`。发生修复时响应增加 `repair: {added, removed, ranges: [{start, end}]}`：范围为最终输出的 UTF-16 偏移，左闭右开；只标识补入的字符，替换表示删除旧字符再补入新字符。限制 256 个修复符号、128 层修复嵌套；有效 JSON 不受修复器层数限制。无法修复返回原语法错误（422），不会保存部分结果。

## 历史和隐私

历史在 MySQL，不依赖 localStorage。随机 HttpOnly Cookie 标识当前浏览器，不同会话无法读取或删除彼此记录。相同输入和选项重复整理只更新时间。只有有效 JSON 写入历史。

清除 Cookie 或切换浏览器后无法通过界面恢复旧会话。目前显示最近 100 条，数据库不自动清理更旧数据。此版本面向个人本地使用，没有账号登录；公开部署前应补充认证、访问配额和保留策略，并配置 HTTPS 及备份。自动保存会将输入原文传到运行服务的后端。

## 开发与验证

修改 `web/editor-source.js` 后先重新打包编辑器，再重启 Go：

```powershell
npm ci
npm run build:editor
./scripts/start-local.ps1
```

保留生成的 `web/editor-bundle.js` 和 `web/editor-bundle.js.LEGAL.txt`，以便只安装 Go 和 MySQL 也能运行网站。

```powershell
gofmt -w cmd internal web/embed.go
go test ./...
go vet ./...
node --check web/app.js
git diff --check
```

浏览器/API 验证需要先启动网站：

```powershell
npm install
npx playwright install chromium
npm run test:e2e
```

测试使用独立会话，覆盖真实 MySQL 历史读写、隔离、去重、异常输入、自动整理、缩进、压缩、换行、键盘复制、XSS、搜索、删除及 320–1440px 布局。截图输出 `.local/screenshots/`；`TEST_URL` 可指定其他本地测试地址。

维护入口：[agent.md](agent.md)，代理自动读取入口：[AGENTS.md](AGENTS.md)。
