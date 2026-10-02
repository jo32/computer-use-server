import type { Locale } from '../locale.ts'
import { filmStrings } from './film-strings.ts'

export type FilmLabels = { play: string; pause: string; replay: string }

/** Looks up one piece of film text; English is the source language and anything unlisted stays as written. */
export function filmText(locale: Locale, source: string): string {
  if (locale === 'en' || !Object.hasOwn(filmStrings, source)) return source
  return filmStrings[source][locale]
}

export function filmLabels(locale: Locale): FilmLabels {
  return { play: filmText(locale, 'Play'), pause: filmText(locale, 'Pause'), replay: filmText(locale, 'Replay') }
}

// Wide characters take two columns of the monospace typing effect, which is sized in `ch` units.
const WIDE = /[ᄀ-ᅟ⺀-꓏가-힣豈-﫿︰-﹯＀-｠￠-￦]/u
export function typedColumns(text: string): number {
  let columns = 0
  for (const character of text) columns += WIDE.test(character) ? 2 : 1
  return columns
}

const attributes = ['alt', 'aria-label', 'data-ch']

/** Translates the film's static markup in place: text nodes, image descriptions, chapter names and typed lines. */
export function translateFilm(root: HTMLElement, locale: Locale): void {
  if (locale === 'en') return
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT)
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    const [, before, source, after] = /^(\s*)([\s\S]*?)(\s*)$/.exec(node.nodeValue ?? '')!
    if (!source) continue
    const text = filmText(locale, source)
    if (text === source) continue
    node.nodeValue = before + text + after
    const typed = node.parentElement?.closest<HTMLElement>('.type')
    typed?.style.setProperty('--n', String(typedColumns(text)))
  }
  for (const element of root.querySelectorAll<HTMLElement>('[alt], [aria-label], [data-ch]')) {
    for (const name of attributes) {
      const value = element.getAttribute(name)
      if (value) element.setAttribute(name, filmText(locale, value))
    }
  }
}
