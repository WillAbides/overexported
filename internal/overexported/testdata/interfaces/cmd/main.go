package main

import (
	"io"

	"interfaces"
)

func main() {
	var r io.Reader = &interfaces.Impl{}
	buf := make([]byte, 10)
	_, _ = r.Read(buf)
}
