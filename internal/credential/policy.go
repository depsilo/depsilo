package credential

import (
	"errors"
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

// ValidatePassword accepts long passphrases without arbitrary composition
// rules. Shorter passwords must use at least three character classes.
func (Policy) ValidatePassword(username, password string) error {
	if !utf8.ValidString(password) {
		return errors.New("password must be valid UTF-8")
	}
	passwordRunes := utf8.RuneCountInString(password)
	if passwordRunes < 12 {
		return errors.New("password must be at least 12 characters")
	}
	if len(password) > 72 {
		return errors.New("password must be at most 72 bytes")
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
	if passwordRunes < 20 && classes < 3 {
		return errors.New("password must use at least three character classes or be a passphrase of at least 20 characters")
	}

	lowerPassword := strings.ToLower(password)
	if username != "" && strings.Contains(lowerPassword, strings.ToLower(username)) {
		return errors.New("password must not contain the username")
	}
	for _, common := range []string{"adminadminadmin", "password123!", "change-me-in-production", "qwerty123456"} {
		if lowerPassword == common {
			return errors.New("password is too common")
		}
	}
	return nil
}
