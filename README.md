# Relay — Local Agent Adapter

Go 实现的本地能力服务与 Wails 桌面控制台。让远端 Agent 通过 REST 或 MCP 操作工作区文件、运行命令、截取并操作 macOS 桌面，并在本机查看完整执行日志。

![Relay control console](docs/relay-console.png)

## 0.5 自动更新

参考 Magpie 的生命周期：正式版本启动 5 秒后检查 GitHub Releases，此后每 6 小时检查一次；发现新版本就后台下载，在菜单栏和「连接 → 软件更新」显示进度、更新说明与「重启并更新」。正常退出也会安装已下载版本。下载期间服务继续运行，只有主动重启或退出才结束当前调用。

默认发布仓库为公开的 [jo32/readyrig](https://github.com/jo32/readyrig)。本机已执行 `gh auth login` 即可读取私有 Release（兼容 macOS Finder 启动时的 Homebrew 路径），也可设置 `RELAY_UPDATE_TOKEN`，使用该仓库 Contents 只读权限的令牌。凭据只发送到 GitHub API，不写入包、日志或控制台，也不转发给下载重定向地址。

- 下载匹配当前系统、架构和桌面/浏览器构建的资产，验证大小和 SHA-256；macOS `.app` 还验证代码签名完整性、签名团队和 bundle ID。签名版本不会降级为 ad-hoc 签名；当前 ad-hoc 包依赖认证的 GitHub 发布源与校验值确认来源，尚未做 Developer ID 公证。
- 下载失败最多重试 3 次；重复检查合并，已有完整下载不会因网络失败丢失。替换失败回滚旧文件；多个进程不能同时更新同一安装。
- `dev`、源码描述版本不自更新；安装目录只读或 Homebrew 管理时给出手动更新提示。不在后台请求管理员权限。
- 仅本地管理接口提供 `GET /api/update`、`POST /api/update/check`、`POST /api/update/restart`；Agent 接口无法访问。
- 重启后恢复原启动参数，权限开关按原启动参数初始化，Agent 随机访问路径重新生成。浏览器模式需要使用终端新输出的控制台链接重新登录。

```sh
bin/relay version                 # 当前版本
bin/relay update                  # 终端检查、下载并安装；运行中的同一安装请使用控制台
bin/relay --no-update              # 本次关闭；也可 RELAY_NO_UPDATE=1
make app VERSION=0.5.0             # 默认不带 VERSION 的构建标记为 dev
make release VERSION=0.5.0         # dist/releases/0.5.0/：安装包、裸二进制、SHA256SUMS
```

发布流程在 `.github/workflows/release.yml`：推送 `vX.Y.Z` 标签后，运行测试、构建 macOS Intel/Apple Silicon 桌面包以及 macOS/Linux/Windows 双架构浏览器版，最后发布到 GitHub Releases。本地 `make release` 只构建，不上传；可用 `SIGN_IDENTITY` 指定 macOS 签名身份。将来更换签名团队需要手动安装一次。

`--update-repo owner/repo` / `RELAY_UPDATE_REPO` 可切换仓库；`--update-feed URL` / `RELAY_UPDATE_FEED` 可覆盖为 Magpie 兼容的 `{version, notes, url, assets: {filename: {url, size, sha256}}}` feed。自定义 feed 只允许 HTTPS，回环地址允许 HTTP 供本地测试。`python3 scripts/test-update.py` 会在临时目录验证真实二进制从 0.4.0 升至 0.5.0 并重启，保留日志和工作区。

## 0.3 Magpie 界面

按 Magpie 的原始主题与组件重做：顶部居中分段导航、48px 工具行、相连统计单元、灰色底面、整块圆角列表，以及日志行内展开的输入/结果双栏。移除侧栏、宣传标题和独立详情面板，深浅色均采用 Magpie 的主题值。保留 Relay 名称与工具能力。

## 0.2 执行查看与回放

- **实时命令输出**：长命令执行期间每秒保存有变化的 stdout/stderr，控制台自动更新；不消耗远端 `write_stdin` 的未读输出。
- **单次取消**：详情中的「停止这次调用」仅取消所选任务及其进程组；保留已经产生的输出，最终状态为 `cancelled`。暂停仍然停止所有任务。
- **可读详情**：终端输出、文件内容、目录表格与搜索结果分别呈现，可复制完整结果；原始 JSON 可展开查看。窄窗口将行内详情变成上下排列，不会把历史记录放进执行表单。
- **动作回放**：将 `frame_id` 关联到同一会话中保存的参考截图，显示点击位置或拖拽起终点；可切换参考画面与执行之后，拖动进度并调整播放速度。未保存前后画面时明确显示缺失，不推测画面。
- **按需加载**：列表只获取摘要，选中后才加载完整内容。长输出不会随着每次列表刷新重复传输；详情保留展开状态与阅读位置。
- **能力隔离**：关闭某一能力只取消这一类别的在途调用，其他类别继续执行。

仅本地管理界面新增 `GET /api/calls/{id}` 和 `POST /api/calls/{id}/cancel`。远端 Agent 无法访问管理路由；如需终止自己的命令，仍使用 `write_stdin`。

## 0.4 Chrome DevTools MCP

本地安装 Google Chrome 且已开启远程调试时，Relay 会自动启动并桥接**官方 Chrome DevTools MCP**，将其工具加入现有 REST、MCP、OpenAPI 和工具库。主程序与桥接器使用 Go；官方 MCP 子进程依赖 Node.js。

1. Chrome 144+ 中打开 `chrome://inspect/#remote-debugging`，启用远程调试并保持 Chrome 运行。
2. 安装 Node.js（20.19+、22.12+ 或更新版本，包含 npx）。Relay 优先使用 PATH 中已安装的 `chrome-devtools-mcp`，否则使用 `npx --yes chrome-devtools-mcp@1.10.1`。首次接入需要联网下载并缓存官方组件；Finder 启动也会查找 Homebrew、Volta、mise 和 nvm 的常用路径。
3. 打开 Relay 的「连接」或「工具库」，确认 Chrome 状态为「已接入」。首次工具调用触发 Chrome 的连接授权时，在 Chrome 中允许。
4. Agent 使用本次运行的 Relay MCP 地址，重新获取 `tools/list` 即可使用 `chrome_list_pages`、`chrome_take_snapshot`、`chrome_click` 等工具。按上游 schema 提供参数，例如页面工具需要 `pageId`。本版网关没有工具变更推送；缓存工具清单的客户端需刷新或重新连接。

默认自动检测稳定版 Chrome 的 `DevToolsActivePort`，也检查本机 `127.0.0.1:9222`。每 5 秒检测一次；调试入口失效或关闭后撤下工具，恢复后重新注册。只连接已存在的 Chrome，不替用户启动浏览器或开启调试。状态「已接入」表示 MCP 工具已注册，首次浏览器操作仍可能等待 Chrome 授权。

使用自定义调试端口、Chrome 数据目录或已安装的 MCP 程序：

```sh
bin/relay-web web --chrome-browser-url http://127.0.0.1:9223
bin/relay-web web --chrome-user-data-dir /absolute/path/to/chrome-profile
bin/relay-web web --chrome-mcp-command /absolute/path/to/chrome-devtools-mcp
```

仅接受本机 HTTP 调试地址，不跟随重定向或接受远程 WebSocket。数据目录检测只读取调试入口文件，不读取浏览历史或账户资料。如果 macOS 拒绝访问该文件，界面会显示原因；可以使用已配置的本机调试端口。`--no-chrome` 可在启动时关闭自动接入；本地「Chrome 浏览器」开关可以临时停用，远端无法修改。

所有浏览器调用记录参数、耗时、结果和失败状态，可在「浏览器」类别筛选。原始 MCP 的 schema、annotations、文本、图片及 structuredContent 保留；浏览器截图在调用详情中显示并随 JSON 日志保存，暂不进入桌面回放与桌面快照计数。调用按顺序执行，超时上限 2 分钟；暂停或取消会断开当前 MCP 子进程，下次恢复后重新接入，不会关闭用户 Chrome，也不会重试已经发出的动作。已完成的浏览器操作不能撤销。

Chrome MCP 可以访问其连接的浏览器资料，并可能通过上传/保存工具访问当前用户可访问的本机文件；它不受 Relay 文件工具的工作区边界限制。默认关闭官方使用统计与 CrUX 数据查询。桥接复用的是官方 MCP 的工具与协议；其他客户端已经启动的 stdio 进程无法被 Relay 直接共享，Relay 管理自己的 MCP 子进程。

参考：[官方现有 Chrome 连接说明](https://github.com/ChromeDevTools/chrome-devtools-mcp/blob/main/docs/advanced-usage.md#connecting-to-a-running-chrome-instance)。

## 启动

需要 Go 1.25+。macOS 桌面构建需要 Xcode Command Line Tools；核心程序无需 Node、npm 或前端打包器；可选 Chrome MCP 功能需要 Node.js。

```sh
make app
open dist/Relay.app
```

也可以直接运行并指定工作区：

```sh
go run ./cmd/adapter --workspace /absolute/path/to/workspace
```

浏览器控制台（保留相同 Go 后端）：

```sh
make cli
bin/relay-web web --workspace /absolute/path/to/workspace
```

用终端输出的 `Dashboard` 链接打开浏览器。链接中的一次启动密钥在登录后换成 HttpOnly / SameSite=Strict cookie，地址栏会移除密钥。Agent 接口使用独立的随机路径授权，无需 Authorization 请求头。

默认目录：

- 工作区：`~/agent_workspace`
- 数据：`~/.local/share/relay/`，包括 SQLite 日志、截图和应用数据
- Agent 接口：`http://127.0.0.1:7332/<8位随机路径>`，实际地址在启动输出和连接界面中显示
- 浏览器控制台：`http://127.0.0.1:7331`（仅 `web` 模式）

数据目录必须位于工作区之外，保护日志、截图与应用配置。

## 使用界面

- **活动日志**：实时状态、按会话/类别/结果筛选、搜索参数或错误、查看请求与响应、分页和 NDJSON 导出。
- **桌面回放**：查看截图时间线、逐帧或自动播放。只展示历史图像，不重新执行操作；最多加载最近 5000 条桌面调用中的截图与动作。
- **工具库**：查看真实注册的工具、JSON Schema、默认示例；测试调用会产生实际副作用并写入日志。
- **连接与权限**：复制本次运行的 Agent 地址与 MCP 配置、查看系统授权状态、启用或关闭能力。
- **暂停控制**：取消当前调用和终端进程组，拒绝新的工具调用。菜单栏也有暂停/恢复入口。关闭窗口保留托盘；退出菜单终止服务。

默认开启文件工具与 Chrome 自动检测（只有发现调试入口后才开放浏览器工具）。终端和桌面操作每次启动都需要在本地开启，也可通过 `--allow-shell`、`--allow-computer` 显式开启。远端接口不提供恢复暂停或修改权限的 API。

## 工具清单

同一注册表供 REST、MCP、OpenAPI 和 UI 使用。

| 工具 | 用途 | 执行方式 |
| --- | --- | --- |
| `help` | 查询当前开放的工具、完整定义与状态，支持按名称查看 | 可并发，暂停时可用 |
| `read_file` | UTF-8 / Base64 读取、按行范围读取 | 可并发 |
| `write_file` | 原子写入，创建父目录 | 串行 |
| `list_directory` | 目录和文件元数据 | 可并发 |
| `search_files` | 文本查找，返回行号 | 可并发 |
| `exec_command` | 启动命令、指定 cwd/env/超时、提前返回进程 ID | 可并发 |
| `write_stdin` | 输入、轮询、关闭 stdin、终止进程 | 可并发 |
| `computer_screenshot` | 主屏幕 JPEG，最长边 1280 | 串行 |
| `computer_action` | 鼠标、键盘、滚动、拖拽，默认截取执行后画面 | 串行 |
| `chrome_*` | 动态发现的官方 Chrome DevTools MCP 工具 | 串行 |

### 工具帮助

Agent 可直接调用 `help`，无需另行读取 REST 文档：

```json
{}
```

默认返回当前允许调用的工具，包含名称、用途、分类、完整 `inputSchema`、上游提供的 `outputSchema` / annotations、是否修改数据、并发标志，以及 `enabled`、`available` 状态。定义直接读取当前注册表，Chrome 接入或断开会同步反映，不使用固定清单。

```json
{"name":"exec_command"}
```

指定名称可查看单个工具，即使它尚未启用。使用 `{"include_disabled":true}` 可列出全部已注册工具及不可用原因（`capability_disabled` / `control_paused`）。`available` 表示 Relay 当前允许调用，不代表系统权限或 Chrome 授权已经满足；已断开的 Chrome 工具不再注册，因此不会出现在清单中。

`help` 只读、始终启用，全局暂停时仍可查询，其调用也记录日志。REST 地址为 `POST /api/v1/tools/help`，MCP 调用名称为 `help`，本地工具库也可直接试用。

### REST

下列 `aSsxba11` 是示例，请使用连接界面复制的实际地址：

```sh
export RELAY_URL="http://127.0.0.1:7332/aSsxba11"
curl "$RELAY_URL/api/v1/tools"

curl "$RELAY_URL/api/v1/fs/read" \
  -H 'X-Session-ID: example-task' \
  -H 'X-Client-Name: My Agent' \
  -H 'Content-Type: application/json' \
  -d '{"path":"README.md","start_line":1,"end_line":20}'
```

以下路径均相对于包含随机前缀的 `RELAY_URL`。所有工具都可用 `POST /api/v1/tools/{name}` 调用，也提供方案中的别名：

```text
POST /api/v1/bash/exec
POST /api/v1/bash/stdin
POST /api/v1/fs/read
POST /api/v1/fs/write
POST /api/v1/fs/list
POST /api/v1/fs/search
POST /api/v1/computer/screenshot
POST /api/v1/computer/action
GET  /api/v1/openapi.json
```

结果统一为 `{call_id, status, result, error}`。工具执行失败返回 HTTP 422；本地暂停或能力关闭返回 423。命令非零退出保留 stdout/stderr/exit_code，并标记失败。缺失、错误或上次运行的随机前缀返回 404，浏览器跨域来源 403，速率超过每分钟 240 请求返回 429。

### MCP

每次启动使用系统安全随机数生成新的 8 位大小写字母/数字路径，所有 Agent 路由均位于该前缀下。无需 Bearer Token，不再创建或读取 `agent-token`；旧文件保留但不参与授权。重启后旧地址失效，需要重新复制配置。控制台可复制完整配置（下面的路径仅为示例）：

```json
{
  "mcpServers": {
    "relay": {
      "url": "http://127.0.0.1:7332/aSsxba11/mcp"
    }
  }
}
```

使用 HTTP POST JSON-RPC；`initialize` 返回 `Mcp-Session-Id`，后续请求需携带该头。支持 `initialize`、`ping`、`tools/list`、`tools/call` 和初始化通知。支持 2025-06-18 协议，兼容 2025-03-26 的单请求 JSON 传输；不实现旧式 SSE、JSON-RPC 批处理、MCP stdio 或服务端主动请求。MCP 图片作为独立 image content 返回，而不是塞入文本。MCP 客户端断线行为见下方限制。

### Computer Use 坐标

1. 在同一会话中调用 `computer_screenshot`。
2. 从返回值获得 `frame_id` 和 `image_size`。
3. 在返回图像的像素坐标系中选择坐标，并传回 `frame_id`。
4. 适配器映射到 macOS 的显示坐标点，处理 Retina 比例。

```json
{
  "action": "left_click",
  "frame_id": "<previous-frame-id>",
  "coordinate": [640, 360],
  "capture_after": true
}
```

支持 `mouse_move`、`left_click`、`right_click`、`middle_click`、`double_click`、`drag`、`scroll`、`type`、`key`。拖拽额外提供 `to: [x,y]`；滚动提供 `scroll_delta: [horizontal,vertical]`；组合键使用 `keys: ["cmd","c"]`。图像坐标必须在边界内；frame 必须属于当前会话且在五分钟内。屏幕布局改变后应重新截图。动作后等待 200ms 再观察。

## 安全边界与实际限制

- **文件边界是真实的目录边界**：用 Go `os.Root` 执行受限文件操作，拒绝绝对路径、父级穿越与指向外部的符号链接；不是简单字符串前缀判断。
- **终端不是系统沙箱**：在宿主机上用当前用户执行 `/bin/sh`。cwd 必须位于工作区内，但命令仍能访问用户可访问的其他路径、网络和设备。暂未提供 Docker 执行后端、PTY 或 Windows shell 适配。默认关闭终端。
- 命令默认超时 60 秒，上限 600 秒；输出每个流最多 1MiB；进程组会在取消时停止，但刻意脱离进程组的程序需要 OS 沙箱才能约束。
- **macOS 原生桌面**：需要屏幕录制及辅助功能权限。使用系统截图工具和 CoreGraphics 输入事件；当前只操作主屏幕、使用真实鼠标，暂无 AX 元素树、后台窗口输入、OCR、浏览器扩展或多显示器选择。Windows/Linux 的原生 Driver 会明确返回“不支持”。
- 鼠标位于屏幕角落时输入被拒绝；拖拽会持续检查取消/角落条件并释放按键。全局暂停不是撤销已完成的动作。
- Agent 网关与管理界面是两个独立入口。随机路径使用常量时间比较；拒绝 Agent 网关上的浏览器 Origin；控制台防跨站请求及 DNS rebinding。可用 `--allow-ip 127.0.0.1/32,::1/128` 限制直连 peer；不信任客户端提供的转发 IP。
- 随机访问路径仅保存在内存中，数据目录以 0700 创建；日志会移除字段名中的 token/password/secret、本次随机访问路径及控制台密钥。**不保证识别自由文本或图像中的所有秘密**；日志、截图应视作敏感本地数据。
- 日志默认持久保留，无自动清理/配额。SQLite 记录每次调用开始和结果；服务重启把未完成调用标为 interrupted。长命令运行时持续保存变化的输出，完成时无需客户端轮询也会更新最终日志。截图单独存储，不把 Base64 填入日志列表。
- 持有本次随机地址的 Agent 共享同一授权主体；会话用于关联日志，不是多租户身份隔离。没有自动重试写入/点击，避免重复副作用。
- MCP 当前采用有限子集，不宣称完整协议认证；网络断线可能取消当前请求，重连前应检查日志，不能直接重放未知结果的写入/点击。还未与真实云端 Agent 或公网隧道做联调。
- 桌面包是本地 ad-hoc 签名构建，已支持自动更新，未做 Developer ID 签名、公证或安装器。系统授权与构建身份有关；替换构建后可能需重新授权。

## 远端接入

仅转发 Agent 端口，控制台保持本地：

```sh
cloudflared tunnel --url http://127.0.0.1:7332
```

该命令是部署示例，不会由 Relay 自动执行或安装。实际部署应使用命名隧道、HTTPS、Cloudflare Access 或等价私网访问控制，在云端 Agent 配置完整的随机路径 URL，例如 `https://relay.example.com/aSsxba11/mcp`。隧道目标只填写本机地址和端口，客户端 URL 保留随机前缀。不要把管理控制台端口转发出去。当前程序没有内置隧道管理、Access 登录 UI；随机访问路径在每次重启时更换。

## 验证与项目结构

```sh
make test                       # Go race tests
node --check internal/server/assets/app.js
make build                      # native Wails application
make cli                        # browser/headless binary
```

```text
cmd/adapter/          启动参数、两个 HTTP 入口、应用生命周期
internal/harness/    tool spec、注册与调度、暂停、文件和终端执行器
internal/chromemcp/  Chrome 检测、官方 MCP stdio 桥接、动态工具与生命周期
internal/computer/   可替换 Driver、截图压缩、坐标映射、macOS 输入
internal/store/      SQLite 日志、过滤、会话、崩溃恢复
internal/server/     REST、MCP、认证、OpenAPI、事件流、静态界面
internal/desktop/    Wails 窗口与菜单栏
internal/update/     GitHub 私有发布认证、后台下载、校验与退出安装
internal/buildinfo/  构建版本与发布仓库
scripts/             macOS .app 打包
```

测试覆盖：文件穿越/符号链接逃逸、读写/查找、参数验证、日志脱敏、暂停及排队取消、stdin/异步进程、超时/进程组终止、输出上限、后台退出自动审计、MCP 初始化/调用、随机路径授权/重启轮换/Origin/CSRF/DNS rebinding、日志重启恢复、Retina 坐标、过期/跨会话 frame、单次取消、能力关闭互不干扰、实时日志不消耗远端输出、摘要与完整详情、参考帧的会话隔离。Chrome 桥接测试覆盖真实子进程协议、分页发现、复杂 schema、图片/结构化结果保留、上游失败、取消重连、停用与本机地址检查。测试不自动操作用户桌面。

## 设计参考

- [Codex](https://github.com/openai/codex)：工具规格、注册表/执行器分离、统一 dispatch 生命周期，以及 `exec_command` / `write_stdin` 的渐进输出方式。参照 main 浅克隆（depth=1、稀疏检出），提交 `94d642d8b40e45e2e544770f0d1f28df9a717f06`。
- [Magpie](https://github.com/yetone/magpie)：Go + Wails v3 + 嵌入原生 HTML/CSS/JS、桌面/网页共用 HTTP handler、轻量信息密度和调用追踪。参照提交 `74834748b98daeb295bf78b38170967e426e0c59`。
- [现有产品调研](docs/product-research.md)：Cua Driver、Peekaboo、Munim、Gokin Studio、Go MCP server、Bytebot；明确可复用能力和未验证项。

执行后端独立实现。界面复用了 Magpie 的 MIT 授权主题变量、分段导航、统计和调用详情样式，版权声明见 `internal/server/assets/MAGPIE-LICENSE.txt`。自动更新流程参考 Magpie 提交 `575a8f5fe3bba6ca22f8ec0509eb3af88100aac0`，更新模块另保留 `internal/update/MAGPIE-LICENSE.txt`。后续引入第三方 Driver 时需分别核对其 API、许可与分发要求。
