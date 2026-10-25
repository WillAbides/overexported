package main

import (
	"go127"
	"math/rand/v2"
)

func main() {
	var value go127.GenericMethod
	_ = value.Map(42)
	_ = go127.NewPointer()
	_ = go127.Sum(go127.Integer(1), go127.Integer(2))
	_ = go127.NewRecord()
	_ = go127.InferredIdentity(42)

	var random rand.Rand
	_ = random.N[int](1)
}
