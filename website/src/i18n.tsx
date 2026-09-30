import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import en from './locales/en.json'
import { languagePreference, resolveLocale, translate } from './locale'
import type { LanguagePreference, Locale, Translate } from './locale'
export { translateData } from './locale'

const storageKey = 'readyrig-site-language'
const LanguageContext = createContext<{
  locale: Locale
  preference: LanguagePreference
  setPreference: (value: LanguagePreference) => void
  t: Translate
} | null>(null)

function initialPreference(): LanguagePreference {
  const requested = languagePreference(new URLSearchParams(location.search).get('lang'))
  if (requested) return requested
  try {
    return languagePreference(localStorage.getItem(storageKey)) || 'auto'
  } catch {
    return 'auto'
  }
}

export function LanguageProvider({ children }: { children: ReactNode }) {
  const [preference, setLanguage] = useState(initialPreference)
  const [languages, setLanguages] = useState(() => navigator.languages || [navigator.language])
  const locale = resolveLocale(preference, languages)
  const t = useCallback<Translate>((message, values) => translate(message, locale === 'en' ? en : {}, values), [locale])
  const setPreference = useCallback((value: LanguagePreference) => {
    setLanguage(value)
    try {
      localStorage.setItem(storageKey, value)
    } catch {
      /* The selection works for this visit. */
    }
    const url = new URL(location.href)
    url.searchParams.delete('lang')
    history.replaceState(null, '', url)
  }, [])

  useEffect(() => {
    const onSystemLanguage = () => setLanguages(navigator.languages || [navigator.language])
    const onStorage = (event: StorageEvent) => {
      if (event.key === storageKey || event.key === null) setLanguage(languagePreference(event.newValue) || 'auto')
    }
    window.addEventListener('languagechange', onSystemLanguage)
    window.addEventListener('storage', onStorage)
    return () => {
      window.removeEventListener('languagechange', onSystemLanguage)
      window.removeEventListener('storage', onStorage)
    }
  }, [])

  useEffect(() => {
    document.documentElement.lang = locale
    const title = t('ReadyRig — 云端的 Agent，也能用你的电脑')
    const description = t('让云端 Agent 用上你的电脑。ReadyRig 将本机文件、开发环境、浏览器和桌面接给线上 Agent，也能将已开放的工具临时分享给可信任的人。')
    document.title = title
    for (const selector of ['meta[property="og:title"]', 'meta[name="twitter:title"]']) document.querySelector(selector)?.setAttribute('content', title)
    for (const selector of ['meta[name="description"]', 'meta[property="og:description"]', 'meta[name="twitter:description"]'])
      document.querySelector(selector)?.setAttribute('content', description)
    document.querySelector('meta[property="og:locale"]')?.setAttribute('content', locale === 'en' ? 'en_US' : 'zh_CN')
  }, [locale, t])

  const value = useMemo(() => ({ locale, preference, setPreference, t }), [locale, preference, setPreference, t])
  return <LanguageContext.Provider value={value}>{children}</LanguageContext.Provider>
}

export function useI18n() {
  const context = useContext(LanguageContext)
  if (!context) throw new Error('LanguageProvider is required')
  return context
}

export function LanguageSelect() {
  const { t, preference, setPreference } = useI18n()
  return (
    <select
      className="language-select"
      aria-label={t('语言')}
      value={preference}
      onChange={(event) => {
        const next = languagePreference(event.target.value)
        if (next) setPreference(next)
      }}
    >
      <option value="auto">{t('跟随系统')}</option>
      <option value="zh-CN">简体中文</option>
      <option value="en">English</option>
    </select>
  )
}
