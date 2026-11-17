package credential

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Policy is the single password policy for every interactive Depsilo
// operator credential. Username shape remains a caller concern: initial setup
// has stricter bootstrap naming rules than users created later in Admin.
type Policy struct{}

// CredentialPolicy is shared by initial setup and Admin user mutations.
var CredentialPolicy Policy

// The setup page checks the same rules in the browser before it submits:
// web/src/lib/passwordPolicy.ts repeats these limits, the character classes
// and the common-password list. Change both files together. Both run against
// testdata/password_vectors.json, so a difference fails one of the two suites.
const (
	// minPasswordCharacters is the shortest accepted password, in code points.
	minPasswordCharacters = 12
	// maxPasswordBytes is the bcrypt input limit, in UTF-8 bytes.
	maxPasswordBytes = 72
	// passphraseCharacters is the length, in code points, from which a
	// password no longer needs minCharacterClasses.
	passphraseCharacters = 20
	// minCharacterClasses counts lower-case letters, upper-case letters,
	// decimal digits and everything else as one class each.
	minCharacterClasses = 3
)

// commonPasswords are compared with the lower-cased password.
var commonPasswords = []string{"adminadminadmin", "password123!", "change-me-in-production", "qwerty123456"}

// ValidatePassword accepts long passphrases without arbitrary composition
// rules. Shorter passwords must use at least three character classes.
func (Policy) ValidatePassword(username, password string) error {
	if !utf8.ValidString(password) {
		return errors.New("password must be valid UTF-8")
	}
	passwordRunes := utf8.RuneCountInString(password)
	if passwordRunes < minPasswordCharacters {
		return fmt.Errorf("password must be at least %d characters", minPasswordCharacters)
	}
	if len(password) > maxPasswordBytes {
		return fmt.Errorf("password must be at most %d bytes", maxPasswordBytes)
	}

	classes := 0
	hasLower, hasUpper, hasDigit, hasSymbol := false, false, false, false
	for _, character := range password {
		switch {
		case unicode.IsControl(character):
			return errors.New("password must not contain control characters")
		case unicode.IsLower(character):
			hasLower = true
		case unicode.IsUpper(character):
			hasUpper = true
		case unicode.IsDigit(character):
			hasDigit = true
		default:
			hasSymbol = true
		}
	}
	for _, present := range []bool{hasLower, hasUpper, hasDigit, hasSymbol} {
		if present {
			classes++
		}
	}
	if passwordRunes < passphraseCharacters && classes < minCharacterClasses {
		return fmt.Errorf("password must use at least three character classes or be a passphrase of at least %d characters", passphraseCharacters)
	}

	lowerPassword := strings.ToLower(password)
	if username != "" && strings.Contains(lowerPassword, strings.ToLower(username)) {
		return errors.New("password must not contain the username")
	}
	for _, common := range commonPasswords {
		if lowerPassword == common {
			return errors.New("password is too common")
		}
	}
	return nil
}
