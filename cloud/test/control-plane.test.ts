import { test, beforeEach } from 'node:test'
import assert from 'node:assert/strict'
import { DatabaseSync } from 'node:sqlite'
import { readFileSync } from 'node:fs'
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
  db.exec(readFileSync(new URL('../migrations/0001_control_plane.sql', import.meta.url), 'utf8'))
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
