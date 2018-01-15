package assert_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/spacemeshos/go-spacemesh/assert"
)

func TestNil(t *testing.T) {
	assert.Nil(t, nil, "should be nil")
}

func TestNotNil(t *testing.T) {
	assert.NotNil(t, "", "should not be nil")
}

func TestTrue(t *testing.T) {
	assert.True(t, true, "should be true")
}

func TestEqual(t *testing.T) {
	var testCases = []struct {
		a interface{}
		b interface{}
	}{
		{"foo", "foo"},
		{nil, nil},
	}
	for _, tc := range testCases {
		msg := fmt.Sprintf("a '%v' should equal to b '%v'", tc.a, tc.b)
		assert.Equal(t, tc.a, tc.b, msg)
	}
}

func TestFalse(t *testing.T) {
	assert.False(t, false, "should be false")
}

func TestNoErr(t *testing.T) {
	assert.NoErr(t, nil, "should be no error")
}

func TestErr(t *testing.T) {
	assert.Err(t, errors.New("test error"), "should have error")
}
