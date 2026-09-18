/**
 * Depsilo's status vocabulary.
 *
 * A single request produces **three independent outcomes** — what the cache
 * did, what policy decided, and how delivery went — plus two facts that are
 * not outcomes at all: how a service is behaving right now, and how severe a
 * vulnerability is. Keeping them separate is the whole point: collapsing them
 * into one signal is the most likely way for this product's UI to lie.
 *
 * Before this module the same fact was coloured differently on different
 * surfaces (a cache miss was `warning` in the dashboard feed and neutral in
 * the logs), a policy refusal read as `warning` rather than as a refusal, and
 * facts that are not outcomes at all — an account being disabled, a token
 * being read-write — borrowed status colours. Every one of those is a
 * judgement about the product that had been made accidentally, at a call site.
 *
 * Tones are named after the token they read, so the vocabulary, the CSS, and
 * `components/ui` all say `destructive` for the same thing.
 */
export type Tone = 'success' | 'warning' | 'destructive' | 'neutral' | 'info'

/** What the cache did with a request. */
export type CacheOutcome = 'hit' | 'miss' | 'unknown' | 'error'

/** What policy decided about a request. */
export type PolicyDecision = 'allow' | 'deny'

/** How delivery went. */
export type DeliveryOutcome = 'upstream' | 'completed' | 'failed' | 'cancelled' | 'unknown'

/** How a service behaves right now. `stale` is a last known value with a failed refresh. */
export type HealthState = 'healthy' | 'degraded' | 'failed' | 'unknown' | 'stale'

/** How severe a known vulnerability is. A risk level, not an outcome. */
export type Severity = 'critical' | 'high' | 'medium' | 'low' | 'unknown'

/**
 * A cache hit is a success. A miss and an unrecorded result are **neutral**:
 * the request was still served, and a miss is how a cache is supposed to
 * behave the first time. Only a cache error is a failure.
 */
export function cacheTone(outcome: CacheOutcome | undefined): Tone {
  switch (outcome) {
    case 'hit': return 'success'
    case 'error': return 'destructive'
    default: return 'neutral'
  }
}

/**
 * An allow is the normal case, so it is neutral: colouring it would paint
 * every row of every log. A deny is an explicit refusal and reads as
 * destructive — never as `warning`, which would say "degraded" about a
 * decision the Operator deliberately made.
 */
export function policyTone(decision: PolicyDecision | undefined): Tone {
  return decision === 'deny' ? 'destructive' : 'neutral'
}

/** A failure is destructive; a cancelled or partial delivery is a warning. */
export function deliveryTone(outcome: DeliveryOutcome | undefined): Tone {
  switch (outcome) {
    case 'failed': return 'destructive'
    case 'cancelled': return 'warning'
    default: return 'neutral'
  }
}

/**
 * `unknown` and `stale` are first-class states. Rendering `unknown` as healthy
 * is wrong; rendering it as a failure is equally wrong.
 */
export function healthTone(state: HealthState | undefined): Tone {
  switch (state) {
    case 'healthy': return 'success'
    case 'degraded':
    case 'stale': return 'warning'
    case 'failed': return 'destructive'
    default: return 'neutral'
  }
}

/**
 * Severity is its own dimension. A critical vulnerability and a failed request
 * are different kinds of fact, and giving them the same colour by accident
 * would make a security page and an operations page mean the same thing.
 */
export function severityTone(severity: Severity | undefined): Tone {
  switch (severity) {
    case 'critical': return 'destructive'
    case 'high': return 'warning'
    default: return 'neutral'
  }
}

/**
 * The one signal a dense row is allowed to show.
 *
 * A refusal or a failure outranks a warning, which outranks a success, which
 * outranks anything neutral. A row showing three coloured chips has not
 * decided what matters and has pushed that work onto the Operator; the full
 * detail belongs in the row's own view.
 */
export function dominantTone(tones: ReadonlyArray<Tone | undefined>): Tone {
  const order: Tone[] = ['destructive', 'warning', 'info', 'success', 'neutral']
  return order.find(tone => tones.includes(tone)) ?? 'neutral'
}
