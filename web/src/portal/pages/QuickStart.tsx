// QuickStart — depsilo's portal landing page.
//
// Layout: compact AI hero strip on top, then a single "console" card
// that combines the left ecosystem rail with the active configuration
// pane. The console fills the viewport so the user never has to scroll
// to reach the config snippet — the picker and the result sit side by
// side, IDE-style.
//
// Endpoint URL lives in the global header (PortalApp.tsx).
import { useState } from 'react'
import ConfigurePane from '@/portal/components/ConfigurePane'
import EcosystemCatalog from '@/portal/components/EcosystemCatalog'
import HeroAICTA from '@/portal/components/HeroAICTA'
import CodeBlock from '@/portal/components/CodeBlock'
import EcosystemIcon from '@/components/EcosystemIcon'
import { LANGUAGES } from '@/lib/ecosystemData'

function renderSnippet(template: string, endpoint: string) {
  return template
    .replaceAll('{URL}', endpoint)
    .replaceAll('{HOST}', endpoint.replace(/^https?:\/\//, ''))
}

function SetupPath({ language, endpoint, onSelect }: { language: string; endpoint: string; onSelect: (id: string) => void }) {
  const current = LANGUAGES.find((item) => item.id === language) ?? LANGUAGES[0]
  const quick = current.managers[0]?.quick
  const code = quick ? renderSnippet(quick.body, endpoint) : `curl ${endpoint}`
  const featured = LANGUAGES.filter((item) => ['python', 'node', 'cargo', 'docker'].includes(item.id))

  return (
    <section
      className="quickstart-setup sv-reveal"
      style={{
        display: 'grid',
        gridTemplateColumns: 'minmax(0, 0.9fr) minmax(320px, 1.1fr)',
        gap: 14,
      }}
    >
      <div
        className="card aurora-rim"
        style={{
          padding: 18,
          display: 'flex',
          flexDirection: 'column',
          gap: 16,
          minWidth: 0,
        }}
      >
        <div>
          <div className="eyebrow">Setup path</div>
          <h2 style={{ fontSize: 24, marginTop: 8 }}>Route installs through Depsilo in three steps</h2>
          <p style={{ color: 'var(--text-muted)', fontSize: 13, margin: '8px 0 0', maxWidth: 560 }}>
            Pick an ecosystem, copy the recommended command, then verify the install shows up in Monitor.
          </p>
        </div>
        <div style={{ display: 'grid', gap: 10 }}>
          {[
            ['1', 'Choose ecosystem', current.name],
            ['2', 'Copy config', current.managers[0]?.name ?? 'Package manager'],
            ['3', 'Verify traffic', 'Open Monitor after one install'],
          ].map(([step, title, hint]) => (
            <div
              key={step}
              style={{
                display: 'grid',
                gridTemplateColumns: '28px 1fr',
                gap: 10,
                alignItems: 'center',
              }}
            >
              <span
                className="num"
                style={{
                  width: 28,
                  height: 28,
                  borderRadius: 8,
                  display: 'inline-flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  background: 'var(--brand-soft)',
                  border: '0.5px solid var(--brand-border)',
                  color: 'var(--brand-text)',
                  fontSize: 12,
                  fontWeight: 700,
                }}
              >
                {step}
              </span>
              <span>
                <strong style={{ display: 'block', fontSize: 13, color: 'var(--text)' }}>{title}</strong>
                <span style={{ display: 'block', fontSize: 12, color: 'var(--text-soft)', marginTop: 1 }}>{hint}</span>
              </span>
            </div>
          ))}
        </div>
      </div>

      <div className="card" style={{ padding: 14, minWidth: 0, display: 'flex', flexDirection: 'column', gap: 12 }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
          <div style={{ minWidth: 0 }}>
            <div className="eyebrow">Recommended first copy</div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 7 }}>
              <EcosystemIcon type={current.iconAdapter as any} size={16} useColor />
              <strong style={{ fontSize: 15, color: 'var(--text)' }}>{current.name}</strong>
              <span style={{ fontSize: 12, color: 'var(--text-soft)' }}>{current.managers[0]?.name}</span>
            </div>
          </div>
          <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', justifyContent: 'flex-end' }}>
            {featured.map((item) => (
              <button
                key={item.id}
                type="button"
                onClick={() => onSelect(item.id)}
                className="active:scale-[0.96]"
                style={{
                  minHeight: 34,
                  display: 'inline-flex',
                  alignItems: 'center',
                  gap: 6,
                  padding: '0 9px',
                  borderRadius: 7,
                  border: `0.5px solid ${item.id === current.id ? 'var(--brand-border)' : 'var(--border)'}`,
                  background: item.id === current.id ? 'var(--brand-soft)' : 'var(--bg-soft)',
                  color: item.id === current.id ? 'var(--brand-text)' : 'var(--text-muted)',
                  fontSize: 12,
                  fontWeight: 600,
                  cursor: 'pointer',
                  transition: 'background 120ms ease, color 120ms ease, border-color 120ms ease, transform 120ms cubic-bezier(0.2, 0, 0, 1)',
                }}
              >
                <EcosystemIcon type={item.iconAdapter as any} size={13} useColor />
                {item.name}
              </button>
            ))}
          </div>
        </div>
        <CodeBlock filename={`${current.managers[0]?.name ?? current.id}.quickstart`} code={code} language={quick?.lang ?? 'sh'} />
      </div>
    </section>
  )
}

export default function QuickStart() {
  const endpoint = window.location.origin
  const [language, setLanguage] = useState('python')

  return (
    <div className="fade-up" style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      {/* Hero — compact horizontal strip carrying the project-level AI
          integration CTA. */}
      <div className="sv-reveal">
        <HeroAICTA />
      </div>

      <SetupPath language={language} endpoint={endpoint} onSelect={setLanguage} />

      {/* Console — single card with the ecosystem rail on the left and
          the active configuration pane on the right. The two share one
          border / radius so they read as a single designed surface. */}
      <section
        className="quickstart-console sv-reveal"
        style={{
          display: 'grid',
          gridTemplateColumns: '260px 1fr',
          background: 'var(--bg-card)',
          border: '0.5px solid var(--border)',
          borderRadius: 14,
          overflow: 'hidden',
          minHeight: 'min(72vh, 720px)',
        }}
      >
        <EcosystemCatalog selected={language} onSelect={setLanguage} />
        <ConfigurePane languageId={language} endpoint={endpoint} flush />
      </section>
    </div>
  )
}
