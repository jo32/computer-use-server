import de from './de.json'
import en from './en.json'
import es from './es.json'
import fr from './fr.json'
import ja from './ja.json'
import ko from './ko.json'
import zhTW from './zh-TW.json'
import type { Locale } from '../locale'

// The page source is Simplified Chinese, so zh-CN needs no catalog: messages are their own keys.
export const catalogs: Record<Locale, Readonly<Record<string, string>>> = {
  'zh-CN': {},
  'zh-TW': zhTW,
  en,
  ja,
  ko,
  es,
  fr,
  de,
}
