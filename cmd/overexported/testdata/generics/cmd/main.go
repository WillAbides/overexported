package main

import (
	"fmt"

	"generics"
)

func main() {
	fmt.Println(generics.UsedGeneric(42))
	t := generics.UsedGenericType[string]{Value: "hello"}
	fmt.Println(t.Get())
}
