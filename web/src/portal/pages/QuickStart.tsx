// QuickStart — depsilo's portal landing page.
//
// Layout: compact AI hero strip, a compiler-cache introduction, then a
// single "console" card that combines the left ecosystem rail with the
// active configuration pane. The picker and result sit side by side,
// IDE-style.
//
// Endpoint URL lives in the global header (PortalApp.tsx).
import { useState } from 'react'
import ConfigurePane from '@/portal/components/ConfigurePane'
import CompileCacheIntro from '@/portal/components/CompileCacheIntro'
import EcosystemCatalog from '@/portal/components/EcosystemCatalog'
import HeroAICTA from '@/portal/components/HeroAICTA'

export default function QuickStart() {
  const endpoint = window.location.origin
  const [language, setLanguage] = useState('python')

  return (
    // Stagger the three page modules so the page builds top-down instead
    // of fading in as one slab.
    <div style={{ display: 'flex', flexDirection: 'column', gap: 22 }}>
      {/* Hero — compact horizontal strip carrying the project-level AI
          integration CTA. */}
      {/* (sv-reveal would override fade-up's `animation` shorthand and
          these blocks start inside the viewport anyway, so the scroll
          reveal never fired for them — load stagger does the work.) */}
      <div className="fade-up">
        <HeroAICTA />
      </div>

      {/* Compiler artifacts are a separate cache domain from the package
          ecosystems below, so the public overview gets its own module. */}
      <div className="fade-up fade-up-d1">
        <CompileCacheIntro />
      </div>

      {/* Console — single card with the ecosystem rail on the left and
          the active configuration pane on the right. The two share one
          border / radius so they read as a single designed surface. */}
      <section
        className="quickstart-console fade-up fade-up-d2"
        style={{
          display: 'grid',
          gridTemplateColumns: 'clamp(300px, 19vw, 340px) 1fr',
          background: 'var(--bg-card)',
          border: '0.5px solid var(--border-strong)',
          borderRadius: 18,
          overflow: 'hidden',
          minHeight: 'min(70vh, 860px)',
          boxShadow: 'var(--shadow-pop)',
        }}
      >
        <EcosystemCatalog selected={language} onSelect={setLanguage} />
        <ConfigurePane key={language} languageId={language} endpoint={endpoint} flush />
      </section>
    </div>
  )
}
