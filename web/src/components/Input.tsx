import { type InputHTMLAttributes } from 'react'

interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  mono?: boolean
}

export default function Input({ className = '', mono, ...rest }: InputProps) {
  return (
    <input
      className={`w-full bg-surface-low rounded-[0.125rem] px-3 py-2.5 text-base text-on-surface placeholder:text-on-surface-variant border-b-2 border-transparent focus:border-b-2 focus:border-primary focus:outline-none transition-colors ${mono ? 'font-mono' : ''} ${className}`}
      {...rest}
    />
  )
}
