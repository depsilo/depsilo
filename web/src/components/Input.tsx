import { type InputHTMLAttributes } from 'react'

interface InputV2Props extends InputHTMLAttributes<HTMLInputElement> {
  mono?: boolean
  label?: string
}

export default function InputV2({ className = '', mono, label, ...rest }: InputV2Props) {
  const input = (
    <input
      className={`w-full rounded-[4px] px-3 py-2 text-[16px] transition-colors duration-150 stripe-focus-ring ${mono ? 'font-mono' : ''} ${className}`}
      style={{
        background: 'var(--bg-card)',
        border: '1px solid var(--border)',
        color: 'var(--text)',
        outline: 'none',
      }}
      onFocus={(e) => {
        e.currentTarget.style.borderColor = 'var(--brand)'
        rest.onFocus?.(e)
      }}
      onBlur={(e) => {
        e.currentTarget.style.borderColor = 'var(--border)'
        rest.onBlur?.(e)
      }}
      {...rest}
    />
  )

  if (label) {
    return (
      <div>
        <label className="block text-[14px] font-[400] mb-1" style={{ color: 'var(--text-muted)' }}>
          {label}
        </label>
        {input}
      </div>
    )
  }

  return input
}
