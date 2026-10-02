import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { filmStrings } from '../src/intro/film-strings.ts'
import { filmLabels, filmText, typedColumns } from '../src/intro/film-text.ts'
import { locales } from '../src/locale.ts'

const stage = readFileSync(new URL('../src/intro/stage.html', import.meta.url), 'utf8')

test('every film string is translated into every non-English locale', () => {
  for (const [source, entry] of Object.entries(filmStrings)) {
    for (const locale of locales.filter((code) => code !== 'en')) {
      assert.ok(entry[locale as keyof typeof entry]?.trim(), `${source} / ${locale}`)
    }
  }
})

test('film text keeps English as the source and falls back to it', () => {
  assert.equal(filmText('en', 'Cancel'), 'Cancel')
  assert.equal(filmText('de', 'Cancel'), 'Abbrechen')
  assert.equal(filmText('ja', 'readyrig.getmegaportal.com'), 'readyrig.getmegaportal.com')
  assert.equal(filmText('fr', 'toString'), 'toString')
  assert.deepEqual(filmLabels('zh-CN'), { play: '播放', pause: '暂停', replay: '重播' })
})

test('typed lines are sized in columns, counting wide characters twice', () => {
  assert.equal(typedColumns('go test'), 7)
  assert.equal(typedColumns('你的电脑'), 8)
})

test('the film markup still contains the text the strings translate', () => {
  const player = readFileSync(new URL('../src/intro/player.ts', import.meta.url), 'utf8')
  const markup = stage.replaceAll('&nbsp;', ' ').replaceAll(' ', ' ') + player
  const missing = Object.keys(filmStrings).filter((source) => !markup.includes(source) && !['Play', 'Pause', 'Replay'].includes(source))
  assert.deepEqual(missing, [])
})
