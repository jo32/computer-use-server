import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'
import CloudConsole from './CloudConsole'
import { LanguageProvider } from './i18n'
import './styles.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode><LanguageProvider>{location.pathname === '/console' || location.pathname === '/console/' ? <CloudConsole /> : <App />}</LanguageProvider></StrictMode>,
)
