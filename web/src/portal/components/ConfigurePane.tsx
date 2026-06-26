// web/src/portal/components/ConfigurePane.tsx
import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import CodeBlock from '@/portal/components/CodeBlock'
import PromptCard from '@/portal/components/PromptCard'
import EcosystemIcon from '@/components/EcosystemIcon'
import { LANGUAGES, buildPrompt, type ManagerConfig } from '@/lib/ecosystemData'

interface Props {
  languageId: string
  endpoint: string
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
      {/* AI option: always first */}
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
          <PromptCard prompt={prompt} label={t('quickstart.promptForTools')} />
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

    </div>
  )
}
