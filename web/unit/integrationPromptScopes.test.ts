import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

import { LANGUAGES } from '../src/lib/ecosystemData'
import { integrationPromptPath } from '../src/portal/integrationPrompt'

// The Portal asks GET /api/v1/integration-prompt?ecosystem=<id> with its own
// ecosystem ids; the server answers 400 for an id it does not know. Read the
// server's scope table so the two lists cannot drift apart unnoticed.
const scopeSource = readFileSync(
  fileURLToPath(new URL('../../internal/prompts/scope.go', import.meta.url)),
  'utf8',
)
const serverIds = [...scopeSource.matchAll(/^\s+"([a-z]+)":\s+\{Name:/gm)].map(match => match[1]).sort()

describe('integration prompt scopes', () => {
  it('cover exactly the Portal ecosystems', () => {
    expect(serverIds).toEqual(LANGUAGES.map(language => language.id).sort())
  })

  it('name the ecosystem in the request', () => {
    expect(integrationPromptPath()).toBe('/api/v1/integration-prompt')
    expect(integrationPromptPath({ id: 'python', name: 'Python' })).toBe('/api/v1/integration-prompt?ecosystem=python')
  })
})
