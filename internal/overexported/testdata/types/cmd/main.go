package main

import (
	"fmt"

	"types"
)

func main() {
	t := types.UsedType{Field: "hello"}
	fmt.Println(t.UsedMethod())
}
