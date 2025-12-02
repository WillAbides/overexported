package main

import (
	"fmt"

	"constvars"
)

func main() {
	fmt.Println(constvars.UsedConst)
	fmt.Println(constvars.UsedVar)
	fmt.Println(constvars.UsedFunc())
}
