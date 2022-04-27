package utils

import "time"

var unixTimeZero = time.Unix(0, 0)

// IsZero returns true if the timestamp is "Go zero" (January 1, 1970 UTC)
// or a 0sec UNIX timestamp converted to Go's location-dependant timestamp.
// see https://github.com/golang/go/issues/33597
func IsZero(t time.Time) bool {
	return t.IsZero() || t.Equal(unixTimeZero)
}
