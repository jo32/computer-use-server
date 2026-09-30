# 现有产品与组件调研

调研日期：2026-09-30。依据官方仓库 README、官方文档与 GitHub API；没有安装运行这些候选项目。文档中的功能声明不等于已通过本机验收，“未确认”不等于“没有”。

## 结论

已有相当接近的产品和可复用组件。尤其 Cua Driver 已覆盖本机控制、外部 Agent 接入、权限归属，以及轨迹录制/渲染，不应再把“有截图日志”当作独有能力。Go + Wails 的近似完整产品也存在：Gokin Studio。

本项目仍有明确的组合目标：一个独立于具体模型和聊天客户端的 **Go 本地能力服务 + 轻量桌面控制台**，把 shell、工作区文件和 computer use 纳入统一的工具注册、远程接入、暂停和审计。

建议保持 Go 控制台和 harness 自有实现，把 computer use 作为可替换 Driver；第一版使用最小 macOS 驱动，后续优先评估接入 Cua Driver / Peekaboo / Munim，而不是持续自行补齐辅助功能树、后台窗口输入和浏览器桥接。

## 对比

| 项目 | 已确认能力 | 与本需求的差异 | 建议 |
| --- | --- | --- | --- |
| [Cua / Cua Driver](https://github.com/trycua/cua) | macOS/Windows/Linux 桌面和浏览器控制；CLI、MCP、Python/TypeScript SDK；桌面应用持有权限的托管模式；轨迹录制与渲染；另有 VM / 云桌面产品 | 不是 Go + Wails 控制台；默认 MCP 连接拓扑与本方案的远端 HTTPS 网关不同；仍需整合 shell/文件权限和统一日志 UI | **最优先评估的 computer use 后端**。不要把 Cua 仅当 VM 工具 |
| [Peekaboo](https://github.com/openclaw/Peekaboo) | macOS 原生自动化；截图、辅助功能、输入、窗口/菜单；CLI/MCP；签名 DMG 菜单栏应用，权限引导、视觉反馈和 agent sessions；结构化日志 | Swift；面向 macOS；本次未确认独立远端 REST/MCP 网关及跨终端/文件/桌面的审计控制台 | macOS 优先时非常值得复用或参考；它已有应用界面，不能描述成“只有 CLI” |
| [Munim Computer Use](https://github.com/munimtechnologies/munim-computer-use) | Swift/macOS + Rust/Windows/Linux；MCP；辅助功能树、元素 ID、后台输入、Agent 指针、Chrome 扩展、应用/网站规则；支持嵌入自己的应用 | 主要为 computer use 引擎；不是 Go；本次未确认独立截图历史审计 UI；后台输入保证依平台而异 | 重点参考后台操作和元素优先的工具设计 |
| [Gokin Studio](https://github.com/ginkida/gokin-studio) | Go + Wails v2 + React；统一工具注册；PTY、文件工具、MCP 连接；会话工具时间线；JSONL 崩溃恢复；审计日志；macOS/Windows 权限控制的 computer use | 定位为运行自身模型的 Agent IDE；README 中远端 MCP 主要描述连接外部服务，未确认作为被云端 Agent 控制的服务端 | **最接近技术栈的完整参考产品**；适合参考 Go harness 和活动记录，不宜未经验证直接改成远端适配器 |
| [go_computer_use_mcp_server](https://github.com/hightemp/go_computer_use_mcp_server) | Go + robotgo；鼠标、键盘、截图、窗口、进程；stdio/SSE MCP | 缺少已确认的桌面审计 UI；README 明示默认无沙箱，需要额外网络防护；README 声称 MIT，但 GitHub API 未识别许可证 | 轻量 Go 原型参考；复用源码前单独核实许可文件 |
| [go-mcp-computer-use](https://github.com/coff33ninja/go-mcp-computer-use) | Go、Windows、MCP；截图、鼠标、键盘、OCR/UIA、窗口；结构化日志/get_logs；录制和动作复制 | Windows 专用；工具面很大（README 当前标注 155 个）；不是轻量 macOS 控制台；动作复制不等同于日志截图回放 | 后续 Windows 支持的候选；不照搬全部工具 |
| [Bytebot](https://github.com/bytebot-ai/bytebot) | 容器化 Ubuntu 桌面、自托管 Agent、Web 任务 UI、实时桌面、人工接管、REST | 控制隔离 Linux 桌面而非当前 Mac；TypeScript/NestJS/Next.js；GitHub API 在调研时标记 archived=true | UI/远程桌面工作流参考；不优先作为新项目基础 |

## 关键证据

- Cua 的[集成选择](https://cua.ai/docs/concepts/choose-a-cua-driver-integration)明确区分 MCP、同进程 SDK、私有 worker、桌面应用持有权限的服务；macOS 权限应归属于应用身份。
- Cua 有独立的[轨迹录制与渲染文档](https://cua.ai/docs/how-to-guides/driver/record-and-render-a-trajectory)，需要实际验收其交互式历史查看效果，不能只凭“recording”推断完整产品 UI。
- Cua 的[从桌面应用暴露 MCP](https://cua.ai/docs/how-to-guides/driver/expose-mcp-from-desktop-app)与本项目的权限持有方式直接相关。
- Peekaboo 官方 [README](https://github.com/openclaw/Peekaboo)列出菜单栏应用；[视觉反馈](https://peekaboo.sh/visualizer.html)指的是操作现场提示；[结构化日志](https://peekaboo.sh/logging-guide.html)主要为诊断输出，这两者不能自动等同于历史轨迹控制台。
- Gokin Studio 官方 [README](https://github.com/ginkida/gokin-studio)明确写有 Agent activity timeline、每会话 JSONL replay log、settings audit log 和 permission-gated computer use。当前只核对文档，未做代码审计和稳定性背书。
- Bytebot 的归档状态通过 [GitHub API](https://api.github.com/repos/bytebot-ai/bytebot)核实；搜索索引仍可能显示旧功能介绍。

## 许可核对

GitHub 元数据：Cua MIT；Peekaboo MIT；Munim Apache-2.0；Gokin Studio MIT；coff33ninja/go-mcp-computer-use Apache-2.0；Bytebot Apache-2.0。hightemp 项目 README 写 MIT，但 API 的 license 字段为空，不能据此视为已完成许可审查。依赖/模型/资源可能有单独许可，需要在实际引入时逐一检查。

## 对当前实现的影响

1. 不把完整 Agent 聊天/模型推理循环塞入服务：远端 Agent 负责规划，本地程序负责执行、权限和审计。
2. 工具规格与实现分离；REST/MCP/OpenAPI/UI 全部来自同一注册表，避免多套接口漂移。
3. Desktop Driver 保持独立接口；原生坐标输入只是第一版，后台窗口输入和辅助功能元素定位可换后端。
4. 优先实现容易验证的完整链路：连接 → 调用 → 实际结果 → 本地日志 → 过滤/截图查看/导出 → 紧急暂停。
5. Go/Wails 并不能天然提供终端沙箱。明确区分工作区约束与宿主 shell；容器模式是独立的后续能力。
6. 暂不自动创建公网隧道或安装其他项目；提供独立 gateway，避免将本地管理界面一并暴露。
