import { useCallback, useEffect, useState } from 'react'

/** Set on <html> while a page is immersive; the Admin shell hides its rail for it. */
export const IMMERSIVE_ATTRIBUTE = 'data-admin-immersive'

/**
 * Immersive mode gives one page the whole screen. The Admin shell hides its
 * navigation rail, and the browser is asked for real fullscreen as an
 * enhancement. The mode is the page's own state, so it still works where the
 * Fullscreen API is missing or refused, and it ends when the page unmounts,
 * which keeps a page without navigation from outliving its exit control.
 */
export function useImmersiveMode(): { active: boolean; toggle: () => void } {
  const [active, setActive] = useState(false)

  useEffect(() => {
    if (!active) return
    const root = document.documentElement
    root.setAttribute(IMMERSIVE_ATTRIBUTE, '')
    // Leaving browser fullscreen, by Escape or the browser's own control, leaves the mode.
    const onFullscreenChange = () => {
      if (!document.fullscreenElement) setActive(false)
    }
    // Without browser fullscreen Escape still leaves, unless a dialog or popover owns the key.
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || event.defaultPrevented || document.fullscreenElement) return
      if (document.querySelector('[role="dialog"]')) return
      setActive(false)
    }
    document.addEventListener('fullscreenchange', onFullscreenChange)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('fullscreenchange', onFullscreenChange)
      document.removeEventListener('keydown', onKeyDown)
      root.removeAttribute(IMMERSIVE_ATTRIBUTE)
      if (document.fullscreenElement) void document.exitFullscreen().catch(() => undefined)
    }
  }, [active])

  const toggle = useCallback(() => {
    if (active) {
      setActive(false)
      return
    }
    setActive(true)
    // Must run inside the click that asked for it; browsers refuse it otherwise.
    const root = document.documentElement
    if (typeof root.requestFullscreen === 'function') void root.requestFullscreen().catch(() => undefined)
  }, [active])

  return { active, toggle }
}
