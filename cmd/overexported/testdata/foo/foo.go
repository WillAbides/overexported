package foo

func Foo() string {
	return Bar()
}

func Bar() string {
	return "baz"
}
