import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { runInNewContext } from 'node:vm'
import { publicConnectionPrompt } from '../src/connection-prompt.ts'
import { translate } from '../src/locale.ts'
import type { Translate } from '../src/locale.ts'

const en = JSON.parse(readFileSync(new URL('../src/locales/en.json', import.meta.url), 'utf8'))
const appSource = readFileSync(new URL('../../internal/server/assets/app.js', import.meta.url), 'utf8')
const appFunction = appSource.slice(appSource.indexOf('function connectionPrompt('), appSource.indexOf('\nfunction renderConnection()'))
const gateway = 'https://example.trycloudflare.com/AbC123xy'

for (const locale of ['zh-CN', 'en']) {
  for (const mode of ['quick', 'fixed']) {
    test(`${locale} ${mode} public prompt matches the app and preserves the complete tool URLs`, () => {
      const t: Translate = (message, values) => translate(message, locale === 'en' ? en : {}, values)
      const desktop = runInNewContext(`${appFunction}\nconnectionPrompt`, { t, PUBLIC_VIEW: true })
      const prompt = publicConnectionPrompt(gateway, mode, t)
      assert.equal(prompt, desktop(gateway, 'public', mode))
      for (const route of ['/api/v1/tools/help', '/api/v1/tools/list_projects', '/mcp']) {
        assert.ok(prompt.includes(gateway + route))
      }
      assert.ok(prompt.includes('session_id'))
      assert.ok(prompt.includes('body {}') || prompt.includes('请求体 {}'))
      assert.ok(!/\{\d+\}/.test(prompt))
      assert.ok(prompt.endsWith(locale === 'en' ? 'My task: ...' : '我想要：...'))
    })
  }
}

test('a refreshed gateway replaces every connection URL and missing mode uses temporary-link guidance', () => {
  const t: Translate = (message, values) => translate(message, en, values)
  const newGateway = 'https://new.example.com/ZyX987'
  const prompt = publicConnectionPrompt(newGateway, undefined, t)
  assert.ok(prompt.includes('temporary public URL'))
  assert.ok(prompt.includes(newGateway + '/api/v1/tools/help'))
  assert.ok(!prompt.includes(gateway))
})
