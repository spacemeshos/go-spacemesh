// Package assert provides basic assert functions for tests
package assert

import (
	"errors"
	"fmt"
	"testing"
)

// basic assertion support

func Nil(t *testing.T, obj interface{}, msgs ...string) {
	t.Helper()
	if obj != nil {
		t.Fatal(msgs, "error:", errors.New("expected object to be nil"))
	}
}

func NotNil(t *testing.T, obj interface{}, msgs ...string) {
	t.Helper()
	if obj == nil {
		t.Fatal(msgs, "epxected a non-nil object")
	}
}

func True(t *testing.T, v bool, msgs ...string) {
	t.Helper()
	if !v {
		t.Fatal(msgs)
	}
}

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

func False(t *testing.T, v bool, msgs ...string) {
	t.Helper()
	True(t, !v, msgs...)
}

func NoErr(t *testing.T, err error, msgs ...string) {
	t.Helper()
	if err != nil {
		t.Fatal(msgs, "error:", err)
	}
}
func Err(t *testing.T, err error, msgs ...string) {
	t.Helper()
	if err == nil {
		t.Fatal(msgs, "error:", err)
	}
}
