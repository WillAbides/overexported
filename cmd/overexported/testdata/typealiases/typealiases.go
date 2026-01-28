package typealiases

import "time"

// Timestamp is a type alias for time.Time that's used externally.
type Timestamp = time.Time

// UnusedTimestamp is a type alias for time.Time that's not used externally.
type UnusedTimestamp = time.Time

// UsedString is a type alias that is used externally.
type UsedString = string

// UnusedString is a type alias that's not used externally.
type UnusedString = string

// Now returns the current time.
func Now() Timestamp {
	return time.Now()
}