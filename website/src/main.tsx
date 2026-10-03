import { StrictMode, Suspense, lazy } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'
import CloudConsole from './CloudConsole'
import { LanguageProvider } from './i18n'
import './styles.css'
import './retro.css'

// The intro film carries its own markup, styles and fonts, so it loads only when someone opens /intro.
const Intro = lazy(() => import('./Intro'))
const path = location.pathname.replace(/\/$/, '')

// Keep local development and authentication pages out of public web analytics.
if (import.meta.env.PROD && location.hostname === 'readyrig.getmegaportal.com' && ['', '/intro', '/console'].includes(path)) {
  const beacon = document.createElement('script')
  beacon.type = 'module'
  beacon.src = 'https://static.cloudflareinsights.com/beacon.min.js'
  beacon.dataset.cfBeacon = JSON.stringify({ token: 'aa8efd18ef81499cb06b2cd8698ec58f' })
  document.body.appendChild(beacon)
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <LanguageProvider>
      {path === '/console' ? <CloudConsole /> : path === '/intro' ? <Suspense fallback={null}><Intro /></Suspense> : <App />}
    </LanguageProvider>
  </StrictMode>,
)
