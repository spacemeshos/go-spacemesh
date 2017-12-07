package assert

import (
	"errors"
	"fmt"
	"testing"
)

// basic assertion support

func Nil(t *testing.T, obj interface{}, msgs ...string) {
	if obj != nil {
		t.Fatal(msgs, "error:", errors.New("Expected object to be nil"))
	}
}

func NotNil(t *testing.T, obj interface{}, msgs ...string) {
	if obj == nil {
		t.Fatal(msgs, "epxected a non-nil object")
	}
}

func True(t *testing.T, v bool, msgs ...string) {
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

func False(t *testing.T, v bool, msgs ...string) {
	True(t, !v, msgs...)
}

func Err(t *testing.T, err error, msgs ...string) {
	if err == nil {
		t.Fatal(msgs, "error:", err)
	}
}
