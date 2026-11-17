import type { AxiosAdapter } from 'axios'
import { afterAll, describe, expect, it } from 'vitest'
import api, { setupApi } from '../src/lib/api'

const originalAdapter = api.defaults.adapter

afterAll(() => {
  api.defaults.adapter = originalAdapter
})

async function decode(body: unknown) {
  const adapter: AxiosAdapter = async config => ({
    data: body, status: 200, statusText: 'OK', headers: {}, config,
  })
  api.defaults.adapter = adapter
  return setupApi.getStatus()
}

describe('setup status decoding', () => {
  it('reads the listening port and the existing-configuration flag', async () => {
    expect(await decode({
      needs_setup: true, token_required: true, listen_port: 24646,
      token_command: 'depsilo setup token --port 24646 --host ::1', config_exists: true,
    })).toEqual({
      kind: 'setup-required', tokenRequired: true, listenPort: 24646,
      tokenCommand: 'depsilo setup token --port 24646 --host ::1', configExists: true,
    })
    expect(await decode({ needs_setup: false, token_required: false, listen_port: 24646 }))
      .toEqual({ kind: 'configured' })
  })

  it('accepts an older server that reports neither', async () => {
    expect(await decode({ needs_setup: true, token_required: false }))
      .toEqual({ kind: 'setup-required', tokenRequired: false, listenPort: undefined, tokenCommand: undefined, configExists: false })
  })

  it('treats a listening port that cannot be one as absent', async () => {
    // The value starts the port field, which setup writes to config.toml.
    for (const port of [0, -1, 65536, 70000, 23333.5, '23333', null, true, Number.NaN]) {
      const status = await decode({ needs_setup: true, token_required: true, listen_port: port })
      expect(status, String(port)).toMatchObject({ kind: 'setup-required', listenPort: undefined })
    }
    for (const port of [1, 65535]) {
      expect(await decode({ needs_setup: true, token_required: true, listen_port: port }), String(port))
        .toMatchObject({ listenPort: port })
    }
  })

  it('shows only a token command that is one', async () => {
    for (const command of [1, null, {}, '', 'rm -rf /', 'depsilo setup tokens', 'depsilo setup token\nrm -rf /',
      `depsilo setup token --host ${'x'.repeat(300)}`]) {
      const status = await decode({ needs_setup: true, token_required: true, token_command: command })
      expect(status, String(command)).toMatchObject({ kind: 'setup-required', tokenCommand: undefined })
    }
    for (const command of ['depsilo setup token', "depsilo setup token --host '::1%$(id)'"]) {
      expect(await decode({ needs_setup: true, token_required: true, token_command: command }), command)
        .toMatchObject({ tokenCommand: command })
    }
  })

  it('reports an existing configuration only for the boolean true', async () => {
    for (const flag of [false, 'true', 1, null, {}]) {
      const status = await decode({ needs_setup: true, token_required: true, config_exists: flag })
      expect(status, String(flag)).toMatchObject({ kind: 'setup-required', configExists: false })
    }
  })

  it('rejects a response without the two required fields', async () => {
    await expect(decode({ needs_setup: true })).rejects.toThrow('Invalid setup status response')
    await expect(decode([])).rejects.toThrow('Invalid setup status response')
  })
})
