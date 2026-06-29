// Single source of truth for the Pro purchase URL.
//
// Today the purchase flow is email-based: the operator opens a mailto
// link, sends a structured order email, the maintainer replies with
// payment instructions (PayPal / Alipay / bank), receives payment,
// and emails back a license key the operator pastes into the License
// page. No payment-provider integration yet.
//
// When a real provider is wired in (Lemon Squeezy / Polar / Gumroad),
// only `buyLifetimeUrl()` needs to change — every CTA across the
// admin UI and (mirror copy in the landing repo) reads from this
// helper. Keep the helper signature stable so callers don't churn.

export const LIFETIME_PRICE_USD = 99
export const LIFETIME_PRICE_LABEL = `$${LIFETIME_PRICE_USD}`

const BUY_EMAIL = 'pay@depsilo.com'

const BUY_SUBJECT = `Buy Depsilo Pro Lifetime (${LIFETIME_PRICE_LABEL})`

const BUY_BODY = [
  'Hi Depsilo team,',
  '',
  `I'd like to purchase a Depsilo Pro lifetime license (${LIFETIME_PRICE_LABEL}).`,
  '',
  'Please reply with payment instructions (PayPal / Alipay / WeChat Pay / bank).',
  '',
  'Once paid, please send the license key to this email address.',
  '',
  'Thanks.',
].join('\n')

/**
 * Returns the mailto URL the "Buy lifetime" CTA opens. Subject + body
 * are pre-filled so the operator only has to click Send. Encoded with
 * encodeURIComponent so newlines and special characters survive the
 * mail-client handoff.
 */
export function buyLifetimeUrl(): string {
  return `mailto:${BUY_EMAIL}?subject=${encodeURIComponent(BUY_SUBJECT)}&body=${encodeURIComponent(BUY_BODY)}`
}
