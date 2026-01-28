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

// UsedAsParam is a type alias used as a function parameter.
type UsedAsParam = int

// UnusedAsParam is a type alias not used externally.
type UnusedAsParam = int

// UsedInStruct is a type alias used in a struct field.
type UsedInStruct = bool

// UnusedInStruct is a type alias not used externally.
type UnusedInStruct = bool

// Config is a struct that uses type aliases.
type Config struct {
	Enabled UsedInStruct
}

// Now returns the current time.
func Now() Timestamp {
	return time.Now()
}

// ProcessCount takes a type alias as a parameter.
func ProcessCount(count UsedAsParam) {
	_ = count
}

// GetConfig returns a struct with type alias fields.
func GetConfig() Config {
	return Config{}
}

// Counter is a type with a method.
type Counter struct {
	count int
}

// Increment increments the counter.
func (c *Counter) Increment() {
	c.count++
}

// MyCounter is an alias to Counter that's used externally.
type MyCounter = Counter

// UnusedCounter is an alias to Counter that's not used externally.
type UnusedCounter = Counter
