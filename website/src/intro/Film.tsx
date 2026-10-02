import { useEffect, useRef } from 'react'
import { useI18n } from '../i18n'
import { filmLabels, translateFilm } from './film-text'
import { mountIntro } from './player'
import stage from './stage.html?raw'
import './intro.css'

/** The animated Macintosh film, shared by the /intro page and the home page hero. Its pixel fonts load from index.html. */
export default function Film() {
  const { locale } = useI18n()
  const film = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const root = film.current
    if (!root) return
    translateFilm(root, locale)
    return mountIntro(root, filmLabels(locale))
  }, [locale])

  return (
    <div className="film">
      {/* The markup is static; the key gives a language change a fresh copy to translate. mountIntro only animates it. */}
      <div key={locale} className="wrap" ref={film} dangerouslySetInnerHTML={{ __html: stage }} />
    </div>
  )
}
