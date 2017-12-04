package assert

import (
	"fmt"
	"testing"
)

// basic assertion support

func Nil(err error, t *testing.T, msgs ...string) {
	if err != nil {
		t.Fatal(msgs, "error:", err)
	}
}

func True(v bool, t *testing.T, msgs ...string) {
	if !v {
		t.Fatal(msgs)
	}
}

func Equal(t *testing.T, a interface{}, b interface{}, msg string) {
	if a == b {
		return
	}
	if len(msg) == 0 {
		msg = fmt.Sprintf("%v != %v", a, b)
	}
	t.Fatal(msg)
}

func False(v bool, t *testing.T, msgs ...string) {
	True(!v, t, msgs...)
}

func Err(err error, t *testing.T, msgs ...string) {
	if err == nil {
		t.Fatal(msgs, "error:", err)
	}
}
