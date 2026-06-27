// QuickStart — depsilo's portal landing page.
//
// Layout (top → bottom):
//   1. Hero AI CTA          — project-level integration prompt, the loudest
//                              workflow on the page
//   2. Ecosystem catalog    — all 14 supported ecosystems grouped into
//                              four sections (OS / Languages / Data & AI /
//                              Infrastructure), every tile always visible
//   3. Active configuration  — the selected ecosystem's snippet pane,
//                              defaulting to the AI-prompt tab so the
//                              language-level AI workflow is always one
//                              click away
//
// The endpoint URL lives in the global header (PortalApp.tsx) as a small
// monospace pill so it stays copyable without occupying hero real estate.
// "Default selection" is Python because pypi is depsilo's most-mirrored
// ecosystem; switching populates immediately — no empty states between
// clicks.
import { useState } from 'react'
import ConfigurePane from '@/portal/components/ConfigurePane'
import EcosystemCatalog from '@/portal/components/EcosystemCatalog'
import HeroAICTA from '@/portal/components/HeroAICTA'

export default function QuickStart() {
  const endpoint = window.location.origin
  const [language, setLanguage] = useState('python')

  return (
    <div className="fade-up" style={{ display: 'flex', flexDirection: 'column', gap: 28 }}>
      {/* Hero — project-level AI integration prompt. */}
      <HeroAICTA />

      {/* Full catalog — 4 groups, all 14 tiles always visible. */}
      <EcosystemCatalog selected={language} onSelect={setLanguage} />

      {/* Active configuration pane — defaults to AI prompt tab inside
          ConfigurePane (set via the ecosystem-data redesign). */}
      <ConfigurePane languageId={language} endpoint={endpoint} />
    </div>
  )
}
