package constvars

// UsedConst is used externally.
const UsedConst = "used"

// UnusedConst is not used externally.
const UnusedConst = "unused"

// UsedVar is used externally.
var UsedVar = "used"

// UnusedVar is not used externally.
var UnusedVar = "unused"

// UsedFunc is used externally.
func UsedFunc() string {
	return UsedConst
}

// UnusedFunc is not used externally.
func UnusedFunc() string {
	return UnusedConst
}
