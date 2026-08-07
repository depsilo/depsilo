import { useCallback, useEffect, useRef, useState } from 'react'

export function useTransientState<T>(initialValue: T) {
  const [value, setValue] = useState(initialValue)
  const timeoutRef = useRef<number | null>(null)

  const clearTimer = useCallback(() => {
    if (timeoutRef.current !== null) window.clearTimeout(timeoutRef.current)
    timeoutRef.current = null
  }, [])

  const show = useCallback((nextValue: T, duration = 2_000) => {
    clearTimer()
    setValue(nextValue)
    timeoutRef.current = window.setTimeout(() => {
      timeoutRef.current = null
      setValue(initialValue)
    }, duration)
  }, [clearTimer, initialValue])

  const reset = useCallback(() => {
    clearTimer()
    setValue(initialValue)
  }, [clearTimer, initialValue])

  useEffect(() => clearTimer, [clearTimer])

  return [value, show, reset] as const
}

export function useTransientFlag(duration = 2_000) {
  const [active, show] = useTransientState(false)
  const trigger = useCallback(() => show(true, duration), [duration, show])

  return [active, trigger] as const
}
