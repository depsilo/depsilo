import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { ChartNoAxesCombined, CloudOff, Package2 } from 'lucide-react'
import { describe, expect, it } from 'vitest'

import Icon from '../src/components/app/icon'

describe('Icon', () => {
  it('renders the requested Lucide glyph inside the shared icon box', () => {
    for (const glyph of [CloudOff, ChartNoAxesCombined, Package2]) {
      const markup = renderToStaticMarkup(createElement(Icon, { icon: glyph }))
      expect(markup).toContain('icon')
      expect(markup).toContain('aria-hidden="true"')
    }
  })

  it('sizes from the shared box and keeps caller classes', () => {
    const small = renderToStaticMarkup(createElement(Icon, { icon: CloudOff, size: 'sm' }))
    expect(small).toContain('icon-sm')
    expect(small).toContain('focusable="false"')

    const large = renderToStaticMarkup(
      createElement(Icon, { icon: CloudOff, size: 'lg', className: 'text-primary' }),
    )
    expect(large).toContain('icon-lg')
    expect(large).toContain('text-primary')
  })
})
