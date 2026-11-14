/** The ecosystem an AI hand-off is limited to. */
export interface AIScope {
  /** Portal ecosystem id; GET /api/v1/integration-prompt accepts it as `ecosystem`. */
  id: string
  /** The ecosystem as users say it, e.g. "Python". */
  name: string
}

/** The integration guide, limited to one ecosystem when a scope is given. */
export function integrationPromptPath(scope?: AIScope): string {
  return scope
    ? `/api/v1/integration-prompt?ecosystem=${encodeURIComponent(scope.id)}`
    : '/api/v1/integration-prompt'
}
