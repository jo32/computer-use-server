import { useEffect, useRef, useState } from 'react'
import { Icon } from './Icon'

export function CopyButton({ text, label = '复制配置' }: { text: string; label?: string }) {
  const [status, setStatus] = useState<'idle' | 'copied' | 'error'>('idle')
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  useEffect(() => () => clearTimeout(timer.current), [])

  async function copy() {
    clearTimeout(timer.current)
    try {
      await navigator.clipboard.writeText(text)
      setStatus('copied')
    } catch {
      setStatus('error')
    }
    timer.current = setTimeout(() => setStatus('idle'), 3000)
  }

  return <button type="button" className="copy-button" onClick={() => { void copy() }}>
    <Icon name={status === 'copied' ? 'check' : 'copy'} width="15" height="15" />
    <span aria-live="polite">{status === 'copied' ? '已复制' : status === 'error' ? '请选中文本复制' : label}</span>
  </button>
}
