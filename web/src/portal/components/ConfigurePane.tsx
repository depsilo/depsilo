// web/src/portal/components/ConfigurePane.tsx
import { useState, useEffect, useCallback } from 'react'
import CodeBlock from '@/portal/components/CodeBlock'
import { LANGUAGES, buildPrompt, type ManagerConfig } from '@/lib/ecosystemData'

interface Props {
  languageId: string
  endpoint: string
}

function relTime(ts: number): string {
  const s = Math.max(0, Math.floor((Date.now() - ts) / 1000))
  if (s < 1) return 'now'
  if (s < 60) return `${s}s ago`
  return `${Math.floor(s / 60)}m ago`
}

// Footer: listens to SSE for requests matching the manager
function LiveDetector({ endpoint, managerId }: { endpoint: string; managerId: string }) {
  const [hits, setHits] = useState<{ id: string; path: string; ms: number; t: number }[]>([])
  const [, setTick] = useState(0)

  const handleEvent = useCallback((e: MessageEvent) => {
    try {
      const ev = JSON.parse(e.data)
      if (!ev.adapter_type) return
      const path = ev.file_name || ev.package_name || '—'
      const ms = ev.latency_ms ?? 0
      const id = ev.id || `${ev.timestamp}-${Math.random().toString(36).slice(2, 6)}`
      setHits(prev => [{ id, path, ms, t: Date.now() }, ...prev].slice(0, 3))
    } catch { /* ignore */ }
  }, [])

  useEffect(() => {
    const es = new EventSource('/api/v1/events/stream')
    es.onmessage = handleEvent
    return () => es.close()
  }, [handleEvent])

  // tick for relative timestamps
  useEffect(() => {
    const id = setInterval(() => setTick(t => t + 1), 1000)
    return () => clearInterval(id)
  }, [])

  const latest = hits[0]
  const fresh = latest && Date.now() - latest.t < 4000

  return (
    <div
      style={{
        borderTop: '0.5px solid var(--border)',
        padding: '8px 14px',
        display: 'flex',
        alignItems: 'center',
        gap: 10,
        background: 'var(--bg-card)',
        flexShrink: 0,
        minHeight: 36,
      }}
    >
      <span
        className="dot-live"
        style={{
          width: 6,
          height: 6,
          borderRadius: '50%',
          background: hits.length ? 'var(--ok)' : 'var(--text-subtle)',
          color: hits.length ? 'var(--ok)' : 'var(--text-subtle)',
          flexShrink: 0,
        }}
      />
      {hits.length === 0 ? (
        <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>
          Listening for requests on{' '}
          <span className="mono" style={{ color: 'var(--text-subtle)' }}>
            {endpoint}
          </span>
          … run the verify command above.
        </span>
      ) : (
        <>
          <span
            style={{
              fontSize: 11,
              color: fresh ? 'var(--ok-text)' : 'var(--text-muted)',
              fontWeight: fresh ? 500 : 400,
              transition: 'color 300ms',
              whiteSpace: 'nowrap',
            }}
          >
            {hits.length} request{hits.length > 1 ? 's' : ''} detected
          </span>
          <span style={{ color: 'var(--border-strong)' }}>·</span>
          <span
            className="mono"
            style={{
              fontSize: 11,
              color: 'var(--text)',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
              flex: 1,
              minWidth: 0,
            }}
          >
            {latest.path}
          </span>
          <span
            className="mono"
            style={{
              fontSize: 10,
              color: 'var(--text-subtle)',
              padding: '2px 6px',
              background: 'var(--bg-soft)',
              border: '0.5px solid var(--border)',
              borderRadius: 4,
              flexShrink: 0,
            }}
          >
            {latest.ms}ms
          </span>
          <span style={{ fontSize: 10, color: 'var(--text-subtle)', flexShrink: 0, whiteSpace: 'nowrap' }}>
            {relTime(latest.t)}
          </span>
        </>
      )}
    </div>
  )
}

function ConfigSection({
  step,
  title,
  subtitle,
  children,
}: {
  step: number
  title: string
  subtitle?: string
  children: React.ReactNode
}) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 10, padding: '0 2px' }}>
        <span
          style={{
            fontFamily: 'var(--font-mono)',
            fontSize: 11,
            color: 'var(--text-subtle)',
            letterSpacing: '0.04em',
            flexShrink: 0,
          }}
        >
          {String(step).padStart(2, '0')}
        </span>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontSize: 14, fontWeight: 600, color: 'var(--text)', letterSpacing: '-0.005em' }}>
            {title}
          </div>
          {subtitle && (
            <div style={{ fontSize: 12, color: 'var(--text-soft)', marginTop: 2 }}>{subtitle}</div>
          )}
        </div>
      </div>
      {children}
    </div>
  )
}

function ManagerTabs({
  managers,
  active,
  onChange,
}: {
  managers: ManagerConfig[]
  active: string
  onChange: (id: string) => void
}) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 4, flexWrap: 'wrap' }}>
      {managers.map(m => {
        const isActive = m.id === active
        return (
          <button
            key={m.id}
            type="button"
            onClick={() => onChange(m.id)}
            style={{
              display: 'inline-flex',
              flexDirection: 'column',
              padding: '6px 10px',
              background: isActive ? 'var(--bg-card)' : 'transparent',
              border: `0.5px solid ${isActive ? 'var(--border-strong)' : 'var(--border)'}`,
              borderRadius: 6,
              textAlign: 'left',
              cursor: 'pointer',
              transition: 'all 120ms ease',
            }}
          >
            <span
              style={{
                fontSize: 12,
                fontWeight: 500,
                color: isActive ? 'var(--text)' : 'var(--text-muted)',
                whiteSpace: 'nowrap',
              }}
            >
              {m.name}
            </span>
            <span style={{ fontSize: 10, color: 'var(--text-subtle)', whiteSpace: 'nowrap' }}>
              {m.hint}
            </span>
          </button>
        )
      })}
    </div>
  )
}

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
            Prompt for ChatGPT / Claude / Cursor
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

export default function ConfigurePane({ languageId, endpoint }: Props) {
  const lang = LANGUAGES.find(l => l.id === languageId)
  const [mgrId, setMgrId] = useState(lang?.managers[0]?.id ?? '')
  const [showPrompt, setShowPrompt] = useState(false)

  useEffect(() => {
    setMgrId(lang?.managers[0]?.id ?? '')
    setShowPrompt(false)
  }, [languageId, lang])

  if (!lang) return null

  const m = lang.managers.find(x => x.id === mgrId) ?? lang.managers[0]
  const url = endpoint
  const host = url.replace(/^https?:\/\//, '')
  const fill = (s: string) => s.replace(/\{URL\}/g, url).replace(/\{HOST\}/g, host)
  const prompt = buildPrompt(endpoint, languageId)

  return (
    <div
      className="card"
      style={{ display: 'flex', flexDirection: 'column', overflow: 'hidden', height: '100%' }}
    >
      {/* Header */}
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
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, whiteSpace: 'nowrap' }}>
          <span
            style={{
              width: 22,
              height: 22,
              borderRadius: 5,
              background: 'var(--brand-soft)',
              border: '0.5px solid var(--brand-border)',
              display: 'inline-flex',
              alignItems: 'center',
              justifyContent: 'center',
              fontFamily: 'var(--font-mono)',
              fontSize: 9,
              fontWeight: 500,
              color: 'var(--brand)',
            }}
          >
            {lang.glyph}
          </span>
          <span style={{ fontSize: 17, fontWeight: 700, letterSpacing: '-0.02em' }}>
            Configure {lang.name}
          </span>
          <span style={{ fontSize: 10, color: 'var(--text-subtle)', marginLeft: 4 }}>
            {lang.managers.length} {lang.managers.length === 1 ? 'manager' : 'managers'}
          </span>
        </div>
        <div style={{ flex: 1 }} />
        <button
          type="button"
          onClick={() => setShowPrompt(p => !p)}
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 6,
            padding: '4px 10px',
            fontSize: 11,
            fontWeight: 500,
            color: showPrompt ? 'var(--brand)' : 'var(--text-muted)',
            background: showPrompt ? 'var(--brand-soft)' : 'transparent',
            border: `0.5px solid ${showPrompt ? 'var(--brand-border)' : 'var(--border)'}`,
            borderRadius: 6,
            whiteSpace: 'nowrap',
            cursor: 'pointer',
          }}
        >
          <span
            style={{
              fontFamily: 'var(--font-mono)',
              fontSize: 9,
              color: showPrompt ? 'var(--brand)' : 'var(--text-subtle)',
              letterSpacing: '0.04em',
            }}
          >
            AI
          </span>
          prompt
        </button>
      </div>

      {/* Body */}
      <div
        style={{
          padding: 16,
          flex: 1,
          overflow: 'auto',
          minHeight: 0,
          display: 'flex',
          flexDirection: 'column',
          gap: 16,
        }}
      >
        {showPrompt && <PromptCard prompt={prompt} />}

        {lang.managers.length > 1 && (
          <ManagerTabs managers={lang.managers} active={m.id} onChange={setMgrId} />
        )}

        {/* 01 Configure */}
        <ConfigSection
          step={1}
          title="Configure"
          subtitle={`Edit ${m.persistent.file} — applied to every install from now on`}
        >
          <CodeBlock
            filename={m.persistent.file}
            code={fill(m.persistent.body)}
            language={m.persistent.lang}
          />
          <details
            style={{
              marginTop: 8,
              border: '0.5px solid var(--border)',
              borderRadius: 6,
              background: 'var(--bg-soft)',
            }}
          >
            <summary
              style={{
                padding: '6px 12px',
                fontSize: 11,
                color: 'var(--text-muted)',
                cursor: 'pointer',
                display: 'flex',
                alignItems: 'center',
                gap: 6,
                listStyle: 'none',
              }}
            >
              <span style={{ color: 'var(--text-subtle)' }}>▸</span>
              Where this manager reads config from
            </summary>
            <div style={{ borderTop: '0.5px solid var(--border)' }}>
              {m.paths.map((p, i) => (
                <div
                  key={i}
                  style={{
                    display: 'grid',
                    gridTemplateColumns: '120px 1fr',
                    alignItems: 'center',
                    gap: 12,
                    padding: '6px 12px',
                    borderBottom:
                      i < m.paths.length - 1 ? '0.5px solid var(--border)' : 'none',
                  }}
                >
                  <span className="eyebrow" style={{ whiteSpace: 'nowrap' }}>
                    {p.os}
                  </span>
                  <span
                    style={{
                      fontFamily: 'var(--font-mono)',
                      fontSize: 11.5,
                      color: 'var(--text)',
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                      whiteSpace: 'nowrap',
                    }}
                  >
                    {p.path}
                  </span>
                </div>
              ))}
            </div>
          </details>
        </ConfigSection>

        {/* 02 Verify */}
        <ConfigSection
          step={2}
          title="Verify"
          subtitle="Run a test install — the request will appear in monitoring within ~2s"
        >
          <CodeBlock code={fill(m.verify.body)} language={m.verify.lang} />
        </ConfigSection>

        {/* 03 Step-by-step */}
        <ConfigSection
          step={3}
          title="Step-by-step"
          subtitle="Walk through the configuration end-to-end"
        >
          <details>
            <summary
              style={{
                padding: '8px 12px',
                fontSize: 11,
                color: 'var(--text-muted)',
                background: 'var(--bg-soft)',
                border: '0.5px solid var(--border)',
                borderRadius: 6,
                cursor: 'pointer',
                listStyle: 'none',
                display: 'flex',
                alignItems: 'center',
                gap: 6,
              }}
            >
              <span style={{ color: 'var(--text-subtle)' }}>▸</span>
              Show {m.tutorial.length} steps
            </summary>
            <ol
              style={{
                margin: '8px 0 0 0',
                paddingLeft: 0,
                listStyle: 'none',
                display: 'flex',
                flexDirection: 'column',
                gap: 6,
              }}
            >
              {m.tutorial.map((step, i) => (
                <li
                  key={i}
                  style={{
                    display: 'grid',
                    gridTemplateColumns: '24px 1fr',
                    alignItems: 'flex-start',
                    gap: 10,
                    padding: '8px 12px',
                    background: 'var(--bg-soft)',
                    border: '0.5px solid var(--border)',
                    borderRadius: 6,
                  }}
                >
                  <span
                    style={{
                      width: 18,
                      height: 18,
                      borderRadius: 4,
                      background: 'var(--brand-soft)',
                      color: 'var(--brand)',
                      fontFamily: 'var(--font-mono)',
                      fontSize: 10,
                      fontWeight: 500,
                      display: 'inline-flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      marginTop: 1,
                      flexShrink: 0,
                    }}
                  >
                    {i + 1}
                  </span>
                  <span style={{ fontSize: 12, lineHeight: 1.6, color: 'var(--text)' }}>
                    {fill(step)}
                  </span>
                </li>
              ))}
            </ol>
          </details>
        </ConfigSection>
      </div>

      <LiveDetector endpoint={endpoint} managerId={m.id} />
    </div>
  )
}
