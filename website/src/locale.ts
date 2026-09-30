export type Locale = 'zh-CN' | 'en'
export type LanguagePreference = Locale | 'auto'
export type Translate = (message: string, values?: Record<string, string | number>) => string

export function languagePreference(value: string | null): LanguagePreference | null {
  return value === 'auto' || value === 'zh-CN' || value === 'en' ? value : null
}

export function resolveLocale(preference: LanguagePreference, languages: readonly string[]): Locale {
  if (preference !== 'auto') return preference
  for (const language of languages) {
    if (/^zh(?:-|$)/i.test(language)) return 'zh-CN'
    if (/^en(?:-|$)/i.test(language)) return 'en'
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
