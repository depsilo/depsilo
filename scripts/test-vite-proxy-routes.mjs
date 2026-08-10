#!/usr/bin/env node

import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { createServer } from 'node:http'
import { createRequire } from 'node:module'
import path from 'node:path'
import { pathToFileURL } from 'node:url'

const root = path.resolve(import.meta.dirname, '..')
const webRequire = createRequire(path.join(root, 'web/package.json'))
const viteEntry = webRequire.resolve('vite')
const { loadConfigFromFile } = await import(pathToFileURL(viteEntry))

const catalog = await readFile(path.join(root, 'internal/ecosystem/catalog.go'), 'utf8')
const catalogRoutes = Array.from(
  catalog.matchAll(/\{Name:\s*"[^"]+",\s*Route:\s*"([^"]+)"/g),
  match => match[1],
)
assert.ok(catalogRoutes.length > 0, 'no package protocol routes found in the Go ecosystem catalog')

const dynamicExtraIndex = '/custom/team-pytorch'
const backend = createServer((request, response) => {
  if (request.url === '/api/v1/stats') {
    response.setHeader('content-type', 'application/json')
    response.end(JSON.stringify({
      extra_indexes: [{ kind: 'pytorch', path: dynamicExtraIndex.slice(1) }],
    }))
    return
  }
  response.statusCode = 404
  response.end()
})

await new Promise((resolve, reject) => {
  backend.once('error', reject)
  backend.listen(0, '127.0.0.1', resolve)
})

const address = backend.address()
assert.ok(address && typeof address === 'object')
const backendURL = `http://127.0.0.1:${address.port}`
const previousBackendURL = process.env.DEPSILO_DEV_BACKEND_URL
process.env.DEPSILO_DEV_BACKEND_URL = backendURL

try {
  const loaded = await loadConfigFromFile(
    { command: 'serve', mode: 'test' },
    path.join(root, 'web/vite.config.ts'),
    root,
  )
  assert.ok(loaded, 'Vite config did not load')

  const proxy = loaded.config.server?.proxy ?? {}
  const matchingTargets = requestPath => Object.entries(proxy)
    .filter(([context]) => context.startsWith('^')
      ? new RegExp(context).test(requestPath)
      : requestPath.startsWith(context))
    .map(([, options]) => typeof options === 'string' ? options : options.target)

  for (const route of [...catalogRoutes, dynamicExtraIndex]) {
    assert.deepEqual(
      matchingTargets(`${route}/probe`),
      [backendURL],
      `Vite does not proxy the current package route ${route} to the Depsilo backend`,
    )
  }

  for (const machinePath of [
    '/api/v1/stats',
    '/ccache/v1/example/object',
    '/sccache/v1/example/object',
    '/p/example/npm/lodash',
  ]) {
    assert.deepEqual(
      matchingTargets(machinePath),
      [backendURL],
      `Vite does not proxy development machine route ${machinePath}`,
    )
  }

  for (const browserRoute of [
    '/',
    '/admin',
    '/monitor',
    '/apiary',
    '/ccachet',
    '/sccache-old',
    '/preview',
    '/governance',
  ]) {
    assert.deepEqual(
      matchingTargets(browserRoute),
      [],
      `Vite package proxy unexpectedly captures browser route ${browserRoute}`,
    )
  }
} finally {
  if (previousBackendURL === undefined) delete process.env.DEPSILO_DEV_BACKEND_URL
  else process.env.DEPSILO_DEV_BACKEND_URL = previousBackendURL
  await new Promise(resolve => backend.close(resolve))
}

console.log('Vite package proxy route tests passed')
