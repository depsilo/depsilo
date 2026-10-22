function getLocalStorage(): Storage | null {
  if (typeof window === 'undefined') return null

  try {
    return window.localStorage
  } catch {
    return null
  }
}

function getSessionStorage(): Storage | null {
  if (typeof window === 'undefined') return null

  try {
    return window.sessionStorage
  } catch {
    return null
  }
}

export function readLocalStorage(key: string): string | null {
  try {
    return getLocalStorage()?.getItem(key) ?? null
  } catch {
    return null
  }
}

export function writeLocalStorage(key: string, value: string): boolean {
  try {
    const storage = getLocalStorage()
    if (!storage) return false
    storage.setItem(key, value)
    return true
  } catch {
    return false
  }
}

export function removeLocalStorage(key: string): boolean {
  if (key === 'token') {
    // Settings drafts contain only the six non-secret editable fields. Clear
    // them on every logout path, including session expiry and password change.
    try {
      const session = getSessionStorage()
      if (session) {
        for (const name of Object.keys(session)) {
          if (name.startsWith('depsilo:settings-draft:')) session.removeItem(name)
        }
      }
    } catch { /* unavailable draft storage must never prevent logout */ }
  }
  try {
    const storage = getLocalStorage()
    if (!storage) return false
    storage.removeItem(key)
    return true
  } catch {
    return false
  }
}

export function readSessionStorage(key: string): string | null {
  try {
    return getSessionStorage()?.getItem(key) ?? null
  } catch {
    return null
  }
}

export function writeSessionStorage(key: string, value: string): boolean {
  try {
    const storage = getSessionStorage()
    if (!storage) return false
    storage.setItem(key, value)
    return true
  } catch {
    return false
  }
}

export function removeSessionStorage(key: string): boolean {
  try {
    const storage = getSessionStorage()
    if (!storage) return false
    storage.removeItem(key)
    return true
  } catch {
    return false
  }
}
