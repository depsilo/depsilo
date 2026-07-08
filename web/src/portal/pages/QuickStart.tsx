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

export default function QuickStart() {
  const endpoint = window.location.origin
  const [language, setLanguage] = useState('python')

  return (
    <div className="fade-up" style={{ display: 'flex', flexDirection: 'column', gap: 22 }}>
      {/* Hero — compact horizontal strip carrying the project-level AI
          integration CTA. */}
      <div className="sv-reveal">
        <HeroAICTA />
      </div>

      {/* Console — single card with the ecosystem rail on the left and
          the active configuration pane on the right. The two share one
          border / radius so they read as a single designed surface. */}
      <section
        className="quickstart-console sv-reveal"
        style={{
          display: 'grid',
          gridTemplateColumns: '300px 1fr',
          background: 'var(--bg-card)',
          border: '0.5px solid var(--border-strong)',
          borderRadius: 18,
          overflow: 'hidden',
          minHeight: 'min(66vh, 760px)',
          boxShadow: 'var(--shadow-pop)',
        }}
      >
        <EcosystemCatalog selected={language} onSelect={setLanguage} />
        <ConfigurePane languageId={language} endpoint={endpoint} flush />
      </section>
    </div>
  )
}
