import { describe, expect, it } from 'vitest'

import {
  cacheTone,
  deliveryTone,
  dominantTone,
  healthTone,
  policyTone,
  severityTone,
} from '../src/lib/status'

describe('cacheTone', () => {
  it('treats only a hit as a success and only an error as a failure', () => {
    expect(cacheTone('hit')).toBe('success')
    expect(cacheTone('error')).toBe('destructive')
  })

  it('keeps a miss neutral, because a miss is how a cache behaves', () => {
    expect(cacheTone('miss')).toBe('neutral')
    expect(cacheTone('unknown')).toBe('neutral')
    expect(cacheTone(undefined)).toBe('neutral')
  })
})

describe('policyTone', () => {
  it('treats an allow as normal and a deny as a refusal', () => {
    expect(policyTone('allow')).toBe('neutral')
    expect(policyTone('deny')).toBe('destructive')
  })

  it('never reports a refusal as merely degraded', () => {
    expect(policyTone('deny')).not.toBe('warning')
  })
})

describe('deliveryTone', () => {
  it('separates a failure from a partial result', () => {
    expect(deliveryTone('failed')).toBe('destructive')
    expect(deliveryTone('cancelled')).toBe('warning')
  })

  it('treats serving the request as normal', () => {
    expect(deliveryTone('upstream')).toBe('neutral')
    expect(deliveryTone('completed')).toBe('neutral')
    expect(deliveryTone('unknown')).toBe('neutral')
  })
})

describe('healthTone', () => {
  it('keeps unknown and stale distinct from both success and failure', () => {
    expect(healthTone('healthy')).toBe('success')
    expect(healthTone('degraded')).toBe('warning')
    expect(healthTone('stale')).toBe('warning')
    expect(healthTone('failed')).toBe('destructive')
    expect(healthTone('unknown')).toBe('neutral')
  })
})

describe('severityTone', () => {
  it('is a risk level rather than an outcome', () => {
    expect(severityTone('critical')).toBe('destructive')
    expect(severityTone('high')).toBe('warning')
  })

  it('never reports a finding as a success', () => {
    // A known vulnerability is a finding at any severity. It used to render in
    // green at `low`, which said the opposite of what it meant.
    expect(severityTone('medium')).toBe('neutral')
    expect(severityTone('low')).toBe('neutral')
    expect(severityTone('unknown')).toBe('neutral')
  })
})

describe('dominantTone', () => {
  it('lets the worst fact in a row win', () => {
    expect(dominantTone(['success', 'destructive'])).toBe('destructive')
    expect(dominantTone(['neutral', 'warning'])).toBe('warning')
    expect(dominantTone(['neutral', 'success'])).toBe('success')
  })

  it('is neutral when nothing is known', () => {
    expect(dominantTone([])).toBe('neutral')
    expect(dominantTone([undefined, undefined])).toBe('neutral')
    expect(dominantTone(['neutral'])).toBe('neutral')
  })
})
