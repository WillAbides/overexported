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
}