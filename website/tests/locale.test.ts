import assert from 'node:assert/strict'
import { test } from 'node:test'
import { languagePreference, localeNames, locales, openGraphLocales, resolveLocale, translate, translateData } from '../src/locale.ts'

test('automatic selection uses supported browser languages in order', () => {
  assert.equal(resolveLocale('auto', ['pt-BR', 'zh-TW', 'en-US']), 'zh-TW')
  assert.equal(resolveLocale('auto', ['en-GB', 'zh-CN']), 'en')
  assert.equal(resolveLocale('auto', ['fr-CA']), 'fr')
  assert.equal(resolveLocale('auto', ['pt-BR', 'ru']), 'en')
  assert.equal(resolveLocale('zh-CN', ['en-US']), 'zh-CN')
  assert.equal(languagePreference('zh-CN'), 'zh-CN')
  assert.equal(languagePreference('unsupported'), null)
  assert.equal(languagePreference(null), null)
})

test('Chinese variants map to the matching script and every locale is selectable', () => {
  for (const tag of ['zh-TW', 'zh-HK', 'zh-MO', 'zh-Hant', 'zh-Hant-TW']) assert.equal(resolveLocale('auto', [tag]), 'zh-TW')
  for (const tag of ['zh', 'zh-CN', 'zh-SG', 'zh-Hans']) assert.equal(resolveLocale('auto', [tag]), 'zh-CN')
  for (const [tag, locale] of [['ja-JP', 'ja'], ['ko-KR', 'ko'], ['es-MX', 'es'], ['de-AT', 'de']]) assert.equal(resolveLocale('auto', [tag]), locale)
  for (const locale of locales) assert.equal(languagePreference(locale), locale)
  assert.equal(Object.keys(localeNames).length, locales.length)
  assert.equal(Object.keys(openGraphLocales).length, locales.length)
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
