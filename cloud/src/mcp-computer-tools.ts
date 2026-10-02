import type { Env } from './index.ts'
import { HTTPError } from './http.ts'
import { computer, summary } from './computer-discovery.ts'

type Identity = { user_id: string; grant_id: string }
type ToolResult = { content: Record<string, unknown>[]; isError: boolean; structuredContent?: unknown }
const maxResponseBytes = 8 * 1024 * 1024

// Only use the freshly authenticated owner's heartbeat URL. Arbitrary tool
// arguments can never select a URL, header, redirect or management endpoint.
export function permittedGateway(gateway: string, env: Env): boolean {
  const url = new URL(gateway)
  const quickTunnel = /^[a-z0-9]+(?:-[a-z0-9]+)*\.trycloudflare\.com$/.test(url.hostname)
  const fixedHosts = (env.MCP_ALLOWED_TUNNEL_HOSTS || '').split(',').map(h => h.trim().toLowerCase()).filter(Boolean)
  return url.protocol === 'https:' && !url.port && !url.username && !url.password && !url.search && !url.hash && /^\/[A-Za-z0-9]{8}$/.test(url.pathname) && (quickTunnel || fixedHosts.includes(url.hostname))
}
async function readResult(response: Response): Promise<unknown> {
  if (Number(response.headers.get('content-length') || 0) > maxResponseBytes) { await response.body?.cancel(); throw new HTTPError(502, 'Computer response is too large; request a smaller result') }
  const reader = response.body?.getReader()
  if (!reader) throw new HTTPError(502, 'Computer returned an empty response')
  let size = 0, value = ''; const decoder = new TextDecoder()
  while (true) {
    const chunk = await reader.read(); if (chunk.done) break
    size += chunk.value.length
    if (size > maxResponseBytes) { await reader.cancel(); throw new HTTPError(502, 'Computer response is too large; request a smaller result') }
    value += decoder.decode(chunk.value, { stream: true })
  }
  try { return JSON.parse(value + decoder.decode()) } catch { throw new HTTPError(502, 'Computer returned an invalid response') }
}
function object(value: unknown): value is Record<string, unknown> { return !!value && typeof value === 'object' && !Array.isArray(value) }

export async function computerTool(env: Env, identity: Identity, deviceID: string, name: string, args: Record<string, unknown>): Promise<ToolResult> {
  if (!/^[A-Za-z][A-Za-z0-9_]{0,127}$/.test(name)) throw new HTTPError(400, 'Invalid computer tool name')
  const device = summary(await computer(env, identity, deviceID))
  if (!device.online) throw new HTTPError(409, 'Computer is offline. Open ReadyRig and keep the computer awake.')
  if (device.paused) throw new HTTPError(423, 'Computer control is paused. Resume control only if authorized by the user.')
  if (!device.links) throw new HTTPError(409, 'Public sharing is not ready. Start a quick tunnel with control_computer and wait for a ready connection.')
  if (!permittedGateway(device.links.gateway, env)) throw new HTTPError(403, 'This tunnel host is not enabled for cloud tool calls. Use a quick tunnel, or ask the service administrator to allow this fixed hostname.')
  const headers = { 'Content-Type': 'application/json', 'X-Session-ID': 'cloud-' + identity.grant_id, 'X-Client-Name': 'ReadyRig Cloud MCP' }
  let response: Response, output: unknown
  try {
    response = await fetch(device.links.gateway + '/api/v1/tools/' + name, { method: 'POST', headers, body: JSON.stringify(args), redirect: 'manual', signal: AbortSignal.timeout(55000) })
    if (response.status >= 300 && response.status < 400) { await response.body?.cancel(); throw new HTTPError(502, 'Computer URL redirected. Wait for a fresh heartbeat; this call was not retried.') }
    output = await readResult(response)
  } catch (error) {
    if (error instanceof HTTPError) throw error
    // The tool may already have executed. Never replay a mutating request.
    throw new HTTPError(502, 'Computer connection failed or timed out. Execution may have occurred. Check local activity or an existing command session before retrying.')
  }
  if (!object(output)) throw new HTTPError(502, 'Computer returned an invalid tool response')
  const failed = !response.ok || !!output.error || ['denied', 'error'].includes(String(output.status))
  const value = output.result
  // Chrome tools already contain MCP text/image/structured result blocks.
  if (object(value) && Array.isArray(value.content)) {
    return { content: value.content, ...(value.structuredContent !== undefined ? { structuredContent: value.structuredContent } : {}), isError: failed || value.isError === true }
  }
  const content: Record<string, unknown>[] = []
  if (object(value) && typeof value.screenshot === 'string' && value.screenshot.startsWith('data:image/jpeg;base64,')) {
    content.push({ type: 'image', mimeType: 'image/jpeg', data: value.screenshot.slice('data:image/jpeg;base64,'.length) })
    delete value.screenshot
  }
  content.push({ type: 'text', text: JSON.stringify(output) })
  return { content, isError: failed }
}
