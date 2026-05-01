// web/src/portal/components/AllInOnePane.tsx
import { useState } from 'react'
import CodeBlock from '@/portal/components/CodeBlock'
import Segmented from '@/components/Segmented'
import { buildAllScript, buildPrompt } from '@/lib/ecosystemData'

interface Props { endpoint: string }

const MODES = [
  { value: 'script', label: 'Shell script' },
  { value: 'prompt', label: 'AI prompt'   },
]

function PromptCard({ prompt }: { prompt: string }) {
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
            AI
          </span>
          <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>
            Prompt for any AI coding tool
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
          {copied ? 'Copied' : 'Copy'}
        </button>
      </div>
      <div
        style={{
          padding: '12px 14px',
          fontSize: 12,
          lineHeight: 1.6,
          color: 'var(--text)',
          whiteSpace: 'pre-wrap',
          maxHeight: 180,
          overflowY: 'auto',
        }}
      >
        {prompt}
      </div>
    </div>
  )
}

export default function AllInOnePane({ endpoint }: Props) {
  const [mode, setMode] = useState('script')
  const script = buildAllScript(endpoint)
  const prompt = buildPrompt(endpoint, 'all')

  return (
    <div
      className="card"
      style={{ display: 'flex', flexDirection: 'column', overflow: 'hidden', height: '100%' }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          borderBottom: '0.5px solid var(--border)',
          padding: '0 14px',
          height: 44,
          flexShrink: 0,
          gap: 12,
        }}
      >
        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            style={{
              fontSize: 17,
              fontWeight: 700,
              whiteSpace: 'nowrap',
              letterSpacing: '-0.02em',
              lineHeight: 1.2,
            }}
          >
            All-in-one setup
          </div>
          <div
            style={{
              fontSize: 12,
              color: 'var(--text-soft)',
              whiteSpace: 'nowrap',
              marginTop: 2,
            }}
          >
            Configure every detected package manager in one go
          </div>
        </div>
        <Segmented options={MODES} value={mode} onChange={setMode} />
      </div>
      <div
        style={{
          padding: 16,
          flex: 1,
          overflow: 'auto',
          minHeight: 0,
          display: 'flex',
          flexDirection: 'column',
          gap: 14,
        }}
      >
        <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
          {mode === 'script'
            ? 'Run this as root on your machine. It edits config for pip, npm, cargo, go, and docker — extend as needed.'
            : 'Paste this into ChatGPT, Claude, Cursor, or any agentic coding tool. The AI will detect your stack and edit the right files.'}
        </div>
        {mode === 'script' ? (
          <CodeBlock filename="depsilo-setup.sh" code={script} language="sh" />
        ) : (
          <PromptCard prompt={prompt} />
        )}
      </div>
    </div>
  )
}
