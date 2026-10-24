# JSON Studio

H5 单编辑框 JSON 工具，Go 提供无状态格式化/修复，历史记录仅保存在使用者浏览器的 IndexedDB 中。无需 MySQL、账号或服务端历史数据库。编辑器已本地打包，运行时不依赖 Node.js 或 CDN。

## 功能

- 粘贴立即整理，普通输入停止 450ms 后整理。错误 JSON 保留原文，点击“修复 JSON”才尝试修复。
- 2 空格、4 空格、Tab 缩进，格式化/压缩切换，滑动开关控制自动换行。
- 大整数、数字写法、字段顺序和重复键保持不变，支持编辑器键盘复制。
- 对象、数组可逐段或全部收起/展开；占位符显示“⋯ N 行”。折叠只改变显示，不改变文档。
- 修复新增字符用绿色下划线标记，显示删除数量，支持撤销。无法推断的缺失值不擅自填入。
- 本地历史支持搜索、恢复、删除、去重和同浏览器多标签页同步。左侧贴边箭头控制历史侧栏，默认隐藏。
- 简洁圆角界面，支持 320px 手机布局；错误显示在编辑器底栏，右侧显示字符数。

## 本地启动

只需 Go 1.25+：

```powershell
./scripts/start-local.ps1
# 打开 http://127.0.0.1:8080
```

脚本在 `.local/` 中构建并后台运行 Go，不会启动或连接数据库。修改前端资源后也需要重启，因为资源通过 `go:embed` 嵌入二进制。

```powershell
./scripts/start-local.ps1 -Port 8081
./scripts/stop-local.ps1
```

停止或重启服务不会删除浏览器历史。日志位于 `.local/server-error.log`。浏览器按协议、主机和端口隔离数据：更换端口、`localhost` 与 `127.0.0.1` 互换，都会看到不同的本地历史。

也可在 Windows / Linux / macOS 上直接运行：

```sh
go run ./cmd/server
# 或构建后运行
go build -o json-studio ./cmd/server
```

可选环境变量 `ADDR` 默认为 `127.0.0.1:8080`。例如 PowerShell：

```powershell
$env:ADDR='127.0.0.1:8081'
go run ./cmd/server
```

`.env.example` 仅为参考；程序不自动加载 `.env`。不再需要 `MYSQL_DSN`。

## 历史存储与隔离

历史仅存在当前网站来源下的浏览器 IndexedDB 中，后端没有历史读写接口，不生成历史会话 Cookie，不保存请求正文或转换结果。

- 不同浏览器、设备、浏览器用户配置之间隔离；同一浏览器配置的同源标签页共享历史，并同步更新。
- 这不是账号隔离：多人共用同一浏览器配置时仍可看见同一份历史。公用电脑应使用独立浏览器配置或无痕窗口。
- 刷新、关闭并重新打开普通浏览器、重启 Go 服务、只清理 Cookie，均不删除 IndexedDB 历史。
- 清除网站数据、删除浏览器配置、浏览器自动回收存储会删除历史；无痕模式通常仅在当前无痕会话内保留。
- 保存最近 100 条，JSON 原文总计最多 10 MiB。超限时按更新时间自动淘汰最旧记录。浏览器实际占用还包括索引等元数据。
- 相同原文、缩进、模式去重并更新时间，跨标签页写入在 IndexedDB 事务中串行处理，避免覆盖或重复。大整数原文不做 JSON 数值解析。
- 存储被禁用或空间不足时，格式化仍可使用，页面明确提示“本地历史未保存”；不会悄悄改用服务器或内存保存。

**格式化/修复仍将当前输入发送到 Go 服务计算**，仅历史保存在浏览器；本版本不是完全离线的纯前端转换器。历史列表、搜索、读取与删除均不上传历史数据。历史恢复时会把选中的内容发送到格式化接口以重新整理。

修复后的有效内容作为历史保存，不保存修复标记；修复前原文暂存在页面内存供撤销。关闭页面后不能撤销上一次页面中的修复。

### 从旧 MySQL 版本升级

新版不会读取、导入、删除旧 MySQL 历史，也不会把共享数据库内容分发给浏览器。旧版 `.local/mysql-data` 和凭据文件保持原样，可按需单独备份处理。旧历史不在新版页面显示。若另有部署的旧数据库服务，可停止该工具专用的实例；不要停止其他项目共用的 MySQL。

## API

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/health` | 服务健康检查，返回 `status: ok, history: browser` |
| POST | `/api/format` | 无状态格式化、压缩或手动修复 |

`/api/history` 及其子路径已移除，返回 404。`save` 参数已移除，旧客户端携带该字段会收到 400，应刷新页面使用新版资源。

请求样例（Content-Type: application/json）：

```json
{"input":"{\"id\":9007199254740993}","indent":"2","mode":"format","repair":false}
```

`indent` 支持 `2` / `4` / `tab`，默认 `2`；`mode` 支持 `format` / `compact`，默认 `format`；`repair` 默认 false，页面只有“修复 JSON”按钮传 true。结果包含 `output`、`bytes`、`lines`；没有 `saved` 或历史会话信息。错误含 `error`，语法错误另含 `line` / `column`。原始输入上限 1 MiB。

发生修复时返回 `repair: {added, removed, ranges: [{start, end}]}`，范围为结果中的 UTF-16 偏移，左闭右开；最多修复 256 个符号、128 层修复嵌套。不能修复返回原语法错误（422），不保存任何部分结果。

## 开发与验证

修改 `web/editor-source.js` 后先重新打包，再重启 Go：

```powershell
npm ci
npm run build:editor
./scripts/start-local.ps1
```

保留生成的 `web/editor-bundle.js` 和 `web/editor-bundle.js.LEGAL.txt`；其他前端文件直接内嵌，无需构建。

```powershell
gofmt -w cmd internal web/embed.go
go test ./...
go vet ./...
node --check web/app.js
node --check web/history.js
git diff --check
```

启动网站后执行浏览器/API 回归：

```powershell
npm ci
npx playwright install chromium
npm run test:e2e
# 仅本地历史验证
npm run test:history
```

测试使用独立浏览器上下文和临时专属配置，不操作用户历史。覆盖格式化、修复、折叠、窄屏布局，以及浏览器重启持久化、配置隔离、跨标签页同步与并发去重、100 条/10 MiB 保留限制、存储禁用和空间不足。截图输出 `.local/screenshots/`；`TEST_URL` 可指定其他本地测试地址。

维护入口：[agent.md](agent.md)，代理入口：[AGENTS.md](AGENTS.md)。
