// Package assert provides basic assert functions for tests.
package assert

import (
	"fmt"
	"testing"
)

// Deprecated: Use standard Go testing package.
func Nil(t *testing.T, obj interface{}, msgs ...string) {
	t.Helper()
	if obj != nil {
		t.Fatal(msgs, "error: expected object to be nil")
	}
}

// Deprecated: Use standard Go testing package.
func NotNil(t *testing.T, obj interface{}, msgs ...string) {
	t.Helper()
	if obj == nil {
		t.Fatal(msgs, "expected a non-nil object")
	}
}

// Deprecated: Use standard Go testing package.
func True(t *testing.T, v bool, msgs ...string) {
	t.Helper()
	if !v {
		t.Fatal(msgs)
	}
}

// Deprecated: Use standard Go testing package.
func Equal(t *testing.T, a interface{}, b interface{}, msg string) {
	t.Helper()
	if a == b {
		return
	}
	if len(msg) == 0 {
		msg = fmt.Sprintf("%v != %v", a, b)
	}
	t.Fatal(msg)
}

// Deprecated: Use standard Go testing package.
func False(t *testing.T, v bool, msgs ...string) {
	t.Helper()
	True(t, !v, msgs...)
}

// Deprecated: Use standard Go testing package.
func NoErr(t *testing.T, err error, msgs ...string) {
	t.Helper()
	if err != nil {
		t.Fatal(msgs, "error:", err)
	}
}

// Deprecated: Use standard Go testing package.
func Err(t *testing.T, err error, msgs ...string) {
	t.Helper()
	if err == nil {
		t.Fatal(msgs, "error:", err)
	}
}
