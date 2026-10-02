import { useEffect, useRef } from 'react'
import { useI18n } from './i18n'
import { mountIntro } from './intro/player'
import stage from './intro/stage.html?raw'
import './intro/intro.css'

// The film uses two pixel fonts that the rest of the site does not need, so they load only on this page.
const fonts = 'https://fonts.googleapis.com/css2?family=Pixelify+Sans:wght@400;600;700&family=VT323&display=swap'

export default function Intro() {
  const { t } = useI18n()
  const film = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const link = document.createElement('link')
    link.rel = 'stylesheet'
    link.href = fonts
    document.head.append(link)
    const stop = film.current ? mountIntro(film.current) : () => {}
    return () => {
      stop()
      link.remove()
    }
  }, [])

  return (
    <div className="intro-page">
      <nav className="intro-bar" aria-label={t('主导航')}>
        <a href="/">← {t('返回官网')}</a>
      </nav>
      {/* The markup is static; mountIntro only animates it and never re-renders it. */}
      <div className="wrap" ref={film} dangerouslySetInnerHTML={{ __html: stage }} />
      <p className="intro-note">{t('短片的画面与字幕为英文。')}</p>
    </div>
  )
}
