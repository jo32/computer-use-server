import { StrictMode, Suspense, lazy } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'
import CloudConsole from './CloudConsole'
import { LanguageProvider } from './i18n'
import './styles.css'

// The intro film carries its own markup, styles and fonts, so it loads only when someone opens /intro.
const Intro = lazy(() => import('./Intro'))
const path = location.pathname.replace(/\/$/, '')

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <LanguageProvider>
      {path === '/console' ? <CloudConsole /> : path === '/intro' ? <Suspense fallback={null}><Intro /></Suspense> : <App />}
    </LanguageProvider>
  </StrictMode>,
)
