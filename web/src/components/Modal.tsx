import { type ReactNode, useEffect, useId } from 'react'
import Icon from './Icon'

interface ModalV2Props {
  open: boolean
  onClose: () => void
  title: string
  children: ReactNode
}

export default function ModalV2({ open, onClose, title, children }: ModalV2Props) {
  const titleId = useId()

  useEffect(() => {
    if (!open) return
    document.body.style.overflow = 'hidden'
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => {
      document.body.style.overflow = ''
      window.removeEventListener('keydown', onKey)
    }
  }, [open, onClose])

  if (!open) return null

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center"
      onClick={onClose}
      style={{ padding: '24px' }}
    >
      {/* Backdrop */}
      <div
        aria-hidden="true"
        className="fixed inset-0"
        style={{
          background: 'oklch(0.18 0.012 250 / 0.45)',
          backdropFilter: 'blur(4px)',
          WebkitBackdropFilter: 'blur(4px)',
        }}
      />

      {/* Dialog */}
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className="modal-card relative w-full"
        style={{
          maxWidth: 440,
          background: 'var(--bg-card)',
          border: '0.5px solid var(--border)',
          borderRadius: 14,
          boxShadow: 'var(--shadow-pop)',
          padding: '22px 22px 20px',
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <button
          onClick={onClose}
          aria-label="Close"
          style={{
            position: 'absolute',
            top: 14,
            right: 14,
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            width: 26,
            height: 26,
            background: 'transparent',
            color: 'var(--text-muted)',
            border: 'none',
            borderRadius: 6,
            cursor: 'pointer',
            transition: 'background 120ms ease, color 120ms ease',
          }}
          onMouseEnter={(e) => {
            e.currentTarget.style.background = 'var(--bg-hover)'
            e.currentTarget.style.color = 'var(--text)'
          }}
          onMouseLeave={(e) => {
            e.currentTarget.style.background = 'transparent'
            e.currentTarget.style.color = 'var(--text-muted)'
          }}
        >
          <Icon name="close" size="sm" />
        </button>

        <h2
          id={titleId}
          style={{
            margin: '0 36px 14px 0',
            fontSize: 17,
            fontWeight: 600,
            letterSpacing: '-0.02em',
            lineHeight: 1.25,
            color: 'var(--text)',
          }}
        >
          {title}
        </h2>

        {children}
      </div>

      <style>{`
        @keyframes modalPop {
          from { opacity: 0; transform: translateY(6px) scale(0.985); }
          to   { opacity: 1; transform: translateY(0) scale(1); }
        }
        .modal-card { animation: modalPop 160ms cubic-bezier(0.2, 0.8, 0.2, 1); }
        @media (prefers-reduced-motion: reduce) {
          .modal-card { animation: none; }
        }
      `}</style>
    </div>
  )
}
