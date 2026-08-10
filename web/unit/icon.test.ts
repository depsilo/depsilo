import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import Icon, { type IconName } from '../src/components/Icon'

describe('Icon registry', () => {
  it('contains every formerly unresolved product icon', () => {
    const names = [
      'cloud_off',
      'gpp_maybe',
      'help',
      'hub',
      'monitoring',
      'package_2',
      'task_alt',
    ] satisfies IconName[]

    for (const name of names) {
      const markup = renderToStaticMarkup(createElement(Icon, { name }))
      expect(markup).toContain(`data-icon="${name}"`)
      expect(markup).not.toContain('data-icon-fallback')
    }
  })

  it('keeps icons decorative and exposes a visible fallback at runtime boundaries', () => {
    const markup = renderToStaticMarkup(createElement(Icon, {
      name: 'unregistered-runtime-icon' as IconName,
    }))

    expect(markup).toContain('aria-hidden="true"')
    expect(markup).toContain('focusable="false"')
    expect(markup).toContain('data-icon-fallback="true"')
  })
})
