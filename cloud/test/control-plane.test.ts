import { test, beforeEach } from 'node:test'
import assert from 'node:assert/strict'
import { DatabaseSync } from 'node:sqlite'
import { readFileSync, readdirSync } from 'node:fs'
import { generateKeyPair, exportJWK, SignJWT } from 'jose'
import worker, { hash, randomToken } from '../src/index.ts'
import type { Env } from '../src/index.ts'

class Statement {
  db: DatabaseSync; sql: string; values: any[] = []
  constructor(db: DatabaseSync, sql: string) { this.db = db; this.sql = sql }
  bind(...values: any[]) { const stmt = new Statement(this.db, this.sql); stmt.values = values; return stmt }
  async first() { return this.db.prepare(this.sql).get(...this.values) || null }
  async all() { return { results: this.db.prepare(this.sql).all(...this.values) } }
  async run() { return { success: true, meta: this.db.prepare(this.sql).run(...this.values) } }
}
let db: DatabaseSync, env: Env, session: string, otherSession: string
const origin = 'https://readyrig.example'
const timestamp = () => Math.floor(Date.now() / 1000)
beforeEach(async () => {
  db?.close(); db = new DatabaseSync(':memory:')
  for (const file of readdirSync(new URL('../migrations/', import.meta.url)).filter(file => file.endsWith('.sql')).sort()) db.exec(readFileSync(new URL('../migrations/' + file, import.meta.url), 'utf8'))
  env = { PUBLIC_ORIGIN: origin, GOOGLE_CLIENT_ID: 'test-client', GOOGLE_CLIENT_SECRET: 'test-secret', ASSETS: { fetch: async () => new Response('website') }, DB: {
    prepare: (sql: string) => new Statement(db, sql),
    batch: async (statements: Statement[]) => { db.exec('BEGIN'); try { const results = statements.map(s => ({ success: true, meta: db.prepare(s.sql).run(...s.values) })); db.exec('COMMIT'); return results } catch (e) { db.exec('ROLLBACK'); throw e } }
  } } as unknown as Env
  session = randomToken(); otherSession = randomToken()
  for (const [id, token] of [['alice', session], ['bob', otherSession]]) {
    db.prepare('INSERT INTO users VALUES(?,?,?,?)').run(id, id + '@example.com', id, timestamp())
    db.prepare('INSERT INTO sessions VALUES(?,?,?)').run(await hash(token), id, timestamp() + 600)
  }
})
async function call(path: string, method = 'GET', input?: unknown, headers: Record<string, string> = {}) {
  const req = new Request(origin + path, { method, headers: { ...(input === undefined ? {} : { 'Content-Type': 'application/json' }), ...headers }, body: input === undefined ? undefined : JSON.stringify(input) })
  const response = await worker.fetch(req, env)
  const result = response.headers.get('content-type')?.includes('application/json') ? await response.json() as any : null
  return { response, result }
}
const owner = () => ({ Cookie: '__Host-readyrig_session=' + session, Origin: origin })
const secondOwner = () => ({ Cookie: '__Host-readyrig_session=' + otherSession, Origin: origin })
async function register() {
  const token = randomToken()
  const { result: pair } = await call('/api/agent/pair', 'POST', { name: 'Test Mac', platform: 'darwin', challenge: await hash(token) })
  assert.equal((await call('/api/pairings/' + pair.id, 'POST', {}, owner())).response.status, 200)
  const { result } = await call('/api/agent/pair/' + pair.id, 'POST', { verifier: token })
  assert.equal(result.device_id, pair.id)
  return { id: pair.id, token, headers: { Authorization: 'Bearer ' + token } }
}
const snapshot = { version: '0.5.0', platform: 'darwin', paused: false, enabled: { files: true }, tunnel: { state: 'stopped' } }

test('domain migration redirects browsers while existing device credentials keep working', async () => {
  const device = await register()
  const legacy = 'https://legacy.example'
  env.LEGACY_ORIGIN = legacy
  const browser = await worker.fetch(new Request(legacy + '/console?pair=' + device.id), env)
  assert.equal(browser.status, 308)
  assert.equal(browser.headers.get('location'), origin + '/console?pair=' + device.id)
  const heartbeat = await worker.fetch(new Request(legacy + '/api/agent/heartbeat', { method: 'POST', headers: { ...device.headers, 'Content-Type': 'application/json' }, body: JSON.stringify({ snapshot }) }), env)
  assert.equal(heartbeat.status, 200)
  assert.equal((await call('/api/devices', 'GET', undefined, owner())).result.devices[0].online, true)
  const pairing = await worker.fetch(new Request(legacy + '/api/agent/pair', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: 'Legacy Mac', platform: 'darwin', challenge: await hash(randomToken()) }) }), env)
  assert.equal(pairing.status, 201)
  for (const host of [legacy, 'https://unknown.example']) {
    const mutation = await worker.fetch(new Request(host + '/api/devices/' + device.id, { method: 'DELETE', headers: owner() }), env)
    assert.equal(mutation.status, 400)
  }
  assert.equal((await worker.fetch(new Request('https://unknown.example/api/health'), env)).status, 400)
  assert.equal((await call('/api/devices/' + device.id, 'DELETE', undefined, { ...owner(), Origin: legacy })).response.status, 403)
})

test('binding requires owner confirmation and secret verifier; lost claim response is recoverable', async () => {
  const token = randomToken(), { result: pair } = await call('/api/agent/pair', 'POST', { name: 'Mac', platform: 'darwin', challenge: await hash(token) })
  assert.equal((await call('/api/agent/pair/' + pair.id, 'POST', { verifier: token })).result.pending, true)
  assert.equal((await call('/api/agent/pair/' + pair.id, 'POST', { verifier: randomToken() })).response.status, 410)
  assert.equal((await call('/api/pairings/' + pair.id)).response.status, 401)
  await call('/api/pairings/' + pair.id, 'POST', {}, owner())
  assert.equal((await call('/api/pairings/' + pair.id, 'POST', {}, secondOwner())).response.status, 404)
  const one = await call('/api/agent/pair/' + pair.id, 'POST', { verifier: token })
  const two = await call('/api/agent/pair/' + pair.id, 'POST', { verifier: token })
  assert.equal(one.result.device_id, two.result.device_id)
  assert.equal(one.result.email, 'alice@example.com')
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM devices').get()!.n, 1)
  assert.equal((await call('/api/devices', 'GET', undefined, secondOwner())).result.devices.length, 0)
})
test('heartbeats update presence, strip local secrets, and deliver commands in order once', async () => {
  const device = await register()
  const create = async (kind: string, payload: unknown) => (await call('/api/devices/' + device.id + '/commands', 'POST', { kind, payload, request_id: randomToken() }, owner())).result
  const first = await create('tunnel.start', { mode: 'quick' }), second = await create('tunnel.stop', {})
  const heartbeat = (results: unknown[] = []) => call('/api/agent/heartbeat', 'POST', { snapshot: { ...snapshot, workspace: '/private', token: 'private', tunnel: { state: 'stopped', logs: ['private'] } }, results }, device.headers)
  assert.equal((await heartbeat()).result.command.id, first.id)
  assert.equal((await heartbeat()).result.command, null)
  assert.equal((await heartbeat([{ id: first.id }])).result.command.id, second.id)
  await heartbeat([{ id: second.id, error: 'example failure' }])
  const listed = (await call('/api/devices', 'GET', undefined, owner())).result.devices[0]
  assert.equal(listed.online, true); assert.equal(listed.snapshot.workspace, undefined); assert.equal(listed.snapshot.tunnel.logs, undefined)
  assert.equal(db.prepare('SELECT status FROM commands WHERE id=?').get(first.id)!.status, 'completed')
  assert.equal(db.prepare('SELECT status FROM commands WHERE id=?').get(second.id)!.status, 'failed')
  db.prepare('UPDATE devices SET last_seen=? WHERE id=?').run(timestamp() - 61, device.id)
  assert.equal((await call('/api/devices', 'GET', undefined, owner())).result.devices[0].online, false)
})
test('another account and cross-origin pages cannot manage devices', async () => {
  const device = await register(), path = '/api/devices/' + device.id + '/commands'
  assert.equal((await call(path, 'POST', { kind: 'tunnel.stop', payload: {}, request_id: randomToken() }, secondOwner())).response.status, 404)
  assert.equal((await call(path, 'GET', undefined, secondOwner())).response.status, 404)
  assert.equal((await call('/api/devices/' + device.id, 'DELETE', undefined, { ...owner(), Origin: 'https://evil.example' })).response.status, 403)
  assert.equal((await call('/api/devices', 'GET', undefined, device.headers)).response.status, 401)
})
test('overlapping heartbeats atomically deliver only one command', async () => {
  const device = await register()
  await call('/api/devices/' + device.id + '/commands', 'POST', { kind: 'tunnel.stop', payload: {}, request_id: randomToken() }, owner())
  const replies = await Promise.all(Array.from({ length: 4 }, () => call('/api/agent/heartbeat', 'POST', { snapshot }, device.headers)))
  assert.ok(replies.every(r => r.response.status === 200))
  assert.equal(replies.filter(r => r.result.command).length, 1)
})
test('command submission is idempotent and unknown operations are rejected', async () => {
  const device = await register(), path = '/api/devices/' + device.id + '/commands', request = { kind: 'tunnel.stop', payload: {}, request_id: randomToken() }
  const first = await call(path, 'POST', request, owner()), again = await call(path, 'POST', request, owner())
  assert.equal(first.result.id, again.result.id)
  assert.equal((await call(path, 'POST', { ...request, kind: 'tunnel.start', payload: { mode: 'quick' } }, owner())).response.status, 409)
  for (const [kind, payload] of [['shell.exec', {}], ['capability.set', { category: 'full_access', enabled: true }], ['control.pause', { paused: 'false' }], ['tunnel.start', { mode: 'unknown' }]]) assert.equal((await call(path, 'POST', { kind, payload, request_id: randomToken() }, owner())).response.status, 400)
})
test('expired commands are not executed or automatically redelivered', async () => {
  const device = await register(), path = '/api/devices/' + device.id + '/commands'
  const request = { kind: 'tunnel.stop', payload: {}, request_id: randomToken() }
  const { result: command } = await call(path, 'POST', request, owner())
  db.prepare('UPDATE commands SET expires_at=? WHERE id=?').run(timestamp() - 1, command.id)
  const beat = () => call('/api/agent/heartbeat', 'POST', { snapshot }, device.headers)
  assert.equal((await beat()).result.command, null)
  const { result: next } = await call(path, 'POST', { ...request, request_id: randomToken() }, owner())
  assert.equal((await beat()).result.command.id, next.id)
  db.prepare('UPDATE commands SET delivered_at=? WHERE id=?').run(timestamp() - 91, next.id)
  assert.equal((await beat()).result.command, null)
  assert.equal(db.prepare('SELECT status FROM commands WHERE id=?').get(next.id)!.status, 'expired')
})
test('revocation removes device access and queued commands; old pairing cannot resurrect it', async () => {
  const device = await register()
  await call('/api/devices/' + device.id + '/commands', 'POST', { kind: 'tunnel.stop', payload: {}, request_id: randomToken() }, owner())
  assert.equal((await call('/api/devices/' + device.id, 'DELETE', undefined, owner())).response.status, 200)
  assert.equal((await call('/api/agent/heartbeat', 'POST', { snapshot }, device.headers)).response.status, 401)
  assert.equal((await call('/api/agent/pair/' + device.id, 'POST', { verifier: device.token })).response.status, 410)
  assert.equal(db.prepare('SELECT status FROM commands').get()!.status, 'revoked')
})
test('receipts cannot acknowledge another computer command', async () => {
  const a = await register(), b = await register()
  const { result: command } = await call('/api/devices/' + a.id + '/commands', 'POST', { kind: 'tunnel.stop', payload: {}, request_id: randomToken() }, owner())
  await call('/api/agent/heartbeat', 'POST', { snapshot }, a.headers)
  await call('/api/agent/heartbeat', 'POST', { snapshot, results: [{ id: command.id }] }, b.headers)
  assert.equal(db.prepare('SELECT status FROM commands WHERE id=?').get(command.id)!.status, 'executing')
})
test('OAuth binds state to browser, verifies signed Google identity and nonce, and creates session', async () => {
  const { publicKey, privateKey } = await generateKeyPair('RS256'), jwk = await exportJWK(publicKey)
  Object.assign(jwk, { kid: 'test-google-key', alg: 'RS256', use: 'sig' })
  const start = await call('/auth/google?return_to=https://evil.example')
  const location = new URL(start.response.headers.get('Location')!)
  assert.equal(location.searchParams.get('code_challenge_method'), 'S256')
  const state = location.searchParams.get('state')!, flow = db.prepare('SELECT * FROM oauth_states WHERE id=?').get(state)!
  assert.equal((await call('/auth/callback?state=' + state + '&code=test')).response.status, 400)
  assert.ok(db.prepare('SELECT id FROM oauth_states WHERE id=?').get(state))
  let idToken = await new SignJWT({ email: 'google@example.com', email_verified: true, nonce: flow.nonce, name: 'Google User' }).setProtectedHeader({ alg: 'RS256', kid: 'test-google-key' }).setIssuer('https://accounts.google.com').setAudience(env.GOOGLE_CLIENT_ID).setSubject('google-user').setExpirationTime('1h').setIssuedAt().sign(privateKey)
  const originalFetch = globalThis.fetch
  globalThis.fetch = async (url: any) => String(url).includes('/token') ? Response.json({ id_token: idToken }) : Response.json({ keys: [jwk] })
  try {
    const completed = await call('/auth/callback?state=' + state + '&code=test', 'GET', undefined, { Cookie: start.response.headers.get('Set-Cookie')!.split(';')[0] })
    assert.equal(completed.response.status, 302); assert.equal(completed.response.headers.get('Location'), '/console')
    assert.ok(completed.response.headers.get('Set-Cookie')!.includes('HttpOnly'))
    assert.equal(db.prepare('SELECT email FROM users WHERE id=?').get('google-user')!.email, 'google@example.com')
    assert.equal((await call('/auth/callback?state=' + state + '&code=test', 'GET', undefined, { Cookie: start.response.headers.get('Set-Cookie')!.split(';')[0] })).response.status, 400)
    for (const invalid of ['nonce', 'audience', 'issuer', 'unverified-email']) {
      const begin = await call('/auth/google'), authURL = new URL(begin.response.headers.get('Location')!), nextState = authURL.searchParams.get('state')!
      const claims = db.prepare('SELECT * FROM oauth_states WHERE id=?').get(nextState)!
      idToken = await new SignJWT({ email: 'invalid@example.com', email_verified: invalid !== 'unverified-email', nonce: invalid === 'nonce' ? 'wrong-nonce' : claims.nonce }).setProtectedHeader({ alg: 'RS256', kid: 'test-google-key' }).setIssuer(invalid === 'issuer' ? 'https://wrong.example' : 'https://accounts.google.com').setAudience(invalid === 'audience' ? 'wrong-client' : env.GOOGLE_CLIENT_ID).setSubject('invalid-user').setExpirationTime('1h').sign(privateKey)
      const rejected = await call('/auth/callback?state=' + nextState + '&code=test', 'GET', undefined, { Cookie: begin.response.headers.get('Set-Cookie')!.split(';')[0] })
      assert.ok(rejected.response.status >= 400, invalid)
      assert.equal(rejected.response.headers.get('Set-Cookie'), null)
      assert.equal(db.prepare('SELECT id FROM users WHERE id=?').get('invalid-user'), undefined)
    }
  } finally { globalThis.fetch = originalFetch }
})
test('logout invalidates its session and malformed or oversized requests fail', async () => {
  assert.equal((await call('/api/logout', 'POST', {}, owner())).response.status, 200)
  assert.equal((await call('/api/me', 'GET', undefined, owner())).response.status, 401)
  assert.equal((await call('/api/agent/pair', 'POST', { challenge: 'a'.repeat(70000) })).response.status, 413)
  assert.equal((await call('/api/agent/pair', 'POST', { challenge: 'bad', name: 'Mac', platform: 'darwin' })).response.status, 400)
})

async function access(headers = owner()) {
  const { response, result } = await call('/api/discovery-token', 'POST', {}, headers)
  assert.equal(response.status, 201)
  return { ...result, headers: { Authorization: 'Bearer ' + result.token } }
}
const beat = (d: { headers: Record<string, string> }, next = snapshot, results: unknown[] = []) => call('/api/agent/heartbeat', 'POST', { snapshot: next, results }, d.headers)

test('public prompt discovery has placeholder credentials and working cloud routes in both languages', async () => {
  for (const lang of ['zh-CN', 'en']) {
    const { response, result } = await call('/api/v1/prompts?lang=' + lang)
    assert.equal(response.status, 200)
    assert.equal(response.headers.get('Cache-Control'), 'no-store')
    assert.ok(result.prompts[0].content.includes('<READYRIG_TOKEN>'))
    for (const path of ['/api/v1/computers', '/commands', 'capability.set', 'terminal', 'request_id', '/api/v1/tools/help', '/api/v1/tools/list_projects', 'links.gateway', 'links.mcp']) assert.ok(result.prompts[0].content.includes(path))
    assert.ok(!JSON.stringify(result).includes('alice'))
    assert.ok(!JSON.stringify(result).includes('/api/agent/'))
  }
})
test('cloud credentials are hashed, follow the signed-in account and cannot manage its login', async () => {
  const a = await register(), other = await register()
  db.prepare('UPDATE devices SET user_id=? WHERE id=?').run('bob', other.id)
  const token = await access()
  assert.match(token.token, /^rr_links_[A-Za-z0-9_-]{43}$/)
  const stored = db.prepare('SELECT * FROM discovery_tokens').get()!
  assert.equal(stored.token_hash, await hash(token.token))
  assert.equal(stored.session_hash, await hash(session))
  assert.equal(token.expires_at, db.prepare('SELECT expires_at FROM sessions WHERE token_hash=?').get(await hash(session))!.expires_at)
  assert.ok(!JSON.stringify(stored).includes(token.token))
  assert.notEqual(token.token, session)
  await beat(a)
  const listed = (await call('/api/v1/computers', 'GET', undefined, token.headers)).result.computers
  assert.deepEqual(listed.map((d: any) => d.id), [a.id]); assert.equal(listed[0].online, true)
  const b = await register()
  const updated = (await call('/api/v1/computers', 'GET', undefined, token.headers)).result.computers
  assert.deepEqual(new Set(updated.map((d: any) => d.id)), new Set([a.id, b.id]))
  assert.equal((await call('/api/v1/computers/' + b.id, 'GET', undefined, token.headers)).response.status, 200)
  assert.equal((await call('/api/v1/computers/' + other.id, 'GET', undefined, token.headers)).response.status, 404)
  const bobToken = await access(secondOwner())
  assert.deepEqual((await call('/api/v1/computers', 'GET', undefined, bobToken.headers)).result.computers.map((d: any) => d.id), [other.id])
  for (const route of ['/api/me', '/api/devices']) assert.equal((await call(route, 'GET', undefined, token.headers)).response.status, 401)
  assert.equal((await call('/api/discovery-token', 'POST', {}, { ...token.headers, Origin: origin })).response.status, 401)
  assert.equal((await call('/api/devices/' + a.id + '/commands', 'POST', { kind: 'capability.set', payload: { category: 'terminal', enabled: true }, request_id: randomToken() }, { ...token.headers, Origin: origin })).response.status, 401)
  for (const headers of [owner(), a.headers, { Authorization: 'Bearer ' + session }, {}]) assert.equal((await call('/api/v1/computers', 'GET', undefined, headers)).response.status, 401)
  assert.equal((await call('/api/v1/computers', 'GET', undefined, { ...token.headers, Origin: 'https://evil.example' })).response.status, 403)
  for (const [method, path] of [['GET', '/api/tokens'], ['POST', '/api/tokens'], ['DELETE', '/api/tokens/' + randomToken()]]) assert.equal((await call(path, method, method === 'GET' ? undefined : {}, owner())).response.status, 404)
})
test('credential issuance requires a same-origin login session without a management form', async () => {
  assert.equal((await call('/api/discovery-token', 'POST', {})).response.status, 403)
  assert.equal((await call('/api/discovery-token', 'POST', {}, { Origin: origin })).response.status, 401)
  assert.equal((await call('/api/discovery-token', 'POST', {}, { ...owner(), Origin: 'https://evil.example' })).response.status, 403)
  assert.equal((await call('/api/discovery-token', 'POST', [], owner())).response.status, 400)
  assert.equal((await call('/api/discovery-token', 'POST', { oversized: 'a'.repeat(70000) }, owner())).response.status, 413)
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM discovery_tokens').get()!.n, 0)
  // Issuance works before binding; there is no selected-device grant to maintain.
  const token = await access()
  assert.deepEqual((await call('/api/v1/computers', 'GET', undefined, token.headers)).result.computers, [])
})
test('authorized link discovery returns complete public URLs without changing the computer', async () => {
  const d = await register(), token = await access(), path = '/api/v1/computers/' + d.id
  const ready = { ...snapshot, tunnel: { state: 'ready', mode: 'quick', gateway: 'https://fixture.trycloudflare.com/AbC123xy', token: 'private-device-secret' } }
  await beat(d, ready as any)
  const listed = await call(path, 'GET', undefined, token.headers)
  assert.deepEqual(listed.result.computer.links, { gateway: ready.tunnel.gateway, mcp: ready.tunnel.gateway + '/mcp', console: ready.tunnel.gateway + '/app/' })
  assert.equal(listed.result.computer.connection.mode, 'quick')
  assert.ok(!JSON.stringify(listed.result).includes('private-device-secret'))
  for (const route of ['/connect', '/tools/help']) assert.equal((await call(path + route, 'POST', {}, token.headers)).response.status, 404)
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM commands').get()!.n, 0)
})
test('discovery hides unavailable links and refreshes temporary links after rotation', async () => {
  const d = await register(), token = await access(), path = '/api/v1/computers/' + d.id
  for (const state of ['stopped', 'connecting', 'failed']) {
    await beat(d, { ...snapshot, tunnel: { state, gateway: 'https://fixture.trycloudflare.com/AbC123xy' } } as any)
    assert.equal((await call(path, 'GET', undefined, token.headers)).result.computer.links, null)
  }
  for (const accessPath of ['AbC123xy', 'XyZ987ab']) {
    const gateway = 'https://fixture.trycloudflare.com/' + accessPath
    await beat(d, { ...snapshot, tunnel: { state: 'ready', gateway } } as any)
    assert.equal((await call(path, 'GET', undefined, token.headers)).result.computer.links.gateway, gateway)
  }
  db.prepare('UPDATE devices SET last_seen=? WHERE id=?').run(timestamp() - 61, d.id)
  const offline = (await call(path, 'GET', undefined, token.headers)).result.computer
  assert.equal(offline.online, false); assert.equal(offline.links, null)
  assert.equal(offline.connection.state, 'offline')
  for (const gateway of ['http://fixture.trycloudflare.com/AbC123xy', 'https://user:pass@fixture.trycloudflare.com/AbC123xy', 'https://fixture.trycloudflare.com/AbC123xy?secret=private', 'https://fixture.trycloudflare.com/invalid/path', 'bad-url']) {
    await beat(d, { ...snapshot, tunnel: { state: 'ready', gateway } } as any)
    assert.equal((await call(path, 'GET', undefined, token.headers)).result.computer.links, null)
  }
})
test('logout and session expiry stop discovery without changing public links or device credentials', async () => {
  const d = await register(), primary = await access(), path = '/api/v1/computers/' + d.id
  const ready = { ...snapshot, tunnel: { state: 'ready', gateway: 'https://fixture.trycloudflare.com/AbC123xy' } }
  await beat(d, ready as any)
  for (const kind of ['logout', 'expire']) {
    const browser = randomToken(), sessionHash = await hash(browser)
    db.prepare('INSERT INTO sessions VALUES(?,?,?)').run(sessionHash, 'alice', timestamp() + 600)
    const headers = { Cookie: '__Host-readyrig_session=' + browser, Origin: origin }
    const token = await access(headers)
    if (kind === 'logout') {
      assert.equal((await call('/api/logout', 'POST', {}, headers)).response.status, 200)
      assert.equal(db.prepare('SELECT * FROM discovery_tokens WHERE session_hash=?').get(sessionHash), undefined)
    } else db.prepare('UPDATE sessions SET expires_at=? WHERE token_hash=?').run(timestamp() - 1, sessionHash)
    assert.equal((await call(path, 'GET', undefined, token.headers)).response.status, 401)
    assert.equal((await call('/api/v1/computers', 'GET', undefined, token.headers)).response.status, 401)
    const controls = path + '/commands'
    assert.equal((await call(controls, 'GET', undefined, token.headers)).response.status, 401)
    assert.equal((await call(controls, 'POST', { kind: 'tunnel.stop', payload: {}, request_id: randomToken() }, token.headers)).response.status, 401)
    assert.equal((await call(path, 'GET', undefined, primary.headers)).response.status, 200)
    const heartbeat = await beat(d, ready as any)
    assert.equal(heartbeat.response.status, 200); assert.equal(heartbeat.result.command, null)
    assert.equal(JSON.parse(db.prepare('SELECT snapshot FROM devices WHERE id=?').get(d.id)!.snapshot as string).tunnel.gateway, ready.tunnel.gateway)
    assert.equal(db.prepare('SELECT COUNT(*) AS n FROM commands').get()!.n, 0)
  }
})
test('unbinding removes an authorized device and prevents old credentials from discovering its links', async () => {
  const d = await register(), token = await access()
  await call('/api/devices/' + d.id, 'DELETE', undefined, owner())
  assert.equal((await call('/api/v1/computers', 'GET', undefined, token.headers)).result.computers.length, 0)
  assert.equal((await call('/api/v1/computers/' + d.id, 'GET', undefined, token.headers)).response.status, 404)
})

test('Bearer computer controls share the browser queue and require an owned bound computer', async () => {
  const d = await register(), other = await register(), token = await access()
  db.prepare('UPDATE devices SET user_id=? WHERE id=?').run('bob', other.id)
  const path = '/api/v1/computers/' + d.id + '/commands'
  const input = { kind: 'capability.set', payload: { category: 'terminal', enabled: true }, request_id: randomToken() }
  for (const headers of [{}, owner(), d.headers]) assert.equal((await call(path, 'POST', input, headers)).response.status, 401)
  assert.equal((await call(path, 'POST', input, { ...token.headers, Origin: 'https://evil.example' })).response.status, 403)
  for (const method of ['GET', 'POST']) assert.equal((await call('/api/v1/computers/' + other.id + '/commands', method, method === 'POST' ? input : undefined, token.headers)).response.status, 404)
  const queued = await call(path, 'POST', input, token.headers)
  assert.equal(queued.response.status, 202); assert.equal(queued.result.status, 'queued')
  const retry = await call('/api/devices/' + d.id + '/commands', 'POST', input, owner())
  assert.equal(retry.response.status, 202); assert.equal(retry.result.id, queued.result.id)
  const delivered = await beat(d)
  assert.deepEqual(delivered.result.command, { id: queued.result.id, kind: input.kind, payload: input.payload })
  assert.equal((await beat(d)).result.command, null)
  await beat(d, { ...snapshot, enabled: { ...snapshot.enabled, terminal: true } }, [{ id: queued.result.id }])
  const history = (await call(path, 'GET', undefined, token.headers)).result.commands
  assert.equal(history[0].id, queued.result.id); assert.equal(history[0].status, 'completed')
  assert.equal((await call('/api/v1/computers/' + d.id, 'GET', undefined, token.headers)).result.computer.enabled.terminal, true)
  await call('/api/devices/' + d.id, 'DELETE', undefined, owner())
  assert.equal((await call(path, 'POST', { ...input, request_id: randomToken() }, token.headers)).response.status, 404)
  assert.equal((await call(path, 'GET', undefined, token.headers)).response.status, 404)
})
test('Bearer controls accept defined toggles and report execution failures and expiry', async () => {
  const d = await register(), token = await access(), path = '/api/v1/computers/' + d.id + '/commands'
  const actions = [
    ['capability.set', { category: 'terminal', enabled: true }],
    ['capability.set', { category: 'terminal', enabled: false }],
    ['capability.set', { category: 'files', enabled: true }],
    ['capability.set', { category: 'browser', enabled: true }],
    ['capability.set', { category: 'computer', enabled: true }],
    ['control.pause', { paused: true }], ['control.pause', { paused: false }],
    ['tunnel.start', { mode: 'quick' }], ['tunnel.start', { mode: 'fixed' }], ['tunnel.stop', {}]
  ]
  for (const [kind, payload] of actions) {
    const { response, result } = await call(path, 'POST', { kind, payload, request_id: randomToken() }, token.headers)
    assert.equal(response.status, 202)
    assert.equal((await beat(d)).result.command.id, result.id)
    await beat(d, snapshot, [{ id: result.id }])
  }
  const failed = await call(path, 'POST', { kind: 'tunnel.start', payload: { mode: 'fixed' }, request_id: randomToken() }, token.headers)
  await beat(d)
  await beat(d, snapshot, [{ id: failed.result.id, error: 'Fixed tunnel is not configured' }])
  const expired = await call(path, 'POST', { kind: 'tunnel.stop', payload: {}, request_id: randomToken() }, token.headers)
  db.prepare('UPDATE commands SET expires_at=? WHERE id=?').run(timestamp() - 1, expired.result.id)
  const history = (await call(path, 'GET', undefined, token.headers)).result.commands
  assert.equal(history.find((c: any) => c.id === failed.result.id).status, 'failed')
  assert.equal(history.find((c: any) => c.id === failed.result.id).error, 'Fixed tunnel is not configured')
  assert.equal(history.find((c: any) => c.id === expired.result.id).status, 'expired')
  assert.equal((await beat(d)).result.command, null)
})
test('Bearer controls validate bodies and preserve retries even at the shared queue limit', async () => {
  const d = await register(), token = await access(), path = '/api/v1/computers/' + d.id + '/commands'
  for (const [kind, payload] of [['shell.exec', {}], ['capability.set', { category: 'full_access', enabled: true }], ['capability.set', { category: ['terminal'], enabled: true }], ['capability.set', { category: 'terminal', enabled: 'true' }], ['control.pause', { paused: 'false' }], ['tunnel.start', { mode: 'unknown' }], ['tunnel.stop', []]]) {
    assert.equal((await call(path, 'POST', { kind, payload, request_id: randomToken() }, token.headers)).response.status, 400)
  }
  for (const request_id of [undefined, '', 'a'.repeat(65)]) assert.equal((await call(path, 'POST', { kind: 'tunnel.stop', payload: {}, request_id }, token.headers)).response.status, 400)
  assert.equal((await call(path, 'POST', { oversized: 'a'.repeat(70000) }, token.headers)).response.status, 413)
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM commands').get()!.n, 0)
  const input = { kind: 'tunnel.stop', payload: {}, request_id: randomToken() }
  const first = await call(path, 'POST', input, token.headers)
  const concurrent = await Promise.all(Array.from({ length: 23 }, (_, index) => call(index % 2 ? '/api/devices/' + d.id + '/commands' : path, 'POST', { ...input, request_id: randomToken() }, index % 2 ? owner() : token.headers)))
  assert.equal(concurrent.filter(r => r.response.status === 202).length, 19)
  assert.equal(concurrent.filter(r => r.response.status === 429).length, 4)
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM commands').get()!.n, 20)
  const retry = await call(path, 'POST', input, token.headers)
  assert.equal(retry.response.status, 202); assert.equal(retry.result.id, first.result.id)
  assert.equal((await call(path, 'POST', { ...input, kind: 'control.pause', payload: { paused: true } }, token.headers)).response.status, 409)
  await call('/api/logout', 'POST', {}, owner())
  assert.equal((await call(path, 'POST', input, token.headers)).response.status, 401)
  assert.equal((await call(path, 'GET', undefined, token.headers)).response.status, 401)
})
