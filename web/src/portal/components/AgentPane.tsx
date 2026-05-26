// web/src/portal/components/AgentPane.tsx
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { buildAgentPrompt } from '@/lib/ecosystemData'

interface Props { endpoint: string }

function PromptCard({ prompt }: { prompt: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  function copy() {
    navigator.clipboard.writeText(prompt).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }).catch(() => {})
  }
  return (
    <div
      style={{
        background: 'var(--bg-soft)',
        border: '0.5px solid var(--border)',
        borderRadius: 8,
        overflow: 'hidden',
      }}
    >
      <div
        style={{
          height: 32,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '0 8px 0 12px',
          borderBottom: '0.5px solid var(--border)',
          background: 'var(--bg-card)',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span
            style={{
              fontFamily: 'var(--font-mono)',
              fontSize: 10,
              color: 'var(--brand)',
              padding: '1px 6px',
              background: 'var(--brand-soft)',
              borderRadius: 3,
              letterSpacing: '0.04em',
            }}
          >
            AGENT
          </span>
          <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>
            {t('quickstart.aiAgentPromptLabel')}
          </span>
        </div>
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
            background: 'transparent',
          }}
        >
          {copied ? t('quickstart.copied') : t('quickstart.copy')}
        </button>
      </div>
      <pre
        style={{
          margin: 0,
          padding: 14,
          fontFamily: 'var(--font-mono)',
          fontSize: 12.5,
          lineHeight: 1.55,
          color: 'var(--text)',
          background: 'var(--bg-soft)',
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-word',
          maxHeight: 520,
          overflow: 'auto',
        }}
      >
        {prompt}
      </pre>
    </div>
  )
}

export default function AgentPane({ endpoint }: Props) {
  const { t } = useTranslation()
  const prompt = buildAgentPrompt(endpoint)
  return (
    <div className="card" style={{ padding: 22, display: 'flex', flexDirection: 'column', gap: 16 }}>
      <div>
        <h2
          style={{
            margin: 0,
            fontSize: 22,
            fontWeight: 600,
            letterSpacing: '-0.02em',
            color: 'var(--text)',
          }}
        >
          {t('quickstart.aiAgentTitle')}
        </h2>
        <p
          style={{
            margin: '8px 0 0 0',
            fontSize: 14,
            lineHeight: 1.55,
            color: 'var(--text-soft)',
            maxWidth: 720,
          }}
        >
          {t('quickstart.aiAgentDesc')}
        </p>
      </div>
      <PromptCard prompt={prompt} />
      <p style={{ margin: 0, fontSize: 12, color: 'var(--text-subtle)' }}>
        {t('quickstart.aiAgentHint')}
      </p>
    </div>
  )
}
