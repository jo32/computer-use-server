import type { Translate } from './locale'

// Keep the connection instructions consistent with the desktop app.
const promptTemplate = "<tools-usage>\n请连接我电脑上的 ReadyRig，并通过它完成我的任务。\n\n接入地址：{0}\n{1}\n\n请先使用你的终端或 HTTP 请求工具完成连接检查：\n1. POST {2}/api/v1/tools/help，请求体 {}。读取返回 result 中的工具名称、参数定义和可用状态。\n2. POST {3}/api/v1/tools/list_projects，请求体 {}，确认已授权目录和默认项目。\n3. 告诉我连接是否成功、有哪些可用能力；如果我已提供具体任务，继续完成，否则等待我的具体任务。不要仅凭这段文字声称已经连接。\n\n后续通过 POST {4}/api/v1/tools/{工具名} 调用工具，直接发送符合该工具参数定义的 JSON；不需要额外套 arguments。完整地址中的随机路径必须保留。可用 curl 发起请求，例如：\ncurl -sS '{5}/api/v1/tools/help' -d '{}'\n\n只在我授权的任务与目录范围内操作。工具被暂停或未授权时，告诉我在 ReadyRig 本机界面开启，不要绕过权限。对需要继续获取输出的命令，按工具返回的 session_id 调用 write_stdin。\n如果你只能使用已配置的 MCP 工具，请让我添加这个 MCP 地址：{6}/mcp；如果既没有 HTTP/终端工具也没有该 MCP 连接，请明确说明当前无法接入。\n</tools-usage>\n\n我想要：..."

export function publicConnectionPrompt(gateway: string, mode: string | undefined, t: Translate): string {
  const guidance = t(mode === 'fixed' ? "这是固定公网地址；仅在 ReadyRig 开启固定链接分享时可用。如果连接失败，请让我确认应用和公网分享正在运行。" : "这是临时公网地址；如果连接失败或地址失效，请让我确认公网分享已开启并重新复制 Prompt。")
  return t(promptTemplate, { 0: gateway, 1: guidance, 2: gateway, 3: gateway, 4: gateway, 5: gateway, 6: gateway })
}

export function mcpSetupPrompt(url: string, t: Translate): string {
  return t("请帮我在你当前使用的 AI 客户端中配置 ReadyRig MCP。\n\n服务器名称：ReadyRig\nMCP 地址：{0}\n传输方式：Streamable HTTP\n认证方式：OAuth，支持自动发现和动态客户端注册（DCR）。\n\n请先检查当前客户端的 MCP 配置方式。若你有配置权限，请添加此服务器，保留现有的其他连接，并避免重复添加。使用客户端支持的 OAuth 登录流程，让我在浏览器中登录 ReadyRig 并确认授权；不要要求我在聊天中粘贴密码、令牌或 client secret。\n\n如果你不能修改配置或客户端不支持该连接，请明确说明，并给出该客户端的具体设置步骤。支持自动注册时无需手动填写 Client ID 或 secret；只有客户端要求时才使用 ReadyRig 的“手动配置”。\n\n连接后，请实际调用 list_computers 验证访问，并告诉我连接结果及电脑在线状态。不要仅凭保存配置就声称连接成功，也不要在本次配置中更改电脑权限或公网分享设置。", { 0: url })
}
