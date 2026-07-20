import { readFile, readdir, stat } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const webRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const distDir = path.join(webRoot, 'dist')
const assetsDir = path.join(distDir, 'assets')
const entryBudgetBytes = 450_000
const chunkBudgetBytes = 500_000
const lazyModules = ['PortalApp-', 'SetupWizard-', 'AdminApp-', 'AdminShell-']

function formatBytes(bytes) {
  return `${(bytes / 1000).toFixed(2)} kB`
}

async function main() {
  const html = await readFile(path.join(distDir, 'index.html'), 'utf8')
  const entryMatch = html.match(/<script\b[^>]*\bsrc="\/assets\/([^"?]+\.js)"/)
  if (!entryMatch) {
    throw new Error('could not find the production entry script in dist/index.html')
  }

  const assetNames = await readdir(assetsDir)
  const chunkNames = assetNames.filter((name) => name.endsWith('.js'))
  const chunks = await Promise.all(chunkNames.map(async (name) => ({
    name,
    bytes: (await stat(path.join(assetsDir, name))).size,
  })))
  chunks.sort((left, right) => right.bytes - left.bytes)

  const failures = []
  const entryName = entryMatch[1]
  const entry = chunks.find((chunk) => chunk.name === entryName)
  if (!entry) {
    failures.push(`entry chunk ${entryName} is missing from dist/assets`)
  } else if (entry.bytes > entryBudgetBytes) {
    failures.push(
      `entry chunk ${entry.name} is ${formatBytes(entry.bytes)}; budget is ${formatBytes(entryBudgetBytes)}`,
    )
  }

  for (const chunk of chunks) {
    if (chunk.bytes > chunkBudgetBytes) {
      failures.push(
        `chunk ${chunk.name} is ${formatBytes(chunk.bytes)}; budget is ${formatBytes(chunkBudgetBytes)}`,
      )
    }
  }

  for (const modulePrefix of lazyModules) {
    const chunk = chunks.find(({ name }) => name.startsWith(modulePrefix))
    if (!chunk) {
      failures.push(`expected lazy module chunk ${modulePrefix}*.js was not emitted`)
      continue
    }
    if (html.includes(`/assets/${chunk.name}`)) {
      failures.push(`lazy module ${chunk.name} is eagerly referenced by dist/index.html`)
    }
  }

  if (failures.length > 0) {
    throw new Error(`bundle budget failed:\n- ${failures.join('\n- ')}`)
  }

  const largest = chunks[0]
  console.log(
    `bundle budget OK: entry ${entry.name} ${formatBytes(entry.bytes)}, ` +
      `largest ${largest.name} ${formatBytes(largest.bytes)}, ${lazyModules.length} lazy seams preserved`,
  )
}

main().catch((error) => {
  console.error(error instanceof Error ? error.message : error)
  process.exitCode = 1
})
