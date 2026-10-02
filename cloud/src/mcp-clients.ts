import type { Env, User } from './index.ts'
import { body, hash, HTTPError, json, now, randomToken, text } from './http.ts'

export type Client = {
  id: string; user_id: string | null; name: string; secret_hash: string | null
  redirect_uris: string; auth_method: 'none' | 'client_secret_post' | 'client_secret_basic'
}
export const mcpScope = 'computers:control'
export const mcpResource = (env: Env) => env.PUBLIC_ORIGIN + '/mcp'
export function validateRedirects(value: unknown): string[] {
  if (!Array.isArray(value) || !value.length || value.length > 10) throw new HTTPError(400, 'Provide 1–10 exact OAuth redirect URLs')
  for (const uri of value) {
    if (typeof uri !== 'string' || uri.length > 2048 || uri !== uri.trim()) throw new HTTPError(400, 'Invalid redirect URL')
    let url: URL
    try { url = new URL(uri) } catch { throw new HTTPError(400, 'Invalid redirect URL') }
    if (uri.includes('#') || url.username || url.password || uri.includes('*') || !(url.protocol === 'https:' || url.protocol === 'http:' && ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname))) throw new HTTPError(400, 'Redirect URL must use HTTPS (or localhost), without fragments or wildcards')
  }
  return [...new Set(value)]
}

export async function manageClients(req: Request, env: Env, owner: User): Promise<Response> {
  const path = new URL(req.url).pathname
  if (path === '/api/mcp/clients' && req.method === 'GET') {
    const { results } = await env.DB.prepare('SELECT c.id,c.name,c.redirect_uris,c.created_at,c.user_id IS NULL AS automatic FROM mcp_clients c WHERE c.user_id=? OR (c.user_id IS NULL AND EXISTS(SELECT 1 FROM mcp_grants g WHERE g.client_id=c.id AND g.user_id=? AND g.refresh_expires_at>?)) ORDER BY c.created_at DESC').bind(owner.id, owner.id, now()).all<{ redirect_uris: string; automatic: number }>()
    return json({ mcp_url: mcpResource(env), clients: results.map(c => ({ ...c, automatic: !!c.automatic, redirect_uris: JSON.parse(c.redirect_uris) })) })
  }
  if (path === '/api/mcp/clients' && req.method === 'POST') {
    const input = await body(req), name = text(input.name), uris = validateRedirects(input.redirect_uris)
    const id = randomToken(), secret = randomToken()
    await env.DB.prepare('INSERT INTO mcp_clients(id,user_id,name,secret_hash,redirect_uris,created_at) SELECT ?,?,?,?,?,? WHERE (SELECT COUNT(*) FROM mcp_clients WHERE user_id=?)<10').bind(id, owner.id, name, await hash(secret), JSON.stringify(uris), now(), owner.id).run()
    if (!await env.DB.prepare('SELECT id FROM mcp_clients WHERE id=?').bind(id).first()) throw new HTTPError(429, 'At most 10 MCP clients per account')
    return json({ client_id: id, client_secret: secret, mcp_url: mcpResource(env) }, 201)
  }
  const match = /^\/api\/mcp\/clients\/([A-Za-z0-9_-]{43})$/.exec(path)
  if (match && req.method === 'DELETE') {
    await env.DB.batch([
      env.DB.prepare('DELETE FROM mcp_grants WHERE client_id=? AND user_id=?').bind(match[1], owner.id),
      env.DB.prepare('DELETE FROM mcp_codes WHERE client_id=? AND user_id=?').bind(match[1], owner.id),
      env.DB.prepare('DELETE FROM mcp_requests WHERE client_id=? AND session_hash IN (SELECT token_hash FROM sessions WHERE user_id=?)').bind(match[1], owner.id),
      env.DB.prepare('DELETE FROM mcp_clients WHERE id=? AND user_id=?').bind(match[1], owner.id),
    ])
    return json({ ok: true })
  }
  throw new HTTPError(404, 'Not found')
}

// RFC 7591 registration declares app metadata; it never grants access to a user.
// User authentication and explicit consent are still required for every grant.
export async function registerClient(req: Request, env: Env): Promise<Response> {
  const input = await body(req, 16384)
  let redirects: string[]
  try { redirects = validateRedirects(input.redirect_uris) } catch { return json({ error: 'invalid_redirect_uri' }, 400) }
  const method = input.token_endpoint_auth_method ?? 'client_secret_basic'
  if (!['none', 'client_secret_post', 'client_secret_basic'].includes(String(method))) return json({ error: 'invalid_client_metadata' }, 400)
  for (const [key, allowed] of [['grant_types', ['authorization_code', 'refresh_token']], ['response_types', ['code']]] as const) {
    const v = input[key]
    if (v !== undefined && (!Array.isArray(v) || !v.length || v.some(x => !allowed.includes(x as never)) || key === 'grant_types' && !v.includes('authorization_code'))) return json({ error: 'invalid_client_metadata' }, 400)
  }
  if (input.scope !== undefined && input.scope !== mcpScope) return json({ error: 'invalid_client_metadata' }, 400)
  let name: string
  try { name = input.client_name === undefined ? 'MCP app' : text(input.client_name) } catch { return json({ error: 'invalid_client_metadata' }, 400) }
  // Cloudflare supplies this header. Never trust arbitrary client identity headers.
  const minute = Math.floor(now() / 60), key = await hash('mcp-register:' + minute + ':' + (req.headers.get('CF-Connecting-IP') || 'local'))
  const limit = await env.DB.prepare('INSERT INTO mcp_registration_limits(key,count,expires_at) VALUES(?,1,?) ON CONFLICT(key) DO UPDATE SET count=count+1 RETURNING count').bind(key, now() + 120).first<{ count: number }>()
  if (!limit || limit.count > 20) return json({ error: 'temporarily_unavailable' }, 429)
  const id = randomToken(), secret = method === 'none' ? null : randomToken()
  await env.DB.prepare('INSERT INTO mcp_clients(id,user_id,name,secret_hash,redirect_uris,auth_method,created_at) SELECT ?,NULL,?,?,?,?,? WHERE (SELECT COUNT(*) FROM mcp_clients WHERE user_id IS NULL AND created_at>?)<1000').bind(id, name, secret ? await hash(secret) : null, JSON.stringify(redirects), method, now(), now() - 86400).run()
  if (!await env.DB.prepare('SELECT id FROM mcp_clients WHERE id=?').bind(id).first()) return json({ error: 'temporarily_unavailable' }, 429)
  return json({ client_id: id, client_id_issued_at: now(), ...(secret ? { client_secret: secret, client_secret_expires_at: 0 } : {}), client_name: name, redirect_uris: redirects, token_endpoint_auth_method: method, grant_types: input.grant_types || ['authorization_code', 'refresh_token'], response_types: ['code'], scope: mcpScope }, 201)
}

export async function authenticateClient(req: Request, env: Env, form: URLSearchParams): Promise<Client | null> {
  let id = form.get('client_id') || '', secret = form.get('client_secret') || ''
  const authorization = req.headers.get('Authorization')
  if (authorization) {
    if (!authorization.startsWith('Basic ') || form.has('client_secret')) return null
    try {
      const decoded = atob(authorization.slice(6)), split = decoded.indexOf(':')
      if (split < 0) return null
      const basicID = decodeURIComponent(decoded.slice(0, split).replace(/\+/g, ' '))
      if (id && id !== basicID) return null
      id = basicID; secret = decodeURIComponent(decoded.slice(split + 1).replace(/\+/g, ' '))
    } catch { return null }
  }
  const client = await env.DB.prepare('SELECT * FROM mcp_clients WHERE id=?').bind(id).first<Client>()
  if (!client) return null
  if (client.auth_method === 'none') return !authorization && !form.has('client_secret') ? client : null
  if (!secret || await hash(secret) !== client.secret_hash) return null
  // Manually issued clients accept both supported secret mechanisms. Dynamically
  // registered clients use the exact method negotiated at registration.
  if (client.user_id === null && (client.auth_method === 'client_secret_basic') !== !!authorization) return null
  return client
}
