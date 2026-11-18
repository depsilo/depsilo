package contract

import "time"

// NullableTime sends "never" as null. Go's zero time would otherwise encode as
// 0001-01-01T00:00:00Z, which a client reads as a real, truthy timestamp. Use it
// for any `tstype:"string | null,required"` time that a domain type keeps as a
// zero value.
func NullableTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}
