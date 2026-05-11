// web/src/portal/components/ConfigurePane.tsx
import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import CodeBlock from '@/portal/components/CodeBlock'
import EcosystemIcon from '@/components/EcosystemIcon'
import { LANGUAGES, buildPrompt, type ManagerConfig } from '@/lib/ecosystemData'

interface Props {
  languageId: string
  endpoint: string
}

function relTime(ts: number): string {
  const s = Math.max(0, Math.floor((Date.now() - ts) / 1000))
  if (s < 1) return '0s'
  if (s < 60) return `${s}s`
  return `${Math.floor(s / 60)}m`
}

// Footer: listens to SSE for requests matching the manager
// managerId: reserved for future per-ecosystem filtering
function LiveDetector({ endpoint, managerId: _managerId }: { endpoint: string; managerId: string }) {
  const { t } = useTranslation()
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
          {t('quickstart.listeningOn')}{' '}
          <span className="mono" style={{ color: 'var(--text-subtle)' }}>
            {endpoint}
          </span>
          {' '}{t('quickstart.verifyHint')}
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
            {t('quickstart.detectCount', { count: hits.length })}
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


function ManagerTabs({
  managers,
  active,
  onChange,
}: {
  managers: ManagerConfig[]
  active: string
  onChange: (id: string) => void
}) {
  const { t } = useTranslation()
  const isAI = active === 'ai'
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 4, flexWrap: 'wrap' }}>
      {/* AI option — always first */}
      <button
        type="button"
        onClick={() => onChange('ai')}
        style={{
          display: 'inline-flex',
          flexDirection: 'column',
          padding: '6px 10px',
          background: isAI ? 'var(--brand-soft)' : 'transparent',
          border: `0.5px solid ${isAI ? 'var(--brand-border)' : 'var(--border)'}`,
          borderRadius: 6,
          textAlign: 'left',
          cursor: 'pointer',
          transition: 'all 120ms ease',
        }}
      >
        <span style={{ display: 'flex', alignItems: 'center', gap: 5 }}>
          <span
            style={{
              fontFamily: 'var(--font-mono)',
              fontSize: 9,
              color: isAI ? 'var(--brand)' : 'var(--text-subtle)',
              background: isAI ? 'var(--brand-soft)' : 'var(--bg-soft)',
              border: `0.5px solid ${isAI ? 'var(--brand-border)' : 'var(--border)'}`,
              borderRadius: 3,
              padding: '1px 4px',
              letterSpacing: '0.04em',
            }}
          >
            AI
          </span>
          <span
            style={{
              fontSize: 12,
              fontWeight: 500,
              color: isAI ? 'var(--brand)' : 'var(--text-muted)',
              whiteSpace: 'nowrap',
            }}
          >
            {t('quickstart.aiTab')}
          </span>
        </span>
        <span style={{ fontSize: 10, color: isAI ? 'var(--brand)' : 'var(--text-subtle)', whiteSpace: 'nowrap', opacity: 0.8 }}>
          {t('quickstart.aiTabHint')}
        </span>
      </button>

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
            AI
          </span>
          <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>
            {t('quickstart.promptForTools')}
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

function PathsCollapsible({ paths }: { paths: { os: string; path: string }[] }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  return (
    <div
      style={{
        border: '0.5px solid var(--border)',
        borderRadius: 6,
        background: 'var(--bg-soft)',
      }}
    >
      <button
        type="button"
        onClick={() => setOpen(v => !v)}
        style={{
          width: '100%',
          padding: '6px 12px',
          fontSize: 11,
          color: 'var(--text-muted)',
          cursor: 'pointer',
          display: 'flex',
          alignItems: 'center',
          gap: 6,
          background: 'transparent',
          border: 'none',
          textAlign: 'left',
        }}
      >
        <span style={{ color: 'var(--text-subtle)', transition: 'transform 150ms', transform: open ? 'rotate(90deg)' : 'none', display: 'inline-block' }}>▸</span>
        {t('quickstart.whereReadsFrom')}
      </button>
      {open && (
        <div style={{ borderTop: '0.5px solid var(--border)', overflow: 'hidden' }}>
          {paths.map((p, i) => (
            <div
              key={i}
              style={{
                display: 'grid',
                gridTemplateColumns: '120px 1fr',
                alignItems: 'center',
                gap: 12,
                padding: '6px 12px',
                borderBottom: i < paths.length - 1 ? '0.5px solid var(--border)' : 'none',
              }}
            >
              <span className="eyebrow" style={{ whiteSpace: 'nowrap' }}>{p.os}</span>
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
      )}
    </div>
  )
}

export default function ConfigurePane({ languageId, endpoint }: Props) {
  const { t } = useTranslation()
  const lang = LANGUAGES.find(l => l.id === languageId)
  const [mgrId, setMgrId] = useState(lang?.managers[0]?.id ?? '')

  useEffect(() => {
    setMgrId(lang?.managers[0]?.id ?? '')
  }, [languageId])

  if (!lang) return null

  const m = lang.managers.find(x => x.id === mgrId) ?? lang.managers[0]
  const url = endpoint
  const host = url.replace(/^https?:\/\//, '')
  const fill = (s: string) => s.replace(/\{URL\}/g, url).replace(/\{HOST\}/g, host)
  const prompt = buildPrompt(endpoint, languageId)

  return (
    <div
      className="card"
      style={{ display: 'flex', flexDirection: 'column' }}
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
            }}
          >
            <EcosystemIcon type={lang.iconAdapter as any} size={14} useColor={true} />
          </span>
          <span style={{ fontSize: 17, fontWeight: 700, letterSpacing: '-0.02em' }}>
            {t('quickstart.configureTitle', { name: lang.name })}
          </span>
          <span style={{ fontSize: 10, color: 'var(--text-subtle)', marginLeft: 4 }}>
            {t('quickstart.managerCount', { count: lang.managers.length })}
          </span>
        </div>
        <div style={{ flex: 1 }} />
      </div>

      {/* Body */}
      <div
        style={{
          padding: 16,
          display: 'flex',
          flexDirection: 'column',
          gap: 16,
        }}
      >
        <ManagerTabs managers={lang.managers} active={mgrId} onChange={setMgrId} />

        {mgrId === 'ai' ? (
          <PromptCard prompt={prompt} />
        ) : (
          <>
            {/* Quick methods (e.g. -i flag, env var) */}
            {m.methods && m.methods.length > 0 && (
              <>
                <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                  {m.methods.map((method, i) => (
                    <div key={i} style={{ display: 'flex', flexDirection: 'column', gap: 5 }}>
                      <span
                        style={{
                          fontFamily: 'var(--font-mono)',
                          fontSize: 10,
                          color: 'var(--text-subtle)',
                          letterSpacing: '0.04em',
                          padding: '0 2px',
                        }}
                      >
                        {t(method.label)}
                      </span>
                      <CodeBlock code={fill(method.body)} language={method.lang} />
                    </div>
                  ))}
                </div>

                {/* separator → persistent config */}
                <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                  <span style={{ fontSize: 11, color: 'var(--text-subtle)', whiteSpace: 'nowrap' }}>
                    {t('quickstart.orSaveToConfig')}
                  </span>
                  <div style={{ flex: 1, height: '0.5px', background: 'var(--border)' }} />
                </div>
              </>
            )}

            {/* Config block */}
            <CodeBlock
              filename={m.persistent.file}
              code={fill(m.persistent.body)}
              language={m.persistent.lang}
            />

            {/* Where config lives */}
            <PathsCollapsible paths={m.paths} />

            {/* Verify block */}
            <CodeBlock code={fill(m.verify.body)} language={m.verify.lang} />
          </>
        )}
      </div>

      <LiveDetector endpoint={endpoint} managerId={m.id} />
    </div>
  )
}
