package main

import (
	"fmt"

	"typealiases"
)

func main() {
	var t typealiases.Timestamp = typealiases.Now()
	fmt.Println(t)

	var s typealiases.UsedString = "hello"
	fmt.Println(s)

	// Explicitly reference type aliases
	var count typealiases.UsedAsParam = 42
	typealiases.ProcessCount(count)

	cfg := typealiases.GetConfig()
	var enabled typealiases.UsedInStruct = true
	cfg.Enabled = enabled
	fmt.Println(cfg)

	// Use method on type alias (not on original type)
	var counter typealiases.MyCounter
	counter.Increment()
	fmt.Println(counter)
}
