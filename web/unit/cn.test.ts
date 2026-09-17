import { describe, expect, it } from 'vitest'

import { cn } from '../src/lib/utils'

/**
 * `text-*` is a shared prefix: a font size and a text colour are both spelled
 * the same way. tailwind-merge only knows Tailwind's built-in sizes, so a
 * project token such as `text-body` looks like an unknown *colour* to it —
 * which silently dropped a button's `text-primary-foreground` and left the
 * label inheriting the container's colour. These cases pin the registration
 * that prevents it.
 */
describe('cn', () => {
  it('keeps a text colour when a project type token is merged', () => {
    const merged = cn('bg-primary text-primary-foreground', 'text-body font-medium')
    expect(merged).toContain('text-primary-foreground')
    expect(merged).toContain('text-body')
  })

  it('resolves two project type tokens to the last one', () => {
    expect(cn('text-body', 'text-micro')).toBe('text-micro')
    expect(cn('text-micro', 'text-body')).toBe('text-body')
  })

  it('resolves a built-in size against a project token', () => {
    expect(cn('text-sm', 'text-body')).toBe('text-body')
    expect(cn('text-body', 'text-title')).toBe('text-title')
  })

  it('still resolves colours independently of sizes', () => {
    expect(cn('text-muted-foreground', 'text-foreground')).toBe('text-foreground')
  })
})
