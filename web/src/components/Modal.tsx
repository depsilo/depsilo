import { type ReactNode, useEffect } from 'react'
import Icon from './Icon'

interface ModalProps {
  open: boolean
  onClose: () => void
  title: string
  children: ReactNode
}

export default function Modal({ open, onClose, title, children }: ModalProps) {
  useEffect(() => {
    if (open) {
      document.body.style.overflow = 'hidden'
    } else {
      document.body.style.overflow = ''
    }
    return () => {
      document.body.style.overflow = ''
    }
  }, [open])

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center" onClick={onClose}>
      <div className="fixed inset-0 bg-black/50 backdrop-blur-sm" />
      <div
        className="relative mt-[20vh] w-full max-w-md bg-surface-container/90 backdrop-blur-xl border border-outline-variant/15 rounded-[0.5rem] p-6"
        onClick={(e) => e.stopPropagation()}
      >
        <button
          onClick={onClose}
          className="absolute top-4 right-4 bg-transparent text-on-surface-variant hover:text-on-surface cursor-pointer transition-colors"
        >
          <Icon name="close" size="sm" />
        </button>
        <h2 className="text-lg font-medium mb-4 text-on-surface">{title}</h2>
        {children}
      </div>
    </div>
  )
}
