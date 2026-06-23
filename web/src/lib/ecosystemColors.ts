// Single source of truth for "what color represents this ecosystem"
// in charts and legends. Each ecosystem's color matches its real-world
// brand (npm red, go cyan, ruby red, etc.) so users recognise their
// own packages at a glance without consulting a legend.
//
// This is a deliberate exception to taste-skill's Color Consistency
// Lock: chart series need differentiation, and brand colors are
// functional (identifying) rather than decorative.
//
// For ecosystems without a strong public brand color, falls back to
// teal-aligned tones so the palette as a whole still feels cohesive.

export const ECOSYSTEM_COLORS: Record<string, string> = {
  pypi:     'var(--brand)',
  apt:      '#3bd671',  // Debian green
  npm:      '#cb3837',  // npm red
  go:       '#00add8',  // Go cyan
  cargo:    '#dea584',  // Rust orange-tan
  maven:    '#c71a36',  // Maven red
  rubygems: '#e9573f',  // Ruby coral
  composer: '#885630',  // Composer brown
  nuget:    '#004880',  // NuGet navy
  conda:    '#44a833',  // Anaconda green
  cran:     '#2266b8',  // CRAN blue
  helm:     '#0f1689',  // Helm deep blue
  docker:   '#2496ed',  // Docker blue
  huggingface: '#ffd21e', // HF yellow
}

// Brand-aligned fallback for unknown ecosystems / index-based access.
// Uses the teal/cyan spec-* tokens so unknown series still feel cohesive
// with the rest of the design system.
const FALLBACK_PALETTE = [
  'var(--spec-1)',
  'var(--spec-2)',
  'var(--spec-3)',
  'var(--spec-4)',
  'var(--brand-strong)',
  'var(--warn)',
]

export function getEcosystemColor(name: string | undefined, indexFallback = 0): string {
  if (name && ECOSYSTEM_COLORS[name]) return ECOSYSTEM_COLORS[name]
  return FALLBACK_PALETTE[indexFallback % FALLBACK_PALETTE.length]
}
