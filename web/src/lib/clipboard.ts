// Robust clipboard copy with HTTP fallback.
//
// navigator.clipboard.writeText only works in a Secure Context — that means
// HTTPS, localhost, or 127.0.0.1. Most Depsilo deployments are reached at a
// LAN IP over plain HTTP (e.g. http://10.4.20.52:23333), where the modern
// API silently fails. We fall back to the deprecated-but-universally-supported
// document.execCommand('copy') via a hidden textarea.
//
// Returns true on success, false on failure. Callers use it for "✓ Copied"
// state toggling — failure leaves the button in its default state.
export async function copyText(text: string): Promise<boolean> {
  if (navigator.clipboard && window.isSecureContext) {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      // fall through to legacy path
    }
  }
  return legacyCopy(text)
}

function legacyCopy(text: string): boolean {
  const ta = document.createElement('textarea')
  ta.value = text
  ta.setAttribute('readonly', '')
  // Off-screen but still in the DOM so selection works.
  ta.style.position = 'fixed'
  ta.style.top = '0'
  ta.style.left = '0'
  ta.style.opacity = '0'
  ta.style.pointerEvents = 'none'
  document.body.appendChild(ta)
  ta.focus()
  ta.select()
  ta.setSelectionRange(0, text.length)
  let ok = false
  try {
    ok = document.execCommand('copy')
  } catch {
    ok = false
  }
  document.body.removeChild(ta)
  return ok
}
