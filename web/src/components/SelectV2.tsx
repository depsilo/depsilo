import { type SelectHTMLAttributes } from 'react'

interface SelectV2Props extends SelectHTMLAttributes<HTMLSelectElement> {
  label?: string
}

export default function SelectV2({ className = '', label, children, ...rest }: SelectV2Props) {
  const select = (
    <select
      className={`w-full rounded-[4px] px-3 py-2 text-[16px] transition-colors duration-150 cursor-pointer stripe-focus-ring ${className}`}
      style={{
        background: 'var(--surface)',
        border: '1px solid var(--border)',
        color: 'var(--heading)',
        outline: 'none',
      }}
      {...rest}
    >
      {children}
    </select>
  )

  if (label) {
    return (
      <div>
        <label className="block text-[14px] font-[400] mb-1" style={{ color: 'var(--label)' }}>
          {label}
        </label>
        {select}
      </div>
    )
  }

  return select
}
