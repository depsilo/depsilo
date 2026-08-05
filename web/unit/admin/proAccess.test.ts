import { expect, it } from 'vitest'

import { proAccessUrl } from '../../src/lib/buy'

it('keeps Pro access enquiries free of unconfirmed commercial terms', () => {
  const url = decodeURIComponent(proAccessUrl())
  expect(url).toContain('access options currently available')
  expect(url).not.toMatch(/\$?99|lifetime|subscription|PayPal|Alipay|bank/i)
})
