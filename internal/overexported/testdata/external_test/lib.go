package lib

// UsedInExternalTest is used by the external test package.
func UsedInExternalTest() string {
	return "test"
}

// UsedInInternalTest is used by the internal test package.
func UsedInInternalTest() string {
	return "internal"
}

// NotUsedInTests is not used by any test.
func NotUsedInTests() string {
	return "unused"
}
