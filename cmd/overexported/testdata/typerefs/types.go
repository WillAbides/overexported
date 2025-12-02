package typerefs

// UsedAsParam is used as a parameter type in main.
type UsedAsParam struct {
	Value string
}

// UsedAsReturn is used as a return type in main.
type UsedAsReturn struct {
	Value string
}

// UsedInSlice is used in a slice type in main.
type UsedInSlice struct{}

// UsedInMap is used in a map type in main.
type UsedInMap struct{}

// UnusedType is not referenced anywhere externally.
type UnusedType struct{}

// TakesParam takes a UsedAsParam.
func TakesParam(p UsedAsParam) string {
	return p.Value
}

// ReturnsType returns a UsedAsReturn.
func ReturnsType() UsedAsReturn {
	return UsedAsReturn{Value: "hello"}
}

// TakesSlice takes a slice of UsedInSlice.
func TakesSlice(s []UsedInSlice) int {
	return len(s)
}

// TakesMap takes a map with UsedInMap.
func TakesMap(m map[string]UsedInMap) int {
	return len(m)
}
