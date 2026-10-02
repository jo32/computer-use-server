export const locales = ['zh-CN', 'zh-TW', 'en', 'ja', 'ko', 'es', 'fr', 'de'] as const
export type Locale = (typeof locales)[number]
export type LanguagePreference = Locale | 'auto'
export type Translate = (message: string, values?: Record<string, string | number>) => string

// Each name is written in its own language so visitors can always find theirs.
export const localeNames: Record<Locale, string> = {
  'zh-CN': '简体中文',
  'zh-TW': '繁體中文',
  en: 'English',
  ja: '日本語',
  ko: '한국어',
  es: 'Español',
  fr: 'Français',
  de: 'Deutsch',
}

export const openGraphLocales: Record<Locale, string> = {
  'zh-CN': 'zh_CN',
  'zh-TW': 'zh_TW',
  en: 'en_US',
  ja: 'ja_JP',
  ko: 'ko_KR',
  es: 'es_ES',
  fr: 'fr_FR',
  de: 'de_DE',
}

function isLocale(value: string): value is Locale {
  return (locales as readonly string[]).includes(value)
}

export function languagePreference(value: string | null): LanguagePreference | null {
  if (value === 'auto') return value
  return value !== null && isLocale(value) ? value : null
}

function matchLocale(language: string): Locale | null {
  if (/^zh-(?:tw|hk|mo|hant)(?:-|$)/i.test(language)) return 'zh-TW'
  if (/^zh(?:-|$)/i.test(language)) return 'zh-CN'
  const base = language.split('-')[0].toLowerCase()
  return isLocale(base) ? base : null
}

export function resolveLocale(preference: LanguagePreference, languages: readonly string[]): Locale {
  if (preference !== 'auto') return preference
  for (const language of languages) {
    const match = matchLocale(language)
    if (match) return match
  }
  return 'en'
}

export function translate(message: string, catalog: Readonly<Record<string, string>>, values?: Record<string, string | number>): string {
  const text = Object.hasOwn(catalog, message) ? catalog[message] : message
  // Only replace numbered parameters supplied by the caller; JSON braces remain intact.
  return text.replace(/\{(\d+)\}/g, (token, key: string) => (values && Object.hasOwn(values, key) ? String(values[key]) : token))
}

// Used exclusively for the site's static demonstration data, never real user content.
export function translateData<T>(value: T, t: Translate): T {
  if (typeof value === 'string') return t(value) as T
  if (Array.isArray(value)) return value.map((item: unknown) => translateData(item, t)) as T
  if (value && typeof value === 'object') return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, translateData(item, t)])) as T
  return value
}
