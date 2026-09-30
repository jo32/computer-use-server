// An isolated integration fixture: only local D1, a generated test account, and
// a harmless connector process. This never opens a real public tunnel.
import { spawn, execFileSync } from 'node:child_process'
import { createServer } from 'node:http'
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { createHash, randomBytes, randomUUID } from 'node:crypto'
import assert from 'node:assert/strict'

const root = resolve(import.meta.dirname, '..'), cloud = join(root, 'cloud')
const dir = mkdtempSync(join(tmpdir(), 'readyrig-cloud-test-')), children = []
const workerOrigin = 'http://localhost:18787', appOrigin = 'http://127.0.0.1:17431'
const session = randomBytes(32).toString('base64url'), sessionHash = createHash('sha256').update(session).digest('hex')
const wrangler = join(cloud, 'node_modules/wrangler/bin/wrangler.js'), persist = join(dir, 'worker')
let appKey, helper, deviceID
const delay = ms => new Promise(resolve => setTimeout(resolve, ms))
async function waitFor(fn, label, timeout = 45000) {
  const deadline = Date.now() + timeout
  while (Date.now() < deadline) { try { const result = await fn(); if (result) return result } catch {} await delay(300) }
  throw new Error('Timed out: ' + label)
}
function start(command, args, options = {}) {
  const child = spawn(command, args, { cwd: root, detached: process.platform !== 'win32', stdio: ['ignore', 'pipe', 'pipe'], ...options }); children.push(child); return child
}
function wranglerSync(args) { return execFileSync(process.execPath, [wrangler, ...args], { cwd: cloud, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }) }
async function request(origin, path, method = 'GET', data, headers = {}) {
  const response = await fetch(origin + path, { method, headers: { ...headers, ...(data === undefined ? {} : { 'Content-Type': 'application/json' }) }, body: data === undefined ? undefined : JSON.stringify(data) })
  const result = await response.json()
  assert.ok(response.ok, `HTTP ${response.status}: ${result.error || path}`); return result
}
const owner = { Cookie: 'readyrig_session=' + session, Origin: workerOrigin }
let localCookie
const local = (path, method = 'GET', data) => request(appOrigin, path, method, data, { Cookie: localCookie, Origin: appOrigin })
const remote = (path, method = 'GET', data) => request(workerOrigin, path, method, data, owner)
try {
  wranglerSync(['d1', 'migrations', 'apply', 'DB', '--local', '--persist-to', persist])
  const seed = `INSERT INTO users VALUES('integration-user','integration@example.invalid','Integration Test',unixepoch()); INSERT INTO sessions VALUES('${sessionHash}','integration-user',unixepoch()+3600);`
  const seedPath = join(dir, 'seed.sql'); writeFileSync(seedPath, seed, { mode: 0o600 })
  wranglerSync(['d1', 'execute', 'DB', '--local', '--persist-to', persist, '--file', seedPath])
  const worker = start(process.execPath, [wrangler, 'dev', '--port', '18787', '--persist-to', persist, '--var', 'PUBLIC_ORIGIN:' + workerOrigin], { cwd: cloud })
  let workerLog = ''; worker.stdout.on('data', data => { workerLog += data }); worker.stderr.on('data', data => { workerLog += data })
  await waitFor(() => request(workerOrigin, '/api/health'), 'Worker startup')
  const binary = join(dir, 'readyrig'); execFileSync('go', ['build', '-tags', 'nogui', '-o', binary, './cmd/adapter'], { cwd: root, stdio: 'pipe' })
  const connector = join(dir, 'connector'); writeFileSync(connector, "#!/bin/sh\necho 'https://readyrig-integration-fixture.trycloudflare.com'\necho 'INF Registered tunnel connection'\nexec sleep 600\n", { mode: 0o700 })
  mkdirSync(join(dir, 'workspace'))
  const app = start(binary, ['web', '--workspace', join(dir, 'workspace'), '--data-dir', join(dir, 'data'), '--ui', '127.0.0.1:17431', '--gateway', '127.0.0.1:17432', '--cloud-url', workerOrigin, '--cloudflared', connector, '--no-chrome', '--no-update'])
  app.stdout.on('data', data => { const match = String(data).match(/#key=([a-f0-9]+)/); if (match) appKey = match[1] })
  await waitFor(() => appKey, 'App startup')
  const login = await fetch(appOrigin + '/api/login', { method: 'POST', headers: { 'Content-Type': 'application/json', Origin: appOrigin }, body: JSON.stringify({ key: appKey }) })
  assert.equal(login.status, 200); localCookie = login.headers.get('set-cookie').split(';')[0]
  const pair = await local('/api/cloud/login', 'POST', { url: workerOrigin, name: 'Integration Mac' })
  const pairID = new URL(pair.login_url).searchParams.get('pair')
  assert.equal((await remote('/api/pairings/' + pairID)).code, pair.code)
  await remote('/api/pairings/' + pairID, 'POST', {})
  await waitFor(async () => { const status = await local('/api/cloud'); deviceID = status.device_id; return status.state === 'online' }, 'Device registration')
  assert.equal((await remote('/api/devices')).devices[0].online, true)
  console.log('PASS: app login → device registration → real D1 heartbeat')
  const commandPath = '/api/devices/' + deviceID + '/commands'
  async function command(kind, payload, check) {
    const cmd = await remote(commandPath, 'POST', { kind, payload, request_id: randomUUID() })
    await waitFor(async () => { const history = await remote(commandPath); const result = history.commands.find(c => c.id === cmd.id); if (result?.status === 'failed') throw new Error(result.error); return result?.status === 'completed' }, kind)
    await check(await local('/api/state'))
    console.log('PASS: ' + kind + ' → app execution → cloud receipt')
  }
  await command('tunnel.start', { mode: 'quick' }, state => assert.equal(state.tunnel.state, 'ready'))
  await command('tunnel.stop', {}, state => assert.equal(state.tunnel.state, 'stopped'))
  await command('capability.set', { category: 'terminal', enabled: true }, state => assert.equal(state.enabled.terminal, true))
  await command('control.pause', { paused: true }, state => assert.equal(state.paused, true))
  await remote('/api/devices/' + deviceID, 'PATCH', { name: 'Renamed Integration Mac' })
  await waitFor(async () => (await local('/api/cloud')).name === 'Renamed Integration Mac', 'Rename heartbeat')
  console.log('PASS: device rename reflected in app')
  helper = createServer((req, res) => {
    if (req.url === '/app') { res.writeHead(302, { Location: appOrigin + '/#key=' + appKey }); res.end(); return }
    res.writeHead(302, { 'Set-Cookie': `readyrig_session=${session}; Path=/; HttpOnly; SameSite=Lax; Max-Age=3600`, Location: workerOrigin + '/console?lang=zh-CN' }); res.end()
  }).listen(18789, 'localhost')
  console.log('Browser fixtures: http://localhost:18789/cloud and http://localhost:18789/app')
  if (process.argv.includes('--keep')) { console.log('Fixtures kept for visual verification. Stop to clean up.'); await new Promise(resolve => { process.once('SIGINT', resolve); process.once('SIGTERM', resolve) }) }
  await remote('/api/devices/' + deviceID, 'DELETE')
  await waitFor(async () => (await local('/api/cloud')).state === 'revoked', 'Credential revocation')
  console.log('PASS: unbinding revokes device credentials')
} finally {
  helper?.close()
  for (const child of children.reverse()) { try { if (process.platform === 'win32') child.kill('SIGTERM'); else process.kill(-child.pid, 'SIGTERM') } catch {} }
  await delay(1000)
  rmSync(dir, { recursive: true, force: true })
}
