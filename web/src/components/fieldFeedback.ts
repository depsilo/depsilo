export function mergeDescriptionIds(...values: Array<string | undefined>) {
  const tokens = values.flatMap((value) => value?.split(/\s+/).filter(Boolean) ?? [])
  const uniqueTokens = [...new Set(tokens)]
  return uniqueTokens.length > 0 ? uniqueTokens.join(' ') : undefined
}
