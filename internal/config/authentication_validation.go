package config

import "errors"

// AuthenticationValidationError carries stable, value-free UI hints. The
// message is also authored by Depsilo; provider responses and submitted values
// must never become field errors.
type AuthenticationValidationError struct {
	Fields  map[string]string
	Message string
}

func (e *AuthenticationValidationError) Error() string { return e.Message }

func authenticationFieldError(field, code, message string) error {
	return &AuthenticationValidationError{Fields: map[string]string{field: code}, Message: message}
}

func AuthenticationFieldErrors(err error) map[string]string {
	var validation *AuthenticationValidationError
	if !errors.As(err, &validation) {
		return nil
	}
	fields := make(map[string]string, len(validation.Fields))
	for field, code := range validation.Fields {
		fields[field] = code
	}
	return fields
}
