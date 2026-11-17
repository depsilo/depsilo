// Browser copy of the operator password policy. The server decides: the rules
// are defined in internal/credential/policy.go and must stay identical here,
// so that the setup page never blocks a password the server accepts and never
// submits one it rejects. Change both files together. Both run against
// internal/credential/testdata/password_vectors.json.

/** Shortest accepted password, in code points. */
export const PASSWORD_MIN_CHARACTERS = 12
/** The bcrypt input limit, in UTF-8 bytes. */
export const PASSWORD_MAX_BYTES = 72
/** Length, in code points, from which the character classes no longer matter. */
export const PASSWORD_PASSPHRASE_CHARACTERS = 20
/** Classes a shorter password needs: lower case, upper case, decimal digit, anything else. */
export const PASSWORD_MIN_CLASSES = 3

/** The limits as interpolation values for the translated strings that quote them. */
export const passwordLimits = {
  min: PASSWORD_MIN_CHARACTERS,
  passphrase: PASSWORD_PASSPHRASE_CHARACTERS,
  maxBytes: PASSWORD_MAX_BYTES,
}

/** Compared with the lower-cased password. The same entries as policy.go, no more and no fewer. */
export const commonPasswords: ReadonlySet<string> = new Set([
  'adminadminadmin',
  'password123!',
  'change-me-in-production',
  'qwerty123456',
])

export type PasswordIssue =
  | 'too_short'
  | 'too_long'
  | 'control_character'
  | 'weak'
  | 'contains_username'
  | 'common'

// Go's strings.ToLower maps every code point on its own. Lower-casing the
// whole string in JavaScript would also turn a word-final sigma into its final
// form, and U+0130 into two code points, so map each code point separately.
function lowerCase(value: string) {
  return Array.from(value, (character) => (
    character === 'İ' ? 'i' : character.toLowerCase()
  )).join('')
}

/** Returns the first rule the password breaks, in the server's order, or null. */
export function passwordIssue(username: string, password: string): PasswordIssue | null {
  const characters = Array.from(password)
  if (characters.length < PASSWORD_MIN_CHARACTERS) return 'too_short'
  if (new TextEncoder().encode(password).length > PASSWORD_MAX_BYTES) return 'too_long'
  if (/\p{Cc}/u.test(password)) return 'control_character'

  // The same switch as policy.go: a character that is not a lower-case letter
  // (Ll), an upper-case letter (Lu) or a decimal digit (Nd) is a symbol. That
  // includes letters without case and digits such as superscripts.
  const classes = [
    /\p{Ll}/u.test(password),
    /\p{Lu}/u.test(password),
    /\p{Nd}/u.test(password),
    /[^\p{Ll}\p{Lu}\p{Nd}]/u.test(password),
  ].filter(Boolean).length
  if (characters.length < PASSWORD_PASSPHRASE_CHARACTERS && classes < PASSWORD_MIN_CLASSES) return 'weak'

  const lowerPassword = lowerCase(password)
  if (username && lowerPassword.includes(lowerCase(username))) return 'contains_username'
  if (commonPasswords.has(lowerPassword)) return 'common'
  return null
}
