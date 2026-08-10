import path from "path"
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from "@tailwindcss/vite"

// Keep this list aligned with internal/ecosystem/catalog.go. The executable
// scripts/test-vite-proxy-routes.mjs contract fails when the Go catalog grows
// without a matching development proxy route.
const PACKAGE_PROXY_PREFIXES = [
  '/pypi',
  '/apt',
  '/npm',
  '/go',
  '/crates',
  '/maven',
  '/rubygems',
  '/composer',
  '/nuget',
  '/conda',
  '/cran',
  '/alpine',
  '/helm',
  '/huggingface',
  '/v2',
] as const

const MACHINE_PROXY_PREFIXES = [
  '/api',
  '/mcp',
  '/health',
  '/live',
  '/ready',
  '/metrics',
  '/ccache',
  '/sccache',
  // Project-scoped package routes include both catalog adapters and dynamic
  // extra indexes. Docker deliberately has no project-scoped route.
  '/p',
] as const

function backendURL(): string {
  const configured = process.env.DEPSILO_DEV_BACKEND_URL ?? 'http://localhost:23333'
  const parsed = new URL(configured)
  if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
    throw new Error('DEPSILO_DEV_BACKEND_URL must use http or https')
  }
  parsed.pathname = '/'
  parsed.search = ''
  parsed.hash = ''
  return parsed.toString().replace(/\/$/, '')
}

function proxyPattern(prefix: string): string {
  const escaped = prefix.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return `^${escaped}(?:/|$)`
}

function normalizeExtraIndexPath(value: unknown): string | null {
  if (typeof value !== 'string') return null
  const route = value.replace(/^\/+|\/+$/g, '')
  if (!route || !route.split('/').every(segment => /^[A-Za-z0-9._-]+$/.test(segment))) {
    return null
  }
  return `/${route}`
}

async function extraIndexPrefixes(target: string): Promise<string[]> {
  try {
    const response = await fetch(`${target}/api/v1/stats`, {
      headers: { accept: 'application/json' },
      signal: AbortSignal.timeout(1500),
    })
    if (!response.ok) return []
    const payload = await response.json() as {
      extra_indexes?: Array<{ path?: unknown }>
    }
    return (payload.extra_indexes ?? [])
      .map(index => normalizeExtraIndexPath(index.path))
      .filter((prefix): prefix is string => prefix !== null)
  } catch {
    // `npm run dev` can still start before the backend. Fixed protocols and UI
    // API routes remain available; make dev-ui starts the backend first and
    // therefore also discovers operator-defined extra-index paths.
    return []
  }
}

export default defineConfig(async ({ command }) => {
  const target = backendURL()
  const dynamicPrefixes = command === 'serve' ? await extraIndexPrefixes(target) : []
  const prefixes = Array.from(new Set([
    ...MACHINE_PROXY_PREFIXES,
    ...PACKAGE_PROXY_PREFIXES,
    ...dynamicPrefixes,
  ]))

  return {
    plugins: [react(), tailwindcss()],
    resolve: {
      alias: {
        "@": path.resolve(__dirname, "./src"),
      },
    },
    server: {
      proxy: Object.fromEntries(
        prefixes.map(prefix => [proxyPattern(prefix), { target }]),
      ),
    },
  }
})
