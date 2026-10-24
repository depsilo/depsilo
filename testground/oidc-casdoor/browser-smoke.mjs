// Provider-only compatibility probe. depsilo-smoke.mjs exercises the application.
// Uses the existing web/ Playwright installation; never prints credentials/tokens.
import { createHash, createPublicKey, randomBytes, verify } from 'node:crypto'
import { createServer } from 'node:http'
import { readFile, mkdir, writeFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { homedir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const fixtureDir = dirname(fileURLToPath(import.meta.url))
const require = createRequire(resolve(fixtureDir, '../../web/package.json'))
const { chromium } = require('playwright')
const stateDir = join(homedir(), '.cache/depsilo/oidc-casdoor')
let credentials
const issuer = 'http://127.0.0.1:18000'
const redirectURI = 'http://127.0.0.1:18082/callback'
const results = []
let stage = 'configuration'
let pending
let browser

function check(condition, message) {
  if (!condition) {
    const error = new Error(message)
    error.name = 'ProbeAssertion'
    throw error
  }
}

async function requestJSON(url, options = {}) {
  const target = new URL(url)
  check(target.origin === issuer, 'Probe only contacts the isolated issuer')
  const response = await fetch(target, { ...options, redirect: 'error', signal: AbortSignal.timeout(10000) })
  const body = await response.json()
  return { response, body }
}

const server = createServer((request, response) => {
  const url = new URL(request.url, 'http://127.0.0.1:18082')
  if (url.pathname !== '/callback') {
    response.writeHead(404).end()
    return
  }
  const transaction = pending
  if (!transaction || url.searchParams.get('state') !== transaction.state) {
    response.writeHead(400).end('No matching test transaction.')
    return
  }
  pending = undefined
  response.writeHead(200, { 'Content-Type': 'text/plain; charset=utf-8', 'Cache-Control': 'no-store', 'Referrer-Policy': 'no-referrer' })
  response.end('Casdoor test callback received. This is an isolated provider probe, not a Depsilo login.')
  transaction.resolve(url)
})

try {
  credentials = JSON.parse(await readFile(join(stateDir, 'credentials.json'), 'utf8'))
  check(credentials.issuer === issuer && credentials.redirect_uris.includes(redirectURI), 'Unexpected fixture issuer or callback')
  await mkdir(join(stateDir, 'evidence'), { recursive: true, mode: 0o700 })
  const { response: discoveryResponse, body: metadata } = await requestJSON(`${issuer}/.well-known/openid-configuration`)
  check(discoveryResponse.ok && metadata.issuer === issuer, 'Discovery issuer mismatch')
  check(metadata.code_challenge_methods_supported.includes('S256'), 'Provider must advertise S256')
  const { response: keysResponse, body: jwks } = await requestJSON(metadata.jwks_uri)
  check(keysResponse.ok && jwks.keys.length > 0, 'Missing JWKS')
  for (const endpoint of [metadata.authorization_endpoint, metadata.token_endpoint, metadata.userinfo_endpoint]) {
    check(new URL(endpoint).origin === issuer, 'Endpoint escaped isolated issuer')
  }
  await new Promise((resolveListen, reject) => {
    server.once('error', reject)
    server.listen(18082, '127.0.0.1', resolveListen)
  })
  browser = await chromium.launch()

  async function authorize(username, { disabled = false, screenshot = false } = {}) {
    const user = credentials.users[username]
    const context = await browser.newContext({ viewport: { width: 1280, height: 800 } })
    const page = await context.newPage()
    const state = randomBytes(32).toString('base64url')
    const nonce = randomBytes(32).toString('base64url')
    const verifier = randomBytes(32).toString('base64url')
    let callbackResolve
    const callback = new Promise((resolveCallback) => { callbackResolve = resolveCallback })
    const callbackTimeout = setTimeout(() => callbackResolve(null), 20000)
    pending = { state, resolve: callbackResolve }
    const url = new URL(metadata.authorization_endpoint)
    for (const [key, value] of Object.entries({
      client_id: credentials.client_id, redirect_uri: redirectURI, response_type: 'code',
      scope: 'openid profile email', state, nonce, code_challenge_method: 'S256',
      code_challenge: createHash('sha256').update(verifier).digest('base64url'),
    })) url.searchParams.set(key, value)
    try {
      await page.goto(url.toString())
      await page.locator('#username').waitFor()
      if (screenshot) await page.screenshot({ path: join(stateDir, 'evidence/casdoor-login.png'), fullPage: true })
      await page.locator('#username').fill(user.username)
      await page.locator('#password').fill(user.password)
      if (disabled) {
        const loginResponse = page.waitForResponse((response) => new URL(response.url()).pathname === '/api/login')
        await page.getByRole('button', { name: 'Sign In', exact: true }).click()
        const body = await (await loginResponse).json()
        check(body.status === 'error', 'Disabled user was accepted')
        check(typeof body.msg === 'string' && /forbidden|disabled/i.test(body.msg), 'Disabled user rejected for an unrelated reason')
        check(page.url().startsWith(issuer), 'Disabled user reached callback')
        pending = undefined
        return
      }
      await page.getByRole('button', { name: 'Sign In', exact: true }).click()
      await page.waitForURL(`${redirectURI}*`, { timeout: 20000 })
      const returned = await callback
      check(returned !== null, 'No matching callback before deadline')
      check(!returned.searchParams.has('error') && !!returned.searchParams.get('code'), 'Authorization failed')
      return { code: returned.searchParams.get('code'), nonce, verifier }
    } finally {
      clearTimeout(callbackTimeout)
      pending = undefined
      await context.close()
    }
  }

  async function exchange(transaction, verifier = transaction.verifier) {
    return requestJSON(metadata.token_endpoint, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: new URLSearchParams({ grant_type: 'authorization_code', client_id: credentials.client_id,
        client_secret: credentials.client_secret, redirect_uri: redirectURI,
        code: transaction.code, code_verifier: verifier }),
    })
  }

  function validateIDToken(encoded, nonce) {
    check(typeof encoded === 'string', 'Token response has no ID token')
    const parts = encoded.split('.')
    check(parts.length === 3, 'Invalid ID token serialization')
    const header = JSON.parse(Buffer.from(parts[0], 'base64url').toString())
    const claims = JSON.parse(Buffer.from(parts[1], 'base64url').toString())
    check(header.alg === 'RS256', 'Fixture signature algorithm changed')
    const key = jwks.keys.find((key) => key.kid === header.kid)
    check(!!key, 'Signing key is not in discovery JWKS')
    check(verify('RSA-SHA256', Buffer.from(`${parts[0]}.${parts[1]}`), createPublicKey({ key, format: 'jwk' }), Buffer.from(parts[2], 'base64url')), 'Invalid ID token signature')
    check(claims.iss === issuer, 'ID token issuer mismatch')
    check((Array.isArray(claims.aud) ? claims.aud : [claims.aud]).includes(credentials.client_id), 'ID token audience mismatch')
    check(claims.nonce === nonce, 'ID token nonce mismatch')
    check(claims.exp > Date.now() / 1000, 'ID token expired')
    check(typeof claims.sub === 'string' && claims.sub.length > 0, 'Missing subject')
    return claims
  }

  const subjects = new Set()
  for (const username of ['admin-test', 'reader-test', 'unmapped-test']) {
    stage = `browser authorization and claims: ${username}`
    const transaction = await authorize(username, { screenshot: username === 'admin-test' })
    const { response, body: token } = await exchange(transaction)
    check(response.ok && !token.error, 'Code exchange failed')
    const claims = validateIDToken(token.id_token, transaction.nonce)
    const { response: userInfoResponse, body: userInfo } = await requestJSON(metadata.userinfo_endpoint, {
      headers: { Authorization: `Bearer ${token.access_token}` },
    })
    check(userInfoResponse.ok && userInfo.sub === claims.sub, 'UserInfo subject mismatch')
    check(!subjects.has(claims.sub), 'Distinct fixture users share a subject')
    subjects.add(claims.sub)
    check(Array.isArray(userInfo.groups) && userInfo.groups.every((group) => typeof group === 'string'), 'UserInfo groups must be a string array')
    const expected = credentials.users[username].groups
    check(JSON.stringify([...userInfo.groups].sort()) === JSON.stringify([...expected].sort()), 'Unexpected UserInfo groups')
    // This classification is probe-only; application enforcement is tested separately.
    const probeRole = userInfo.groups.includes(`${credentials.organization}/depsilo-admins`) ? 'admin'
      : userInfo.groups.includes(`${credentials.organization}/depsilo-readers`) ? 'readonly' : 'denied'
    check(probeRole === ({ 'admin-test': 'admin', 'reader-test': 'readonly', 'unmapped-test': 'denied' })[username], 'Unexpected probe role')
    results.push({ scenario: username, outcome: 'passed', checks: ['browser code flow', 'S256', 'signature', 'issuer', 'audience', 'nonce', 'UserInfo subject', 'groups'], groups: userInfo.groups, probeRole })
    if (username === 'admin-test') {
      stage = 'authorization code replay'
      const replay = await exchange(transaction)
      check(replay.response.status === 400 && replay.body.error === 'invalid_grant'
        && replay.body.error_description?.startsWith('authorization code has been used for token: ['),
      'Provider did not return the expected code-replay rejection')
      results.push({ scenario: 'consumed authorization code rejected', outcome: 'passed' })
    }
  }
  stage = 'incorrect PKCE verifier'
  const transaction = await authorize('reader-test')
  const invalid = await exchange(transaction, randomBytes(32).toString('base64url'))
  check(invalid.response.status === 400 && invalid.body.error === 'invalid_grant'
    && invalid.body.error_description?.startsWith('verifier is invalid, challengeAnswer: ['),
  'Provider did not return the expected PKCE rejection')
  results.push({ scenario: 'incorrect PKCE verifier rejected', outcome: 'passed' })

  stage = 'disabled user'
  await authorize('disabled-test', { disabled: true })
  results.push({ scenario: 'disabled user rejected by Casdoor', outcome: 'passed' })
  const report = { timestamp: new Date().toISOString(), issuer, providerOnly: true,
    limitation: 'No Depsilo OIDC login or authorization implementation was tested.', results }
  await writeFile(join(stateDir, 'evidence/browser-smoke.json'), `${JSON.stringify(report, null, 2)}\n`, { mode: 0o600 })
  console.log(JSON.stringify(report, null, 2))
} catch (error) {
  // Browser errors may include filled passwords; never print arbitrary error messages.
  console.error(`Casdoor provider probe failed at: ${stage}. Error type: ${error.name}.`)
  if (error.name === 'ProbeAssertion') console.error(error.message)
  process.exitCode = 1
} finally {
  if (browser) await browser.close()
  if (server.listening) await new Promise((resolveClose) => server.close(resolveClose))
}
