package lib_test

import (
	"testing"

	"lib"
)

func TestExternal(t *testing.T) {
	_ = lib.UsedInExternalTest()
}
