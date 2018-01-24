package assert

import (
	"errors"
	"testing"
)

func TestAsserts(t *testing.T) {
	Nil(t, nil, "expected nil")
	NotNil(t, "foo'", "expected not nil")
	True(t, true, "expected true")
	False(t, false, "expected true")
	Equal(t, 1, 1, "expected equal")
	Equal(t, "foo", "foo", "expected equal")

	var err error
	NoErr(t, err, "expected no error")

	err = errors.New("An error")
	Err(t, err, "exected an error")
}
