import assert from 'node:assert/strict'
import { test } from 'node:test'
import { languagePreference, resolveLocale, translate, translateData } from '../src/locale.ts'

test('automatic selection uses supported browser languages in order', () => {
  assert.equal(resolveLocale('auto', ['de-DE', 'zh-TW', 'en-US']), 'zh-CN')
  assert.equal(resolveLocale('auto', ['en-GB', 'zh-CN']), 'en')
  assert.equal(resolveLocale('auto', ['fr-FR']), 'en')
  assert.equal(resolveLocale('zh-CN', ['en-US']), 'zh-CN')
  assert.equal(languagePreference('zh-CN'), 'zh-CN')
  assert.equal(languagePreference('unsupported'), null)
})

test('interpolation allows parameter reordering without changing JSON or inserted text', () => {
  assert.equal(translate('example', { example: '{1}: {0}' }, { 0: '$& {1}', 1: 'Result' }), 'Result: $& {1}')
  const json = '{"arguments":{},"count":0}'
  assert.equal(translate(json, {}, { 0: 'value' }), json)
  assert.equal(translate('missing {0}', {}, { 0: 'value' }), 'missing value')
  assert.equal(translate('toString', {}), 'toString')
})

test('static demo data translates without changing identifiers or mutating the source', () => {
  const source = [{ id: 'files', label: '文件', args: { path: 'src/App.tsx', enabled: true } }]
  const result = translateData(source, (message) => translate(message, { 文件: 'Files' }))
  assert.equal(result[0].label, 'Files')
  assert.equal(result[0].id, 'files')
  assert.equal(result[0].args.path, 'src/App.tsx')
  assert.equal(result[0].args.enabled, true)
  assert.equal(source[0].label, '文件')
})
