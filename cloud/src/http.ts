export const now = () => Math.floor(Date.now() / 1000)
export function randomToken(): string {
  return btoa(String.fromCharCode(...crypto.getRandomValues(new Uint8Array(32)))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}
export async function hash(value: string): Promise<string> {
  return Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256', new TextEncoder().encode(value))), x => x.toString(16).padStart(2, '0')).join('')
}
export class HTTPError extends Error { status: number; constructor(status: number, message: string) { super(message); this.status = status } }
export function json(data: unknown, status = 200): Response { return Response.json(data, { status, headers: { 'Cache-Control': 'no-store' } }) }
export async function body(req: Request, limit = 65536): Promise<Record<string, unknown>> {
  if (!req.headers.get('Content-Type')?.startsWith('application/json')) throw new HTTPError(415, '请使用 JSON 请求')
  if (Number(req.headers.get('Content-Length') || 0) > limit) throw new HTTPError(413, '请求过大')
  const reader = req.body?.getReader(); if (!reader) throw new HTTPError(400, '缺少请求内容')
  const chunks: Uint8Array[] = []; let size = 0
  while (true) { const { value, done } = await reader.read(); if (done) break; size += value.length; if (size > limit) { await reader.cancel(); throw new HTTPError(413, '请求过大') }; chunks.push(value) }
  const bytes = new Uint8Array(size); let offset = 0; for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.length }
  try { const value = JSON.parse(new TextDecoder().decode(bytes)); if (!value || typeof value !== 'object' || Array.isArray(value)) throw 0; return value } catch { throw new HTTPError(400, '无效的 JSON') }
}
export function text(value: unknown, max = 128): string { if (typeof value !== 'string' || !value.trim() || new TextEncoder().encode(value).length > max) throw new HTTPError(400, '文本长度无效'); return value.trim() }
