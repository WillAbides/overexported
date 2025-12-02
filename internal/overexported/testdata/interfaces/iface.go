package interfaces

import "io"

// Impl implements io.Reader, so its Read method should be kept.
type Impl struct{}

// Read implements io.Reader.
func (i *Impl) Read(p []byte) (n int, err error) {
	return 0, io.EOF
}

// UnusedImplMethod is not part of any interface.
func (i *Impl) UnusedImplMethod() {}

// UnusedImpl doesn't implement any interface used externally.
type UnusedImpl struct{}

// DoSomething is not used externally.
func (u *UnusedImpl) DoSomething() {}
