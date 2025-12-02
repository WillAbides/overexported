package types

// UsedType is a type that is used externally.
type UsedType struct {
	Field string
}

// UsedMethod is a method used externally.
func (u UsedType) UsedMethod() string {
	return u.Field
}

// UnusedMethod is a method not used externally.
func (u UsedType) UnusedMethod() string {
	return ""
}

// UnusedType is a type not used externally.
type UnusedType struct {
	Field string
}

// UnusedTypeMethod is a method on an unused type.
func (u UnusedType) UnusedTypeMethod() string {
	return u.Field
}
