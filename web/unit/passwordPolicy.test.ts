import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import {
  PASSWORD_MAX_BYTES,
  PASSWORD_MIN_CHARACTERS,
  PASSWORD_MIN_CLASSES,
  PASSWORD_PASSPHRASE_CHARACTERS,
  passwordIssue,
} from '../src/lib/passwordPolicy'

interface PasswordVector {
  name: string
  username: string
  password: string
  accepted: boolean
  reason: string
}

// The Go policy runs against the same file in
// internal/credential/policy_vectors_test.go.
const vectorsPath = fileURLToPath(
  new URL('../../internal/credential/testdata/password_vectors.json', import.meta.url),
)
const { vectors } = JSON.parse(readFileSync(vectorsPath, 'utf8')) as { vectors: PasswordVector[] }

const policySource = readFileSync(
  fileURLToPath(new URL('../../internal/credential/policy.go', import.meta.url)),
  'utf8',
)

function goConstant(name: string) {
  const match = new RegExp(`^\\s*${name}\\s*=\\s*(\\d+)\\s*$`, 'm').exec(policySource)
  if (!match) throw new Error(`policy.go does not define ${name}`)
  return Number(match[1])
}

describe('browser password policy', () => {
  it('has shared vectors to run', () => {
    expect(vectors.length).toBeGreaterThan(0)
  })

  it.each(vectors)('agrees with the server on: $name', ({ username, password, accepted, reason }) => {
    expect(accepted).toBe(reason === '')
    expect(passwordIssue(username, password)).toBe(accepted ? null : reason)
  })

  it('uses the limits named in internal/credential/policy.go', () => {
    expect(PASSWORD_MIN_CHARACTERS).toBe(goConstant('minPasswordCharacters'))
    expect(PASSWORD_MAX_BYTES).toBe(goConstant('maxPasswordBytes'))
    expect(PASSWORD_PASSPHRASE_CHARACTERS).toBe(goConstant('passphraseCharacters'))
    expect(PASSWORD_MIN_CLASSES).toBe(goConstant('minCharacterClasses'))
  })

  it('lists the common passwords of internal/credential/policy.go', () => {
    const list = /commonPasswords = \[\]string\{([^}]*)\}/.exec(policySource)
    expect(list).not.toBeNull()
    for (const quoted of list![1].split(',')) {
      const common = JSON.parse(quoted.trim()) as string
      // Two entries are shorter than a passphrase and have too few classes, so
      // the class rule reports them first; none may ever be accepted.
      expect(['common', 'weak']).toContain(passwordIssue('', common))
    }
  })
})
