/**
 * Join `aria-describedby` sources without repeating an id that is already
 * present, so a field's own message can sit alongside a caller-supplied hint
 * without the accessible description listing the same node twice.
 */
export function mergeDescriptionIds(...values: Array<string | undefined>) {
  const tokens = values.flatMap((value) => value?.split(/\s+/).filter(Boolean) ?? [])
  const uniqueTokens = [...new Set(tokens)]
  return uniqueTokens.length > 0 ? uniqueTokens.join(' ') : undefined
}
