import type { Translate } from './locale'

// Keep the connection instructions consistent with the desktop app.
const promptTemplate = "<tools-usage>\n请连接我电脑上的 ReadyRig，并通过它完成我的任务。\n\n接入地址：{0}\n{1}\n\n请先使用你的终端或 HTTP 请求工具完成连接检查：\n1. POST {2}/api/v1/tools/help，请求体 {}。读取返回 result 中的工具名称、参数定义和可用状态。\n2. POST {3}/api/v1/tools/list_projects，请求体 {}，确认已授权目录和默认项目。\n3. 告诉我连接是否成功、有哪些可用能力；如果我已提供具体任务，继续完成，否则等待我的具体任务。不要仅凭这段文字声称已经连接。\n\n后续通过 POST {4}/api/v1/tools/{工具名} 调用工具，直接发送符合该工具参数定义的 JSON；不需要额外套 arguments。完整地址中的随机路径必须保留。可用 curl 发起请求，例如：\ncurl -sS '{5}/api/v1/tools/help' -d '{}'\n\n只在我授权的任务与目录范围内操作。工具被暂停或未授权时，告诉我在 ReadyRig 本机界面开启，不要绕过权限。对需要继续获取输出的命令，按工具返回的 session_id 调用 write_stdin。\n如果你只能使用已配置的 MCP 工具，请让我添加这个 MCP 地址：{6}/mcp；如果既没有 HTTP/终端工具也没有该 MCP 连接，请明确说明当前无法接入。\n</tools-usage>\n\n我想要：..."

export function publicConnectionPrompt(gateway: string, mode: string | undefined, t: Translate): string {
  const guidance = t(mode === 'fixed' ? "这是固定公网地址；仅在 ReadyRig 开启固定链接分享时可用。如果连接失败，请让我确认应用和公网分享正在运行。" : "这是临时公网地址；如果连接失败或地址失效，请让我确认公网分享已开启并重新复制 Prompt。")
  return t(promptTemplate, { 0: gateway, 1: guidance, 2: gateway, 3: gateway, 4: gateway, 5: gateway, 6: gateway })
}
