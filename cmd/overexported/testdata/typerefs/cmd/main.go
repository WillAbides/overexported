package main

import (
	"fmt"

	"typerefs"
)

func main() {
	p := typerefs.UsedAsParam{Value: "hello"}
	fmt.Println(typerefs.TakesParam(p))

	r := typerefs.ReturnsType()
	fmt.Println(r.Value)

	s := []typerefs.UsedInSlice{{}, {}}
	fmt.Println(typerefs.TakesSlice(s))

	m := map[string]typerefs.UsedInMap{"a": {}}
	fmt.Println(typerefs.TakesMap(m))
}
