package generics

// UsedGeneric is a generic function used externally.
func UsedGeneric[T any](v T) T {
	return v
}

// UnusedGeneric is a generic function not used externally.
func UnusedGeneric[T any](v T) T {
	return v
}

// UsedGenericType is a generic type used externally.
type UsedGenericType[T any] struct {
	Value T
}

// Get returns the value.
func (u UsedGenericType[T]) Get() T {
	return u.Value
}

// UnusedGenericType is a generic type not used externally.
type UnusedGenericType[T any] struct {
	Value T
}

// Get returns the value.
func (u UnusedGenericType[T]) Get() T {
	return u.Value
}
