import { type ReactNode, useEffect, useId } from 'react'
import { createPortal } from 'react-dom'
import Icon from './Icon'

interface ModalV2Props {
  open: boolean
  onClose: () => void
  title: string
  children: ReactNode
  /**
   * Max width of the dialog in px. Default 440 (narrow forms). Pass a larger
   * value (e.g. 720) for long-form content like prompts or code blocks.
   */
  width?: number
}

export default function ModalV2({ open, onClose, title, children, width = 440 }: ModalV2Props) {
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

  // Render through a portal anchored at document.body so the modal escapes
  // any ancestor that has `transform` / `filter` / `perspective` applied
  // (e.g. .fade-up's residual translateY from its enter animation, which
  // creates a containing block for `position: fixed` descendants and was
  // clipping the backdrop blur to the QuickStart main-content rectangle
  // — leaving the page header and right margin un-dimmed).
  return createPortal(
    <div
      // Outer wrapper scrolls when the modal is taller than the viewport,
      // so trackpad / wheel users can reach content past the fold. The
      // dialog is centered via flex; padding gives it breathing room from
      // the viewport edges.
      className="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto"
      onClick={onClose}
      style={{ padding: '24px' }}
    >
      {/* Backdrop — `position: fixed` from inside a body-level portal is
          guaranteed to be relative to the viewport, so the blur covers
          the entire page regardless of which component opened the modal. */}
      <div
        aria-hidden="true"
        className="fixed inset-0"
        style={{
          background: 'oklch(0.18 0.012 250 / 0.45)',
          backdropFilter: 'blur(4px)',
          WebkitBackdropFilter: 'blur(4px)',
        }}
      />

      {/* Dialog. Vertical margin (auto top/bottom) keeps it visually centered
          when content fits the viewport but lets it sit at the top with the
          outer wrapper handling scroll when it doesn't. */}
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className="modal-card relative w-full"
        style={{
          maxWidth: width,
          margin: 'auto',
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
          className="active:scale-[0.96]"
          style={{
            position: 'absolute',
            top: 10,
            right: 10,
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            // Bumped from 26×26 — the previous size left a sub-40px tap target
            // that mobile/trackpad users routinely missed. Hit area is now
            // comfortable without crowding the title.
            width: 34,
            height: 34,
            background: 'transparent',
            color: 'var(--text-muted)',
            border: 'none',
            borderRadius: 8,
            cursor: 'pointer',
            transition: 'background 120ms ease, color 120ms ease, transform 120ms cubic-bezier(0.2, 0, 0, 1)',
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
            margin: '0 40px 14px 0',
            fontSize: 17,
            fontWeight: 600,
            letterSpacing: '-0.02em',
            lineHeight: 1.25,
            color: 'var(--text)',
            // Balanced wrapping comes from the root rule in index.css
            // (`@layer base { h1, h2, h3, h4 { text-wrap: balance } }`).
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
    </div>,
    document.body
  )
}
