import { useId, useRef } from 'react'
import type { ReactNode } from 'react'

type TabOption<T extends string> = { value: T; label: ReactNode }

export function Tabs<T extends string>({ label, options, value, onChange, children, className = '' }: {
  label: string
  options: TabOption<T>[]
  value: T
  onChange: (value: T) => void
  children: ReactNode
  className?: string
}) {
  const id = useId()
  const buttons = useRef<(HTMLButtonElement | null)[]>([])

  return <div className={className}>
    <div className="segmented" role="tablist" aria-label={label}>
      {options.map((option, index) => <button
        key={option.value}
        ref={(element) => { buttons.current[index] = element }}
        id={`${id}-${option.value}`}
        role="tab"
        type="button"
        aria-selected={value === option.value}
        aria-controls={`${id}-panel`}
        tabIndex={value === option.value ? 0 : -1}
        onClick={() => onChange(option.value)}
        onKeyDown={(event) => {
          let next: number | undefined
          if (event.key === 'ArrowRight') next = (index + 1) % options.length
          if (event.key === 'ArrowLeft') next = (index - 1 + options.length) % options.length
          if (event.key === 'Home') next = 0
          if (event.key === 'End') next = options.length - 1
          if (next !== undefined) {
            event.preventDefault()
            onChange(options[next].value)
            buttons.current[next]?.focus()
          }
        }}
      >{option.label}</button>)}
    </div>
    <div id={`${id}-panel`} role="tabpanel" aria-labelledby={`${id}-${value}`} tabIndex={0}>{children}</div>
  </div>
}
