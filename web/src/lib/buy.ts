// Single source of truth for the Pro access enquiry URL.
//
// Depsilo's commercial model is not settled. The UI therefore opens a
// neutral email enquiry instead of promising a price, term, payment
// method, or future entitlement. The team can reply with whatever access
// options are actually available at that time.
//
// If a durable access channel is chosen later, only `proAccessUrl()` needs
// to change; callers remain independent of the commercial implementation.

const PRO_ACCESS_EMAIL = 'pay@depsilo.com'
const PRO_ACCESS_SUBJECT = 'Depsilo Pro access enquiry'
const PRO_ACCESS_BODY = [
  'Hi Depsilo team,',
  '',
  "I'm evaluating Depsilo Pro and would like to understand the access options currently available.",
  '',
  'Please reply with the current details and next steps.',
  '',
  'Thanks.',
].join('\n')

/**
 * Returns the mailto URL used by Pro access CTAs. Subject and body are
 * pre-filled, while deliberately avoiding unconfirmed commercial terms.
 */
export function proAccessUrl(): string {
  return `mailto:${PRO_ACCESS_EMAIL}?subject=${encodeURIComponent(PRO_ACCESS_SUBJECT)}&body=${encodeURIComponent(PRO_ACCESS_BODY)}`
}
