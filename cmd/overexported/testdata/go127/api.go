package go127

type GenericMethod struct{}

func (GenericMethod) Map[T any](value T) T {
	return value
}

type Promoted struct {
	GenericMethod
}

type UnusedGenericMethod struct{}

func (UnusedGenericMethod) Map[T any](value T) T {
	return value
}

type Addable[A Addable[A]] interface {
	Add(A) A
}

type Integer int

func (i Integer) Add(other Integer) Integer {
	return i + other
}

func Sum[A Addable[A]](x, y A) A {
	return x.Add(y)
}

func NewPointer() *int {
	return new(42)
}

type Embedded struct {
	Value int
}

type Record struct {
	Embedded
}

func NewRecord() Record {
	return Record{Value: 42}
}

func Identity[T any](value T) T {
	return value
}

var InferredIdentity func(int) int = Identity
