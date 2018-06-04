// Package assert provides basic assert functions for tests
package assert

import (
	"errors"
	"fmt"
	"testing"
)

// basic assertion support

// Nil verifies that obj is nil.
func Nil(t *testing.T, obj interface{}, msgs ...string) {
	t.Helper()
	if obj != nil {
		t.Fatal(msgs, "error:", errors.New("expected object to be nil"))
	}
}

// NotNil verifies that obj is not nil.
func NotNil(t *testing.T, obj interface{}, msgs ...string) {
	t.Helper()
	if obj == nil {
		t.Fatal(msgs, "epxected a non-nil object")
	}
}

// True verifies that v is true.
func True(t *testing.T, v bool, msgs ...string) {
	t.Helper()
	if !v {
		t.Fatal(msgs)
	}
}

// Equal verifies that a is equal to b.
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

// False verifies that v is false.
func False(t *testing.T, v bool, msgs ...string) {
	t.Helper()
	True(t, !v, msgs...)
}

// NoErr verifies that err is nil.
func NoErr(t *testing.T, err error, msgs ...string) {
	t.Helper()
	if err != nil {
		t.Fatal(msgs, "error:", err)
	}
}

// Err verifies that err is not nil.
func Err(t *testing.T, err error, msgs ...string) {
	t.Helper()
	if err == nil {
		t.Fatal(msgs, "error:", err)
	}
}
