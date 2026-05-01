// web/src/portal/pages/QuickStart.tsx
import { useState } from 'react'
import StatusDot from '@/components/StatusDot'
import LanguageRail from '@/portal/components/LanguageRail'
import ConfigurePane from '@/portal/components/ConfigurePane'
import AllInOnePane from '@/portal/components/AllInOnePane'

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)
  function copy() {
    navigator.clipboard.writeText(text).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }).catch(() => {})
  }
  return (
    <button
      type="button"
      onClick={copy}
      style={{
        fontSize: 11,
        color: copied ? 'var(--ok-text)' : 'var(--text-muted)',
        padding: '3px 8px',
        border: '0.5px solid var(--border)',
        borderRadius: 4,
        cursor: 'pointer',
        flexShrink: 0,
        background: 'transparent',
      }}
    >
      {copied ? 'Copied' : 'Copy'}
    </button>
  )
}

function EndpointInline({ endpoint }: { endpoint: string }) {
  return (
    <div
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 8,
        padding: '4px 6px 4px 10px',
        background: 'var(--bg-soft)',
        border: '0.5px solid var(--border)',
        borderRadius: 6,
        flexShrink: 0,
      }}
    >
      <StatusDot status="healthy" live />
      <span
        className="mono"
        style={{
          fontSize: 12,
          color: 'var(--text)',
          letterSpacing: '-0.02em',
          whiteSpace: 'nowrap',
        }}
      >
        {endpoint}
      </span>
      <CopyButton text={endpoint} />
    </div>
  )
}

export default function QuickStart() {
  const endpoint = window.location.origin
  const [language, setLanguage] = useState('python')

  return (
    <div className="fade-up" style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
      <div
        style={{
          display: 'flex',
          alignItems: 'flex-end',
          justifyContent: 'space-between',
          gap: 16,
        }}
      >
        <div>
          <h1
            className="grad-text"
            style={{
              margin: 0,
              fontSize: 44,
              fontWeight: 700,
              letterSpacing: '-0.04em',
              lineHeight: 1.02,
            }}
          >
            Quick start
          </h1>
          <p
            style={{
              margin: '14px 0 0 0',
              fontSize: 17,
              lineHeight: 1.45,
              color: 'var(--text)',
              maxWidth: 580,
              fontWeight: 400,
              letterSpacing: '-0.005em',
            }}
          >
            Pick a language, choose a package manager, copy the snippet —{' '}
            <span style={{ color: 'var(--text-soft)' }}>
              or grab the AI prompt for your assistant.
            </span>
          </p>
        </div>
        <EndpointInline endpoint={endpoint} />
      </div>

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: '240px 1fr',
          gap: 16,
          height: 720,
        }}
      >
        <LanguageRail selected={language} onSelect={setLanguage} />
        {language === 'all' ? (
          <AllInOnePane endpoint={endpoint} />
        ) : (
          <ConfigurePane languageId={language} endpoint={endpoint} />
        )}
      </div>
    </div>
  )
}
