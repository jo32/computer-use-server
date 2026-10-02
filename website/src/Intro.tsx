import { useI18n } from './i18n'
import Film from './intro/Film'

export default function Intro() {
  const { t } = useI18n()

  return (
    <div className="intro-page">
      <nav className="intro-bar" aria-label={t('主导航')}>
        <a href="/">← {t('返回官网')}</a>
      </nav>
      <Film />
    </div>
  )
}
