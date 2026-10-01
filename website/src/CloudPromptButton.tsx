import { useEffect, useRef, useState } from 'react'
import { api } from './cloud-api'
import { agentPrompt } from '../../cloud/src/agent-prompt'
import { Icon } from './components/Icon'
import { useI18n } from './i18n'

type Credential = { token: string; expires_at: number }

export function CloudPromptButton({ disabled }: { disabled: boolean }) {
  const { t, locale } = useI18n()
  const credential = useRef<Credential | null>(null)
  const [status, setStatus] = useState<'idle' | 'busy' | 'copied'>('idle')
  const [error, setError] = useState('')
  const [manualPrompt, setManualPrompt] = useState('')

  useEffect(() => {
    if (status !== 'copied') return
    const timer = setTimeout(() => setStatus('idle'), 3000)
    return () => clearTimeout(timer)
  }, [status])

  async function prompt() {
    if (!credential.current || credential.current.expires_at * 1000 <= Date.now()) {
      credential.current = await api<Credential>('/api/discovery-token', 'POST', {})
    }
    return agentPrompt(location.origin, credential.current.token, locale)
  }

  async function copy() {
    setStatus('busy'); setError(''); setManualPrompt('')
    const generated = prompt()
    try {
      await navigator.clipboard.writeText(await generated)
      setStatus('copied')
    } catch {
      try {
        setManualPrompt(await generated)
        setError(t('复制失败，请选中 Prompt 手动复制'))
      } catch (e) { setError(t((e as Error).message)) }
      setStatus('idle')
    }
  }

  return <div className="cloud-prompt-action">
    <button type="button" className="button button-secondary" disabled={disabled || status === 'busy'} onClick={() => { void copy() }}>
      <Icon name={status === 'copied' ? 'check' : 'copy'} width="15" height="15" />
      <span aria-live="polite">{t(status === 'copied' ? '已复制' : status === 'busy' ? '正在生成…' : '复制云端 Prompt')}</span>
    </button>
    {error && <p className="cloud-error" role="alert">{error}</p>}
    {manualPrompt && <textarea readOnly value={manualPrompt} aria-label={t('云端连接 Prompt')} onFocus={e => e.target.select()} />}
  </div>
}
