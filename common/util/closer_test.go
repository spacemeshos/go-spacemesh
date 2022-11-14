package util

import "testing"

func TestCloserDoubleClose(t *testing.T) {
	closer := NewCloser()
	closer.Close()
	closer.Close()
}
