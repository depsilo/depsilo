import { type ReactNode, useEffect } from 'react'
import Icon from './Icon'

interface ModalV2Props {
  open: boolean
  onClose: () => void
  title: string
  children: ReactNode
}

export default function ModalV2({ open, onClose, title, children }: ModalV2Props) {
  useEffect(() => {
    if (open) {
      document.body.style.overflow = 'hidden'
    } else {
      document.body.style.overflow = ''
    }
    return () => { document.body.style.overflow = '' }
  }, [open])

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center" onClick={onClose}>
      <div className="fixed inset-0 bg-black/40 backdrop-blur-sm" />
      <div
        className="relative mt-[20vh] w-full max-w-md rounded-[8px] p-6"
        style={{
          background: 'var(--bg-card)',
          border: '1px solid var(--border)',
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <button
          onClick={onClose}
          className="absolute top-4 right-4 bg-transparent cursor-pointer transition-colors duration-150 p-1 rounded-[4px]"
          style={{ color: 'var(--text-soft)' }}
          onMouseEnter={(e) => { e.currentTarget.style.color = 'var(--text)' }}
          onMouseLeave={(e) => { e.currentTarget.style.color = 'var(--text-soft)' }}
        >
          <Icon name="close" size="sm" />
        </button>
        <h2 className="text-[18px] font-[300] mb-4" style={{ color: 'var(--text)' }}>{title}</h2>
        {children}
      </div>
    </div>
  )
}
